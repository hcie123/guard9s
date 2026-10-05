package model_test

import (
	"reflect"
	"testing"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIndexMatchesSnapshotAndRefresh(t *testing.T) {
	s := demo.Snapshot("mixed")
	indexed := s.Indexed()
	lookup := s.WithLookups()
	if lookup.WithLookups().Node("worker-01") != s.Nodes[0] || lookup.Used("worker-01") != s.Used("worker-01") || lookup.PendingRequests() != s.PendingRequests() || !reflect.DeepEqual(lookup.OwnerPods(s.Pods[0].Namespace, s.Owner(s.Pods[0])), s.OwnerPods(s.Pods[0].Namespace, s.Owner(s.Pods[0]))) {
		t.Fatal("lookup-only cache changed resource evidence")
	}
	if !reflect.DeepEqual(indexed, indexed.Indexed()) {
		t.Fatal("index rebuilt")
	}
	for _, n := range s.Nodes {
		if indexed.Node(n.Name) != n || !reflect.DeepEqual(indexed.NodePods(n.Name), s.NodePods(n.Name)) || indexed.Used(n.Name) != s.Used(n.Name) || indexed.Free(n) != s.Free(n) {
			t.Fatal(n.Name)
		}
	}
	for _, p := range s.Pods {
		o := s.Owner(p)
		if indexed.Owner(p) != o || indexed.PodRequests(p) != model.Requests(p) || !reflect.DeepEqual(indexed.OwnerPods(p.Namespace, o), s.OwnerPods(p.Namespace, o)) {
			t.Fatal(p.Name)
		}
		a, err := model.MatchingPDBs(s, p)
		b, otherErr := model.MatchingPDBs(indexed, p)
		if !reflect.DeepEqual(a, b) || err != otherErr {
			t.Fatal("PDB mismatch")
		}
	}
	for _, p := range s.PVCs {
		if indexed.PVC(p.Namespace, p.Name) != p {
			t.Fatal(p.Name)
		}
	}
	for _, p := range s.PVs {
		if indexed.PV(p.Name) != p {
			t.Fatal(p.Name)
		}
	}
	if indexed.Node("absent") != nil || indexed.PVC("demo", "absent") != nil || indexed.PV("absent") != nil || indexed.PendingRequests() != s.PendingRequests() {
		t.Fatal("lookup or pending mismatch")
	}
	other := demo.Snapshot("healthy")
	other.Pods[0].Spec.NodeName = ""
	fresh := other.Indexed()
	if fresh.Node("worker-01") == indexed.Node("worker-01") || fresh.PendingRequests().Pods != 1 || indexed.PendingRequests().Pods != 1 {
		t.Fatal("cross-snapshot evidence reuse")
	}
	external := other.Pods[0].DeepCopy()
	external.Status.Phase = core.PodSucceeded
	if fresh.PodRequests(external) != model.Requests(external) || !fresh.Owner(external).Resolved {
		t.Fatal("fallback lookup")
	}
}

func TestIndexedRequestsDoNotLeakReusedResources(t *testing.T) {
	s := demo.Snapshot("healthy")
	s.Pods = s.Pods[:4]
	s.Pods[0].Spec.InitContainers = []core.Container{{Name: "init", Resources: core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("2"), core.ResourceMemory: resource.MustParse("1Gi")}}}}
	s.Pods[1].Spec.InitContainers = []core.Container{{Name: "sidecar", RestartPolicy: ptr(core.ContainerRestartPolicyAlways), Resources: core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("500m"), core.ResourceMemory: resource.MustParse("128Mi")}}}}
	s.Pods[1].Spec.Overhead = core.ResourceList{core.ResourceCPU: resource.MustParse("100m"), core.ResourceMemory: resource.MustParse("64Mi")}
	s.Pods[2].Spec.Resources = &core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("1"), core.ResourceMemory: resource.MustParse("1Gi")}}
	s.Pods[3].Spec.Containers[0].Resources.Requests = nil
	s.Pods[3].Spec.NodeName = ""
	indexed := s.Indexed()
	for _, p := range s.Pods {
		if got, want := indexed.PodRequests(p), model.Requests(p); got != want {
			t.Fatalf("resource reuse changed %s: got %+v, want %+v", p.Name, got, want)
		}
	}
	if indexed.PendingRequests() != (model.Resources{Pods: 1}) || indexed.Used("worker-01") != s.Used("worker-01") {
		t.Fatal("previous pod requests leaked into empty requests or node totals")
	}
}
func TestIndexTerminalAndInvalidPDB(t *testing.T) {
	s := demo.Snapshot("healthy")
	s.Pods[0].Status.Phase = core.PodSucceeded
	s.PDBs[0].Spec.Selector = &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{{Key: "app", Operator: "invalid"}}}
	indexed := s.Indexed()
	if len(indexed.NodePods("worker-01")) != len(s.NodePods("worker-01")) {
		t.Fatal("terminal counted")
	}
	if _, err := model.MatchingPDBs(indexed, s.Pods[1]); err == nil {
		t.Fatal("invalid selector accepted")
	}
}
