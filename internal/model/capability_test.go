package model

import (
	"testing"

	core "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCapabilityInventoryAndLookupParity(t *testing.T) {
	s := Snapshot{Namespaces: []*core.Namespace{{ObjectMeta: meta.ObjectMeta{Name: "synthetic"}}}, CSINodes: []*storage.CSINode{{ObjectMeta: meta.ObjectMeta{Name: "worker"}}}, Pods: []*core.Pod{{ObjectMeta: meta.ObjectMeta{Name: "pod", Namespace: "synthetic"}}}}
	if s.Capability(NamespacesResource).State != Unavailable {
		t.Fatal("object presence silently proved complete collection")
	}
	s.Capabilities = map[string]Capability{NamespacesResource: {State: Available}, CSINodesResource: {State: Forbidden}}
	for _, candidate := range []Snapshot{s, s.WithLookups(), s.Indexed()} {
		if candidate.NamespaceObject("synthetic") == nil || candidate.CSINode("worker") == nil || len(candidate.NamespacePods("synthetic")) != 1 {
			t.Fatal("lookup lost inventory")
		}
		if candidate.NamespaceObject("absent") != nil || candidate.CSINode("absent") != nil || len(candidate.NamespacePods("absent")) != 0 {
			t.Fatal("invented objects")
		}
		if candidate.Capability(CSINodesResource).State != Forbidden {
			t.Fatal("capability lost during indexing")
		}
	}
}
