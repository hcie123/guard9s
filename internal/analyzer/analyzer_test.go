package analyzer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func fixture() model.Snapshot {
	s := demo.Snapshot("healthy")
	s.Pods = []*core.Pod{s.Pods[0], s.Pods[9]}
	s.Deployments[0].Spec.Replicas = ptr(int32(2))
	s.Deployments[0].Status.ReadyReplicas = 2
	s.PDBs[0].Status.CurrentHealthy = 2
	s.PDBs[0].Status.DesiredHealthy = 1
	s.PDBs[0].Status.ExpectedPods = 2
	s.PDBs[0].Status.DisruptionsAllowed = 1
	return s
}
func ptr[T any](v T) *T { return &v }
func run(t *testing.T, a Analyzer, s model.Snapshot) []model.Finding {
	t.Helper()
	f, err := a.Analyze(context.Background(), Input{Snapshot: s, Node: "worker-01"})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range f {
		if x.Resource == "" || x.Reason == "" || x.Evidence == "" || x.Recommendation == "" {
			t.Fatalf("incomplete evidence: %+v", x)
		}
	}
	return f
}
func has(f []model.Finding, level model.Severity, reason string) bool {
	for _, x := range f {
		if x.Severity == level && strings.Contains(x.Reason, reason) {
			return true
		}
	}
	return false
}
func anyUnknown(f []model.Finding) bool {
	for _, x := range f {
		if x.Unknown {
			return true
		}
	}
	return false
}

func TestNodeHealth(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*model.Snapshot)
		level  model.Severity
		reason string
	}{
		{"ready", func(s *model.Snapshot) {}, model.Pass, "Ready=True"},
		{"not_ready", func(s *model.Snapshot) { s.Nodes[0].Status.Conditions[0].Status = core.ConditionFalse }, model.High, "Ready=False"},
		{"memory_pressure", func(s *model.Snapshot) { s.Nodes[0].Status.Conditions[1].Status = core.ConditionTrue }, model.High, "MemoryPressure=True"},
		{"disk_pressure", func(s *model.Snapshot) { s.Nodes[0].Status.Conditions[2].Status = core.ConditionTrue }, model.High, "DiskPressure=True"},
		{"pid_pressure", func(s *model.Snapshot) { s.Nodes[0].Status.Conditions[3].Status = core.ConditionTrue }, model.High, "PIDPressure=True"},
		{"unknown", func(s *model.Snapshot) { s.Nodes[0].Status.Conditions = nil }, model.High, "UNKNOWN"},
		{"cordoned", func(s *model.Snapshot) { s.Nodes[0].Spec.Unschedulable = true }, model.Info, "unschedulable"},
		{"control_plane", func(s *model.Snapshot) { s.Nodes[0].Labels["node-role.kubernetes.io/control-plane"] = "" }, model.High, "quorum"},
		{"missing_node", func(s *model.Snapshot) { s.Nodes = nil }, model.High, "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			tc.change(&s)
			if f := run(t, NodeHealthAnalyzer{}, s); !has(f, tc.level, tc.reason) {
				t.Fatalf("findings=%+v", f)
			}
		})
	}
}

func TestPDBCases(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*model.Snapshot)
		level   model.Severity
		reason  string
		unknown bool
	}{
		{"zero_allowance", func(s *model.Snapshot) { s.PDBs[0].Status.DisruptionsAllowed = 0 }, model.Critical, "prevents", false},
		{"positive_allowance", func(s *model.Snapshot) {}, model.Pass, "permits", false},
		{"no_pdb", func(s *model.Snapshot) { s.PDBs = nil }, model.Info, "No matching", false},
		{"nil_selector_matches_none", func(s *model.Snapshot) { s.PDBs[0].Spec.Selector = nil }, model.Info, "No matching", false},
		{"empty_selector_matches_all", func(s *model.Snapshot) {
			s.PDBs[0].Spec.Selector = &meta.LabelSelector{}
			s.PDBs[0].Status.DisruptionsAllowed = 0
		}, model.Critical, "prevents", false},
		{"expression_selector", func(s *model.Snapshot) {
			s.PDBs[0].Spec.Selector = &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{{Key: "app", Operator: meta.LabelSelectorOpIn, Values: []string{"nginx"}}}}
		}, model.Pass, "permits", false},
		{"different_namespace", func(s *model.Snapshot) { s.PDBs[0].Namespace = "production" }, model.Info, "No matching", false},
		{"overlapping", func(s *model.Snapshot) { s.PDBs = append(s.PDBs, s.PDBs[0].DeepCopy()); s.PDBs[1].Name = "overlapping" }, model.Critical, "Multiple", false},
		{"invalid_selector", func(s *model.Snapshot) {
			s.PDBs[0].Spec.Selector = &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{{Key: "app", Operator: "Invalid"}}}
		}, model.High, "selector", true},
		{"stale_status", func(s *model.Snapshot) { s.PDBs[0].Generation = 2 }, model.High, "stale", true},
		{"unhealthy_always_allow_conservative", func(s *model.Snapshot) {
			s.PDBs[0].Status.DisruptionsAllowed = 0
			s.PDBs[0].Spec.UnhealthyPodEvictionPolicy = ptr(policy.AlwaysAllow)
			s.Pods[0].Status.Conditions[0].Status = core.ConditionFalse
		}, model.Critical, "requires review", false},
		{"shared_budget_sequential", func(s *model.Snapshot) { s.Pods[1].Spec.NodeName = "worker-01" }, model.Warn, "more matching", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			tc.change(&s)
			f := run(t, PDBAnalyzer{}, s)
			if !has(f, tc.level, tc.reason) || anyUnknown(f) != tc.unknown {
				t.Fatalf("findings=%+v", f)
			}
		})
	}
	s := demo.Snapshot("mixed")
	f := run(t, PDBAnalyzer{}, s)
	for _, x := range f {
		if strings.HasPrefix(x.Resource, "node-agent") || x.Resource == "static-example" {
			t.Fatal("drain-exempt pod was evaluated for eviction")
		}
	}
}

func TestReplicaCases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*model.Snapshot)
		level  model.Severity
		reason string
	}{
		{"replicas_two_spread", func(s *model.Snapshot) {}, model.Pass, "other nodes"},
		{"single_replica", func(s *model.Snapshot) { s.Pods = s.Pods[:1]; s.Deployments[0].Spec.Replicas = ptr(int32(1)) }, model.High, "Only one"},
		{"multiple_desired_one_ready", func(s *model.Snapshot) { s.Pods[1].Status.Conditions[0].Status = core.ConditionFalse }, model.High, "Only one"},
		{"replica_on_not_ready_node", func(s *model.Snapshot) { s.Nodes[1].Status.Conditions[0].Status = core.ConditionFalse }, model.High, "Only one"},
		{"all_colocated", func(s *model.Snapshot) { s.Pods[1].Spec.NodeName = "worker-01" }, model.High, "All ready"},
		{"none_ready", func(s *model.Snapshot) {
			for _, p := range s.Pods {
				p.Status.Conditions[0].Status = core.ConditionFalse
			}
		}, model.High, "No ready"},
		{"standalone", func(s *model.Snapshot) { s.Pods[0].OwnerReferences = nil }, model.High, "Standalone"},
		{"static", func(s *model.Snapshot) {
			s.Pods[0].Annotations = map[string]string{core.MirrorPodAnnotationKey: "mock"}
		}, model.High, "Static"},
		{"missing_rs", func(s *model.Snapshot) { s.ReplicaSets = nil }, model.High, "could not be resolved"},
		{"missing_deployment", func(s *model.Snapshot) { s.Deployments = nil }, model.High, "could not be resolved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			tc.change(&s)
			if f := run(t, ReplicaAnalyzer{}, s); !has(f, tc.level, tc.reason) {
				t.Fatalf("findings=%+v", f)
			}
		})
	}
	s := demo.Snapshot("mixed")
	f := run(t, ReplicaAnalyzer{}, s)
	if !has(f, model.High, "Only one") || !has(f, model.Info, "DaemonSet") {
		t.Fatalf("%+v", f)
	}
	f, _ = (ReplicaAnalyzer{}).Analyze(context.Background(), Input{Snapshot: s, Node: "worker-02"})
	if !has(f, model.Warn, "Batch") {
		t.Fatal("CronJob risk missing")
	}
	s.StatefulSets[0].Spec.Replicas = ptr(int32(3))
	f = run(t, ReplicaAnalyzer{}, s)
	if !has(f, model.Warn, "quorum") {
		t.Fatal("stateful quorum review missing")
	}
}

func TestStorageCases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*model.Snapshot)
		level  model.Severity
		reason string
	}{
		{"emptydir", func(s *model.Snapshot) {}, model.Warn, "emptyDir"},
		{"local_pv", func(s *model.Snapshot) {}, model.High, "Local Persistent"},
		{"hostpath", func(s *model.Snapshot) {
			s.Pods[0].Spec.Volumes = []core.Volume{{Name: "local", VolumeSource: core.VolumeSource{HostPath: &core.HostPathVolumeSource{Path: "/demo"}}}}
		}, model.High, "node-local filesystem"},
		{"missing_pvc", func(s *model.Snapshot) { s.PVCs = nil }, model.High, "PVC is missing"},
		{"missing_pv", func(s *model.Snapshot) { s.PVs = nil }, model.High, "PV is unavailable"},
		{"unbound", func(s *model.Snapshot) { s.PVCs[0].Status.Phase = core.ClaimPending }, model.High, "unbound"},
		{"missing_sc", func(s *model.Snapshot) { s.StorageClasses = nil }, model.High, "StorageClass"},
		{"rwx_not_safe", func(s *model.Snapshot) { s.Pods[9].Spec.NodeName = "worker-01" }, model.Info, "safety is not guaranteed"},
		{"rwo_reattach", func(s *model.Snapshot) {
			s.PVs[0].Spec.Local = nil
			s.PVs[0].Spec.CSI = &core.CSIPersistentVolumeSource{Driver: "example.invalid/csi", VolumeHandle: "demo"}
		}, model.Warn, "reattachment"},
		{"inline_csi", func(s *model.Snapshot) {
			s.Pods[0].Spec.Volumes = []core.Volume{{Name: "csi", VolumeSource: core.VolumeSource{CSI: &core.CSIVolumeSource{Driver: "example.invalid/csi"}}}}
		}, model.High, "ephemeral"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := demo.Snapshot("mixed")
			tc.change(&s)
			f := run(t, StorageAnalyzer{}, s)
			if !has(f, tc.level, tc.reason) {
				t.Fatalf("findings=%+v", f)
			}
		})
	}
}

func TestPodHealthCases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*core.Pod, time.Time)
		level  model.Severity
		reason string
	}{
		{"crashloop", func(p *core.Pod, now time.Time) {
			p.Status.ContainerStatuses[0].State = core.ContainerState{Waiting: &core.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
		}, model.High, "CrashLoopBackOff"},
		{"imagepullbackoff", func(p *core.Pod, now time.Time) {
			p.Status.ContainerStatuses[0].State = core.ContainerState{Waiting: &core.ContainerStateWaiting{Reason: "ImagePullBackOff"}}
		}, model.High, "ImagePullBackOff"},
		{"errimagepull", func(p *core.Pod, now time.Time) {
			p.Status.ContainerStatuses[0].State = core.ContainerState{Waiting: &core.ContainerStateWaiting{Reason: "ErrImagePull"}}
		}, model.High, "ErrImagePull"},
		{"oom_history", func(p *core.Pod, now time.Time) {
			p.Status.ContainerStatuses[0].LastTerminationState.Terminated = &core.ContainerStateTerminated{Reason: "OOMKilled"}
		}, model.High, "OOMKilled"},
		{"oom_current", func(p *core.Pod, now time.Time) {
			p.Status.ContainerStatuses[0].State = core.ContainerState{Terminated: &core.ContainerStateTerminated{Reason: "OOMKilled"}}
		}, model.High, "OOMKilled"},
		{"pending", func(p *core.Pod, now time.Time) { p.Status.Phase = core.PodPending }, model.High, "Pending"},
		{"unknown", func(p *core.Pod, now time.Time) { p.Status.Phase = core.PodUnknown }, model.High, "Unknown"},
		{"creating_long", func(p *core.Pod, now time.Time) {
			p.Status.ContainerStatuses[0].State = core.ContainerState{Waiting: &core.ContainerStateWaiting{Reason: "ContainerCreating"}}
		}, model.High, "ContainerCreating"},
		{"restarts", func(p *core.Pod, now time.Time) { p.Status.ContainerStatuses[0].RestartCount = 15 }, model.Warn, "restart"},
		{"terminating_long", func(p *core.Pod, now time.Time) { p.DeletionTimestamp = ptr(meta.NewTime(now.Add(-10 * time.Minute))) }, model.High, "Terminating"},
		{"unready", func(p *core.Pod, now time.Time) { p.Status.Conditions[0].Status = core.ConditionFalse }, model.High, "not Ready"},
		{"init_crash", func(p *core.Pod, now time.Time) {
			p.Status.InitContainerStatuses = []core.ContainerStatus{{Name: "init", State: core.ContainerState{Waiting: &core.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}
		}, model.High, "CrashLoopBackOff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			tc.change(s.Pods[0], s.At)
			f := run(t, PodHealthAnalyzer{}, s)
			if !has(f, tc.level, tc.reason) {
				t.Fatalf("findings=%+v", f)
			}
		})
	}
	s := fixture()
	s.Pods[0].CreationTimestamp = meta.NewTime(s.At.Add(-time.Minute))
	s.Pods[0].Status.ContainerStatuses[0].State = core.ContainerState{Waiting: &core.ContainerStateWaiting{Reason: "ContainerCreating"}}
	if f := run(t, PodHealthAnalyzer{}, s); has(f, model.High, "ContainerCreating") {
		t.Fatal("brief creating wrongly marked stuck")
	}
}

func TestEventFiltering(t *testing.T) {
	s := fixture()
	s.Events = []*core.Event{{Type: "Warning", Reason: "FailedScheduling", Message: "Insufficient CPU", LastTimestamp: meta.NewTime(s.At), InvolvedObject: core.ObjectReference{Kind: "Pod", Namespace: s.Pods[0].Namespace, Name: s.Pods[0].Name}}}
	if f := run(t, EventAnalyzer{}, s); !has(f, model.Warn, "FailedScheduling") {
		t.Fatal(f)
	}
	s.Events[0].LastTimestamp = meta.NewTime(s.At.Add(-2 * time.Hour))
	if f := run(t, EventAnalyzer{}, s); len(f) != 0 {
		t.Fatal("old event influenced readiness")
	}
	s.Events[0].LastTimestamp = meta.NewTime(s.At)
	s.Events[0].Type = "Normal"
	s.Events[0].Reason = "Pulled"
	if f := run(t, EventAnalyzer{}, s); len(f) != 0 {
		t.Fatal("normal noise not suppressed")
	}
}

type failingAnalyzer struct{}

func (failingAnalyzer) Name() string { return "test" }
func (failingAnalyzer) Analyze(context.Context, Input) ([]model.Finding, error) {
	return nil, errors.New("test failure")
}
func TestEngineConservative(t *testing.T) {
	s := fixture()
	f := Run(context.Background(), Input{Snapshot: s, Node: "worker-01"}, failingAnalyzer{})
	if !anyUnknown(f) || model.Readiness(f) != "NOT RECOMMENDED" {
		t.Fatal(f)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if f := Run(ctx, Input{Snapshot: s, Node: "worker-01"}); !anyUnknown(f) {
		t.Fatal("cancelled analysis produced safe result")
	}
	s.Issues = []string{"pods watch interrupted"}
	if f := Run(context.Background(), Input{Snapshot: s, Node: "worker-01"}); !anyUnknown(f) {
		t.Fatal("incomplete collection looked safe")
	}
}

func TestSchedulingCases(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*model.Snapshot)
		want    int
		unknown bool
	}{
		{"unconstrained", func(s *model.Snapshot) {}, 2, false},
		{"node_selector", func(s *model.Snapshot) {
			s.Pods[0].Spec.NodeSelector = map[string]string{"kubernetes.io/hostname": "worker-02"}
		}, 1, false},
		{"node_selector_no_match", func(s *model.Snapshot) { s.Pods[0].Spec.NodeSelector = map[string]string{"pool": "missing"} }, 0, false},
		{"not_ready", func(s *model.Snapshot) { s.Nodes[1].Status.Conditions[0].Status = core.ConditionFalse }, 1, false},
		{"unschedulable", func(s *model.Snapshot) { s.Nodes[1].Spec.Unschedulable = true }, 1, false},
		{"taint", func(s *model.Snapshot) {
			s.Nodes[1].Spec.Taints = []core.Taint{{Key: "dedicated", Value: "demo", Effect: core.TaintEffectNoSchedule}}
		}, 1, false},
		{"equal_toleration", func(s *model.Snapshot) {
			s.Nodes[1].Spec.Taints = []core.Taint{{Key: "dedicated", Value: "demo", Effect: core.TaintEffectNoSchedule}}
			s.Pods[0].Spec.Tolerations = []core.Toleration{{Key: "dedicated", Value: "demo", Effect: core.TaintEffectNoSchedule}}
		}, 2, false},
		{"exists_toleration", func(s *model.Snapshot) {
			s.Nodes[1].Spec.Taints = []core.Taint{{Key: "dedicated", Value: "demo", Effect: core.TaintEffectNoExecute}}
			s.Pods[0].Spec.Tolerations = []core.Toleration{{Operator: core.TolerationOpExists}}
		}, 2, false},
		{"wrong_effect", func(s *model.Snapshot) {
			s.Nodes[1].Spec.Taints = []core.Taint{{Key: "dedicated", Effect: core.TaintEffectNoSchedule}}
			s.Pods[0].Spec.Tolerations = []core.Toleration{{Key: "dedicated", Operator: core.TolerationOpExists, Effect: core.TaintEffectNoExecute}}
		}, 1, false},
		{"prefer_no_schedule_soft", func(s *model.Snapshot) {
			s.Nodes[1].Spec.Taints = []core.Taint{{Key: "dedicated", Effect: core.TaintEffectPreferNoSchedule}}
		}, 2, false},
		{"required_node_affinity", func(s *model.Snapshot) {
			s.Pods[0].Spec.Affinity = &core.Affinity{NodeAffinity: &core.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &core.NodeSelector{NodeSelectorTerms: []core.NodeSelectorTerm{{MatchExpressions: []core.NodeSelectorRequirement{{Key: "kubernetes.io/hostname", Operator: core.NodeSelectorOpIn, Values: []string{"worker-02"}}}}}}}}
		}, 1, false},
		{"required_pod_affinity", func(s *model.Snapshot) {
			s.Pods[0].Spec.Affinity = &core.Affinity{PodAffinity: &core.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{{TopologyKey: "zone"}}}}
		}, 0, false},
		{"required_anti_affinity", func(s *model.Snapshot) {
			s.Pods[0].Spec.Affinity = &core.Affinity{PodAntiAffinity: &core.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{{TopologyKey: "zone"}}}}
		}, 2, true},
		{"topology_spread", func(s *model.Snapshot) {
			s.Pods[0].Spec.TopologySpreadConstraints = []core.TopologySpreadConstraint{{WhenUnsatisfiable: core.DoNotSchedule, TopologyKey: "zone"}}
		}, 2, true},
		{"custom_scheduler", func(s *model.Snapshot) { s.Pods[0].Spec.SchedulerName = "custom" }, 2, true},
		{"extended_resource", func(s *model.Snapshot) {
			s.Pods[0].Spec.Containers[0].Resources.Requests["example.invalid/gpu"] = resource.MustParse("1")
		}, 2, true},
		{"host_port", func(s *model.Snapshot) { s.Pods[0].Spec.Containers[0].Ports = []core.ContainerPort{{HostPort: 8080}} }, 2, true},
		{"sidecar_host_port", func(s *model.Snapshot) {
			s.Pods[0].Spec.InitContainers = []core.Container{{Name: "sidecar", RestartPolicy: ptr(core.ContainerRestartPolicyAlways), Ports: []core.ContainerPort{{ContainerPort: 8080, HostPort: 8080}}}}
		}, 2, true},
		{"host_network", func(s *model.Snapshot) { s.Pods[0].Spec.HostNetwork = true }, 2, true},
		{"template_pin", func(s *model.Snapshot) { s.Deployments[0].Spec.Template.Spec.NodeName = "worker-01" }, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			tc.change(&s)
			f := Candidates(s, s.Pods[0], "worker-01")
			if len(f.Nodes) != tc.want || (len(f.Unknown) > 0) != tc.unknown {
				t.Fatalf("candidates=%+v", f)
			}
			findings := run(t, SchedulingAnalyzer{}, s)
			if anyUnknown(findings) != tc.unknown {
				t.Fatal(findings)
			}
			if tc.name == "sidecar_host_port" && CalculateCapacity(Input{Snapshot: s, Node: "worker-01"}).Assessment != "UNKNOWN" {
				t.Fatal("sidecar host port allowed a positive capacity recommendation")
			}
		})
	}
	s := demo.Snapshot("mixed")
	for _, p := range s.Pods {
		if p.Name == "redis-0" {
			f := Candidates(s, p, "worker-01")
			if len(f.Nodes) != 0 || len(f.Unknown) == 0 {
				t.Fatalf("local PV: %+v", f)
			}
		}
	}
}

func TestDiagnosisMatchesIndependentChecks(t *testing.T) {
	for _, scenario := range []string{"healthy", "mixed", "incomplete"} {
		t.Run(scenario, func(t *testing.T) {
			s := demo.Snapshot(scenario)
			if scenario == "incomplete" {
				s.Issues = []string{"watch failed"}
			}
			in := Input{Snapshot: s, Node: "worker-01"}
			d := Diagnose(context.Background(), in)
			if !reflect.DeepEqual(d.Findings, Run(context.Background(), in)) || !reflect.DeepEqual(d.Capacity, CalculateCapacity(in)) {
				t.Fatal("shared evidence changed diagnosis or capacity")
			}
		})
	}
	s := fixture()
	in := Input{Snapshot: s, Node: "worker-01"}
	before := Diagnose(context.Background(), in)
	s.Pods[0].Spec.Containers[0].Resources.Requests[core.ResourceCPU] = resource.MustParse("100")
	after := Diagnose(context.Background(), Input{Snapshot: s, Node: "worker-01"})
	if before.Capacity.Assessment != "PASS" || after.Capacity.Assessment != "HIGH" {
		t.Fatal("capacity leaked across snapshots")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := Diagnose(ctx, in)
	if d.Capacity.Assessment != "UNKNOWN" || !anyUnknown(d.Findings) || model.Readiness(d.Findings) != "NOT RECOMMENDED" {
		t.Fatal("cancelled diagnosis was positive")
	}
}

func TestSchedulingUnknownReasonsAreUnique(t *testing.T) {
	s := fixture()
	s.Pods[0].Spec.Containers[0].Ports = []core.ContainerPort{{HostPort: 8080}, {HostPort: 8081}}
	s.Pods[0].Spec.InitContainers = []core.Container{{Name: "sidecar", RestartPolicy: ptr(core.ContainerRestartPolicyAlways), Ports: []core.ContainerPort{{HostPort: 8082}}}}
	f := Candidates(s, s.Pods[0], "worker-01")
	if len(f.Unknown) != 1 || f.Unknown[0] != "hostPort availability" {
		t.Fatalf("duplicate or missing reasons: %+v", f)
	}
}

func TestCapacity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*model.Snapshot)
		want   string
	}{
		{"sufficient", func(s *model.Snapshot) {}, "PASS"},
		{"cpu_insufficient", func(s *model.Snapshot) {
			s.Pods[0].Spec.Containers[0].Resources.Requests[core.ResourceCPU] = resource.MustParse("100")
		}, "HIGH"},
		{"memory_insufficient", func(s *model.Snapshot) {
			s.Pods[0].Spec.Containers[0].Resources.Requests[core.ResourceMemory] = resource.MustParse("100Gi")
		}, "HIGH"},
		{"pod_slots", func(s *model.Snapshot) {
			for _, n := range s.Nodes {
				n.Status.Allocatable[core.ResourcePods] = resource.MustParse("0")
			}
		}, "HIGH"},
		{"all_ineligible", func(s *model.Snapshot) { s.Nodes[1].Spec.Unschedulable = true; s.Nodes[2].Spec.Unschedulable = true }, "HIGH"},
		{"unknown_constraints", func(s *model.Snapshot) { s.Pods[0].Spec.SchedulerName = "custom" }, "UNKNOWN"},
		{"missing_requests", func(s *model.Snapshot) { s.Pods[0].Spec.Containers[0].Resources.Requests = nil }, "UNKNOWN"},
		{"single_pod_fragmentation", func(s *model.Snapshot) {
			s.Pods[0].Spec.Containers[0].Resources.Requests[core.ResourceCPU] = resource.MustParse("10")
		}, "HIGH"},
		{"no_relocation", func(s *model.Snapshot) { s.Pods = nil }, "PASS"},
		{"incomplete_collection", func(s *model.Snapshot) { s.Issues = []string{"watch error"} }, "UNKNOWN"},
		{"missing_target", func(s *model.Snapshot) { s.Nodes = s.Nodes[1:] }, "UNKNOWN"},
		{"empty_incomplete_snapshot", func(s *model.Snapshot) {
			s.Pods = nil
			s.Issues = []string{"watch error"}
		}, "UNKNOWN"},
		{"pending_demand", func(s *model.Snapshot) { s.Pods[1].Spec.NodeName = "" }, "UNKNOWN"},
		{"terminal_unscheduled_ignored", func(s *model.Snapshot) {
			s.Pods[1].Spec.NodeName = ""
			s.Pods[1].Status.Phase = core.PodSucceeded
		}, "PASS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			tc.change(&s)
			c := CalculateCapacity(Input{Snapshot: s, Node: "worker-01"})
			if c.Assessment != tc.want {
				t.Fatalf("%+v", c)
			}
			if tc.name == "pending_demand" && (c.Pending.Pods != 1 || !strings.Contains(c.Detail, "Unscheduled demand")) {
				t.Fatalf("pending demand lost from evidence: %+v", c)
			}
			run(t, CapacityAnalyzer{}, s)
		})
	}
	t.Run("shared_candidate_not_double_counted", func(t *testing.T) {
		s := fixture()
		s.Pods[1].Spec.NodeName = "worker-01"
		c := CalculateCapacity(Input{Snapshot: s, Node: "worker-01"})
		if len(c.Eligible) != 2 || c.Free.CPU != 16000 || c.Demand.Pods != 2 {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("overlapping_candidate_packing_unknown", func(t *testing.T) {
		s := fixture()
		s.Pods[1].Spec.NodeName = "worker-01"
		for _, p := range s.Pods {
			p.Spec.Containers[0].Resources.Requests[core.ResourceCPU] = resource.MustParse("5")
			p.Spec.NodeSelector = map[string]string{"kubernetes.io/hostname": "worker-02"}
		}
		extra := s.Pods[0].DeepCopy()
		extra.Name = "extra"
		extra.Spec.NodeSelector = map[string]string{"kubernetes.io/hostname": "worker-03"}
		extra.Spec.Containers[0].Resources.Requests[core.ResourceCPU] = resource.MustParse("1")
		s.Pods = append(s.Pods, extra)
		c := CalculateCapacity(Input{Snapshot: s, Node: "worker-01"})
		if c.Assessment != "UNKNOWN" {
			t.Fatalf("%+v", c)
		}
	})
}
