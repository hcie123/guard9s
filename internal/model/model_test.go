package model_test

import (
	"testing"
	"time"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ptr[T any](v T) *T { return &v }
func TestReadiness(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    []model.Finding
		want string
	}{{"pass", []model.Finding{{Severity: model.Pass}}, "READY"}, {"info", []model.Finding{{Severity: model.Info}}, "READY"}, {"warn", []model.Finding{{Severity: model.Warn}}, "READY WITH WARNINGS"}, {"high", []model.Finding{{Severity: model.High}, {Severity: model.Warn}}, "NOT RECOMMENDED"}, {"critical", []model.Finding{{Severity: model.Critical}}, "BLOCKED"}, {"unknown", []model.Finding{{Severity: model.Info, Unknown: true}}, "NOT RECOMMENDED"}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.Readiness(tc.f); got != tc.want {
				t.Fatalf("%s != %s", got, tc.want)
			}
		})
	}
	if model.Severity(99).String() != "UNKNOWN" {
		t.Fatal("invalid severity")
	}
	f := []model.Finding{{Severity: model.Warn, Category: "B"}, {Severity: model.Critical}, {Severity: model.Warn, Category: "A"}}
	model.SortFindings(f)
	if f[0].Severity != model.Critical || f[1].Category != "A" {
		t.Fatal(f)
	}
}
func TestOwners(t *testing.T) {
	s := demo.Snapshot("mixed")
	kinds := map[string]bool{}
	for _, p := range s.Pods {
		o := s.Owner(p)
		if !o.Resolved {
			t.Fatalf("unresolved demo owner: %+v", o)
		}
		kinds[o.Kind] = true
	}
	for _, kind := range []string{"Deployment", "StatefulSet", "DaemonSet", "Standalone", "StaticPod", "CronJob"} {
		if !kinds[kind] {
			t.Fatalf("missing %s", kind)
		}
	}
	p := s.Pods[0]
	s.ReplicaSets[0].OwnerReferences = nil
	s.ReplicaSets[0].Spec.Replicas = nil
	if o := s.Owner(p); o.Kind != "ReplicaSet" || !o.Resolved || o.Replicas != 1 {
		t.Fatal(o)
	}
	s.ReplicaSets[0].UID = "different"
	if s.Owner(p).Resolved {
		t.Fatal("mismatched owner UID accepted")
	}
	jobPod := s.Pods[len(s.Pods)-1]
	s.Jobs[0].OwnerReferences = nil
	if o := s.Owner(jobPod); o.Kind != "Job" || !o.Resolved {
		t.Fatal(o)
	}
	s.Jobs = nil
	if s.Owner(jobPod).Resolved {
		t.Fatal("missing job accepted")
	}
}
func TestRequests(t *testing.T) {
	for _, tc := range []struct {
		name     string
		change   func(*core.Pod)
		cpu, mem int64
	}{
		{"normal", func(p *core.Pod) {}, 250, 256 << 20},
		{"init_peak", func(p *core.Pod) {
			p.Spec.InitContainers = []core.Container{{Name: "init", Resources: core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("2"), core.ResourceMemory: resource.MustParse("1Gi")}}}}
		}, 2000, 1 << 30},
		{"sidecar_overlap", func(p *core.Pod) {
			p.Spec.InitContainers = []core.Container{{Name: "sidecar", RestartPolicy: ptr(core.ContainerRestartPolicyAlways), Resources: core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("500m"), core.ResourceMemory: resource.MustParse("128Mi")}}}}
		}, 750, 384 << 20},
		{"overhead", func(p *core.Pod) {
			p.Spec.Overhead = core.ResourceList{core.ResourceCPU: resource.MustParse("100m"), core.ResourceMemory: resource.MustParse("64Mi")}
		}, 350, 320 << 20},
		{"pod_level", func(p *core.Pod) {
			p.Spec.Resources = &core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("1"), core.ResourceMemory: resource.MustParse("1Gi")}}
		}, 1000, 1 << 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := demo.Snapshot("healthy").Pods[0]
			tc.change(p)
			r := model.Requests(p)
			if r.CPU != tc.cpu || r.Memory != tc.mem || r.Pods != 1 {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestResourceAndPodHelpers(t *testing.T) {
	s := demo.Snapshot("healthy")
	p := s.Pods[0]
	if !model.Ready(p) {
		t.Fatal("ready")
	}
	p.DeletionTimestamp = ptr(meta.NewTime(s.At))
	if model.Ready(p) {
		t.Fatal("terminating counted ready")
	}
	p.Status.Phase = core.PodSucceeded
	if model.Active(p) {
		t.Fatal("terminal pod consumes capacity")
	}
	p.Status.Phase = core.PodRunning
	p.DeletionTimestamp = nil
	p.Status.Conditions = nil
	if model.Ready(p) {
		t.Fatal("missing Ready accepted")
	}
	if s.Node("missing") != nil || s.PVC("demo", "missing") != nil || s.PV("missing") != nil {
		t.Fatal("missing lookup")
	}
	s.Nodes[0].Status.Allocatable = core.ResourceList{}
	r := s.Free(s.Nodes[0])
	if r.CPU != 0 || r.Memory != 0 || r.Pods != 0 {
		t.Fatalf("negative free: %+v", r)
	}
	if !(model.Resources{CPU: 1, Memory: 2, Pods: 3}).Fits(model.Resources{CPU: 1, Memory: 2, Pods: 3}) {
		t.Fatal("fit")
	}
}
func TestEventTimes(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := &core.Event{ObjectMeta: meta.ObjectMeta{CreationTimestamp: meta.NewTime(now)}}
	if !model.EventTime(e).Equal(now) {
		t.Fatal("creation time")
	}
	e.EventTime = meta.NewMicroTime(now.Add(time.Minute))
	if !model.EventTime(e).Equal(e.EventTime.Time) {
		t.Fatal("event time")
	}
	e.LastTimestamp = meta.NewTime(now.Add(2 * time.Minute))
	if !model.EventTime(e).Equal(e.LastTimestamp.Time) {
		t.Fatal("last time")
	}
	e.Series = &core.EventSeries{LastObservedTime: meta.NewMicroTime(now.Add(3 * time.Minute))}
	if !model.EventTime(e).Equal(e.Series.LastObservedTime.Time) {
		t.Fatal("series time")
	}
}

func TestNodeEligibilityAndEventContracts(t *testing.T) {
	node := &core.Node{
		ObjectMeta: meta.ObjectMeta{Name: "worker-a"},
		Status: core.NodeStatus{Conditions: []core.NodeCondition{
			{Type: core.NodeReady, Status: core.ConditionTrue},
			{Type: core.NodeMemoryPressure, Status: core.ConditionFalse},
			{Type: core.NodeDiskPressure, Status: core.ConditionFalse},
			{Type: core.NodePIDPressure, Status: core.ConditionFalse},
			{Type: core.NodeNetworkUnavailable, Status: core.ConditionFalse},
		}},
	}
	if model.Condition(node, core.NodeReady) != core.ConditionTrue || !model.Eligible(node) {
		t.Fatal("healthy node should be eligible")
	}
	if model.Condition(node, core.NodeConditionType("SyntheticMissing")) != core.ConditionUnknown {
		t.Fatal("missing node condition should stay unknown")
	}
	node.Spec.Unschedulable = true
	if model.Eligible(node) {
		t.Fatal("cordoned node should not be eligible")
	}
	node.Spec.Unschedulable = false
	node.Status.Conditions[1].Status = core.ConditionTrue
	if model.Eligible(node) {
		t.Fatal("memory pressure should make node ineligible")
	}

	active := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "standalone", Namespace: "demo"}, Status: core.PodStatus{Phase: core.PodRunning}}
	if !model.Movable(model.Snapshot{}, active) {
		t.Fatal("active standalone pod should be movable")
	}
	daemon := active.DeepCopy()
	daemon.Name = "daemon"
	daemon.OwnerReferences = []meta.OwnerReference{{Kind: "DaemonSet", Name: "agent", Controller: ptr(true)}}
	if model.Movable(model.Snapshot{}, daemon) {
		t.Fatal("DaemonSet pod should not be counted as relocation demand")
	}
	static := active.DeepCopy()
	static.Name = "static"
	static.Annotations = map[string]string{core.MirrorPodAnnotationKey: "mirror"}
	if model.Movable(model.Snapshot{}, static) {
		t.Fatal("static pod should not be counted as relocation demand")
	}
	active.Status.Phase = core.PodSucceeded
	if model.Movable(model.Snapshot{}, active) {
		t.Fatal("terminal pod should not be movable")
	}

	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	pod := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "app", Namespace: "demo"}, Spec: core.PodSpec{NodeName: "worker-a"}}
	otherPod := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "other", Namespace: "demo"}, Spec: core.PodSpec{NodeName: "worker-b"}}
	events := []*core.Event{
		{ObjectMeta: meta.ObjectMeta{Name: "node-warning", CreationTimestamp: meta.NewTime(now.Add(time.Minute))}, Type: core.EventTypeWarning, Reason: "SyntheticWarning", InvolvedObject: core.ObjectReference{Kind: "Node", Name: "worker-a"}},
		{ObjectMeta: meta.ObjectMeta{Name: "pod-important", CreationTimestamp: meta.NewTime(now.Add(2 * time.Minute))}, Type: core.EventTypeNormal, Reason: "FailedMount", InvolvedObject: core.ObjectReference{Kind: "Pod", Namespace: "demo", Name: "app"}},
		{ObjectMeta: meta.ObjectMeta{Name: "routine", CreationTimestamp: meta.NewTime(now.Add(3 * time.Minute))}, Type: core.EventTypeNormal, Reason: "Started", InvolvedObject: core.ObjectReference{Kind: "Pod", Namespace: "demo", Name: "app"}},
		{ObjectMeta: meta.ObjectMeta{Name: "other-node", CreationTimestamp: meta.NewTime(now.Add(4 * time.Minute))}, Type: core.EventTypeWarning, Reason: "SyntheticWarning", InvolvedObject: core.ObjectReference{Kind: "Pod", Namespace: "demo", Name: "other"}},
	}
	s := model.Snapshot{Pods: []*core.Pod{pod, otherPod}, Events: events}
	got := model.RelevantEvents(s, "worker-a")
	if len(got) != 2 {
		t.Fatalf("unexpected node event count: %d", len(got))
	}
	if got[0].Name != "pod-important" || got[1].Name != "node-warning" {
		t.Fatalf("unexpected node event filtering/order: %s, %s", got[0].Name, got[1].Name)
	}
	if all := model.RelevantEvents(s, ""); len(all) != 3 || all[0].Name != "other-node" {
		t.Fatalf("unexpected cluster event filtering/order: %v", len(all))
	}

	if !model.OptionalResource(model.CSINodesResource) || !model.OptionalResource(model.VolumeAttachmentsResource) || model.OptionalResource(model.NamespacesResource) {
		t.Fatal("optional resource contract changed")
	}
	if got := (model.Resources{CPU: 1500, Memory: 2 << 30, Pods: 3}).String(); got != "CPU 1.50 cores | memory 2.00 GiB | pods 3" {
		t.Fatalf("unexpected resource rendering: %s", got)
	}
	if model.Severity(-1).String() != "UNKNOWN" {
		t.Fatal("negative severity should be unknown")
	}
}

func TestUnindexedLookupFallbacks(t *testing.T) {
	node := &core.Node{ObjectMeta: meta.ObjectMeta{Name: "worker-a"}}
	pvc := &core.PersistentVolumeClaim{ObjectMeta: meta.ObjectMeta{Name: "claim", Namespace: "demo"}}
	pv := &core.PersistentVolume{ObjectMeta: meta.ObjectMeta{Name: "volume"}}
	s := model.Snapshot{Nodes: []*core.Node{node}, PVCs: []*core.PersistentVolumeClaim{pvc}, PVs: []*core.PersistentVolume{pv}}
	if s.Node("worker-a") != node || s.PVC("demo", "claim") != pvc || s.PV("volume") != pv {
		t.Fatal("unindexed linear lookups lost present inventory")
	}
}
