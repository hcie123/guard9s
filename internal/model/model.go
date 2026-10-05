// Package model contains immutable snapshots shared by collection, analysis and UI.
package model

import (
	"fmt"
	"sort"
	"time"

	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	storage "k8s.io/api/storage/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	podresource "k8s.io/component-helpers/resource"
)

type Severity int

const (
	Pass Severity = iota
	Info
	Warn
	High
	Critical
)

func (s Severity) String() string {
	if s < Pass || s > Critical {
		return "UNKNOWN"
	}
	return [...]string{"PASS", "INFO", "WARN", "HIGH", "CRITICAL"}[s]
}

type Finding struct {
	Severity                                                        Severity
	ResourceKind                                                    string
	Category, Namespace, Resource, Reason, Evidence, Recommendation string
	Unknown                                                         bool
}

func SortFindings(f []Finding) {
	sort.SliceStable(f, func(i, j int) bool {
		if f[i].Severity != f[j].Severity {
			return f[i].Severity > f[j].Severity
		}
		return f[i].Category+f[i].Namespace+f[i].Resource+f[i].Reason < f[j].Category+f[j].Namespace+f[j].Resource+f[j].Reason
	})
}

func Readiness(f []Finding) string {
	state := "READY"
	for _, x := range f {
		if x.Severity == Critical {
			return "BLOCKED"
		}
		if x.Severity >= High || x.Unknown {
			state = "NOT RECOMMENDED"
		}
		if x.Severity == Warn && state == "READY" {
			state = "READY WITH WARNINGS"
		}
	}
	return state
}

type Snapshot struct {
	index              *snapshotIndex
	KubeconfigPath     string // Path only; never kubeconfig contents.
	Context, Namespace string // Namespace is a display filter; collection remains cluster-wide.
	At                 time.Time
	Issues             []string
	Capabilities       map[string]Capability
	Namespaces         []*core.Namespace
	CSINodes           []*storage.CSINode
	VolumeAttachments  []*storage.VolumeAttachment
	Nodes              []*core.Node
	Pods               []*core.Pod
	Deployments        []*apps.Deployment
	ReplicaSets        []*apps.ReplicaSet
	StatefulSets       []*apps.StatefulSet
	DaemonSets         []*apps.DaemonSet
	Jobs               []*batch.Job
	CronJobs           []*batch.CronJob
	PDBs               []*policy.PodDisruptionBudget
	PVCs               []*core.PersistentVolumeClaim
	PVs                []*core.PersistentVolume
	StorageClasses     []*storage.StorageClass
	Events             []*core.Event
}

func (s Snapshot) Node(name string) *core.Node {
	if s.index != nil {
		return s.index.nodes[name]
	}
	for _, n := range s.Nodes {
		if n.Name == name {
			return n
		}
	}
	return nil
}

func Active(p *core.Pod) bool {
	return p.Status.Phase != core.PodSucceeded && p.Status.Phase != core.PodFailed
}
func Ready(p *core.Pod) bool {
	if !Active(p) || p.DeletionTimestamp != nil {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == core.PodReady {
			return c.Status == core.ConditionTrue
		}
	}
	return false
}
func Condition(n *core.Node, typ core.NodeConditionType) core.ConditionStatus {
	for _, c := range n.Status.Conditions {
		if c.Type == typ {
			return c.Status
		}
	}
	return core.ConditionUnknown
}
func Eligible(n *core.Node) bool {
	if n.Spec.Unschedulable || Condition(n, core.NodeReady) != core.ConditionTrue {
		return false
	}
	for _, c := range []core.NodeConditionType{core.NodeMemoryPressure, core.NodeDiskPressure, core.NodePIDPressure, core.NodeNetworkUnavailable} {
		if Condition(n, c) == core.ConditionTrue {
			return false
		}
	}
	return true
}
func (s Snapshot) NodePods(name string) []*core.Pod {
	if s.index != nil {
		return s.index.podsByNode[name]
	}
	var out []*core.Pod
	for _, p := range s.Pods {
		if p.Spec.NodeName == name && Active(p) {
			out = append(out, p)
		}
	}
	return out
}

type Resources struct{ CPU, Memory, Pods int64 }

func (r Resources) Add(b Resources) Resources {
	return Resources{r.CPU + b.CPU, r.Memory + b.Memory, r.Pods + b.Pods}
}
func (r Resources) Sub(b Resources) Resources {
	return Resources{r.CPU - b.CPU, r.Memory - b.Memory, r.Pods - b.Pods}
}
func (r Resources) Fits(b Resources) bool {
	return r.CPU >= b.CPU && r.Memory >= b.Memory && r.Pods >= b.Pods
}
func (r Resources) String() string {
	return fmt.Sprintf("CPU %.2f cores | memory %.2f GiB | pods %d", float64(r.CPU)/1000, float64(r.Memory)/(1<<30), r.Pods)
}
func Requests(p *core.Pod) Resources {
	// Upstream helper accounts for init/sidecar containers, overhead, pod-level
	// requests and allocated resources during in-place resize.
	r := podresource.PodRequests(p, podresource.PodResourcesOptions{UseStatusResources: true, InPlacePodLevelResourcesVerticalScalingEnabled: true})
	return Resources{r.Cpu().MilliValue(), r.Memory().Value(), 1}
}
func Allocatable(n *core.Node) Resources {
	return Resources{n.Status.Allocatable.Cpu().MilliValue(), n.Status.Allocatable.Memory().Value(), n.Status.Allocatable.Pods().Value()}
}
func (s Snapshot) Used(node string) Resources {
	if s.index != nil && s.index.used != nil {
		return s.index.used[node]
	}
	var r Resources
	for _, p := range s.NodePods(node) {
		r = r.Add(Requests(p))
	}
	return r
}
func (s Snapshot) Free(n *core.Node) Resources {
	r := Allocatable(n).Sub(s.Used(n.Name))
	r.CPU = max(0, r.CPU)
	r.Memory = max(0, r.Memory)
	r.Pods = max(0, r.Pods)
	return r
}

type Owner struct {
	Kind, Name, UID         string
	Replicas, ReadyReplicas int32
	Resolved                bool
}

func referenceMatches(obj meta.Object, r *meta.OwnerReference) bool {
	return obj.GetName() == r.Name && (r.UID == "" || obj.GetUID() == r.UID)
}
func (s Snapshot) Owner(p *core.Pod) Owner {
	if s.index != nil {
		if o, ok := s.index.owners[p]; ok {
			return o
		}
	}
	if _, ok := p.Annotations[core.MirrorPodAnnotationKey]; ok {
		return Owner{Kind: "StaticPod", Name: p.Name, Resolved: true}
	}
	r := meta.GetControllerOfNoCopy(p)
	if r == nil {
		return Owner{Kind: "Standalone", Name: p.Name, Replicas: 1, Resolved: true}
	}
	o := Owner{Kind: r.Kind, Name: r.Name, UID: string(r.UID)}
	if r.Kind == "ReplicaSet" {
		for _, rs := range s.ReplicaSets {
			if rs.Namespace == p.Namespace && referenceMatches(rs, r) {
				if parent := meta.GetControllerOfNoCopy(rs); parent != nil && parent.Kind == "Deployment" {
					r = parent
					o = Owner{Kind: r.Kind, Name: r.Name, UID: string(r.UID)}
				} else {
					o.Replicas = value(rs.Spec.Replicas)
					o.ReadyReplicas = rs.Status.ReadyReplicas
					o.Resolved = true
				}
				break
			}
		}
	}
	switch o.Kind {
	case "Deployment":
		for _, x := range s.Deployments {
			if x.Namespace == p.Namespace && referenceMatches(x, r) {
				o.Replicas = value(x.Spec.Replicas)
				o.ReadyReplicas = x.Status.ReadyReplicas
				o.Resolved = true
				break
			}
		}
	case "StatefulSet":
		for _, x := range s.StatefulSets {
			if x.Namespace == p.Namespace && referenceMatches(x, r) {
				o.Replicas = value(x.Spec.Replicas)
				o.ReadyReplicas = x.Status.ReadyReplicas
				o.Resolved = true
				break
			}
		}
	case "DaemonSet":
		for _, x := range s.DaemonSets {
			if x.Namespace == p.Namespace && referenceMatches(x, r) {
				o.Replicas = x.Status.DesiredNumberScheduled
				o.ReadyReplicas = x.Status.NumberReady
				o.Resolved = true
				break
			}
		}
	case "Job":
		for _, x := range s.Jobs {
			if x.Namespace == p.Namespace && referenceMatches(x, r) {
				o.Resolved = true
				if parent := meta.GetControllerOfNoCopy(x); parent != nil && parent.Kind == "CronJob" {
					o.Kind = "CronJob"
					o.Name = parent.Name
					o.UID = string(parent.UID)
					o.Resolved = false
					for _, cj := range s.CronJobs {
						if cj.Namespace == p.Namespace && referenceMatches(cj, parent) {
							o.Resolved = true
						}
					}
				}
				break
			}
		}
	}
	return o
}
func value(p *int32) int32 {
	if p == nil {
		return 1
	}
	return *p
}
func Movable(s Snapshot, p *core.Pod) bool {
	o := s.Owner(p)
	return Active(p) && o.Kind != "DaemonSet" && o.Kind != "StaticPod"
}
func MatchingPDBs(s Snapshot, p *core.Pod) ([]*policy.PodDisruptionBudget, error) {
	if s.index != nil {
		var out []*policy.PodDisruptionBudget
		for _, b := range s.index.budgets[p.Namespace] {
			if b.err != nil {
				return nil, b.err
			}
			if b.selector.Matches(labels.Set(p.Labels)) {
				out = append(out, b.pdb)
			}
		}
		return out, nil
	}
	var out []*policy.PodDisruptionBudget
	for _, b := range s.PDBs {
		if b.Namespace != p.Namespace {
			continue
		}
		selector, err := meta.LabelSelectorAsSelector(b.Spec.Selector)
		if err != nil {
			return nil, err
		}
		if selector.Matches(labels.Set(p.Labels)) {
			out = append(out, b)
		}
	}
	return out, nil
}
func (s Snapshot) PVC(namespace, name string) *core.PersistentVolumeClaim {
	if s.index != nil {
		return s.index.pvcs[namespace+"/"+name]
	}
	for _, x := range s.PVCs {
		if x.Namespace == namespace && x.Name == name {
			return x
		}
	}
	return nil
}
func (s Snapshot) PV(name string) *core.PersistentVolume {
	if s.index != nil {
		return s.index.pvs[name]
	}
	for _, x := range s.PVs {
		if x.Name == name {
			return x
		}
	}
	return nil
}

func RelevantEvents(s Snapshot, node string) []*core.Event {
	objects := map[string]bool{"Node//" + node: true}
	for _, p := range s.Pods {
		if p.Spec.NodeName == node {
			objects["Pod/"+p.Namespace+"/"+p.Name] = true
		}
	}
	important := map[string]bool{"FailedScheduling": true, "FailedMount": true, "FailedAttachVolume": true, "FailedCreatePodSandBox": true, "NetworkNotReady": true, "OOMKilled": true, "BackOff": true, "Unhealthy": true, "Evicted": true, "NodeNotReady": true, "FailedKillPod": true}
	var out []*core.Event
	for _, e := range s.Events {
		if (e.Type == core.EventTypeWarning || important[e.Reason]) && (node == "" || objects[e.InvolvedObject.Kind+"/"+e.InvolvedObject.Namespace+"/"+e.InvolvedObject.Name]) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return EventTime(out[i]).After(EventTime(out[j])) })
	return out
}
func EventTime(e *core.Event) time.Time {
	if e.Series != nil {
		return e.Series.LastObservedTime.Time
	}
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}
