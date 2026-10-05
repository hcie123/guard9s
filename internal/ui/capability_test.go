package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type capabilitySource struct {
	calls int
	fail  bool
}

func (s *capabilitySource) Snapshot(context.Context) (model.Snapshot, error) {
	s.calls++
	if s.fail {
		return model.Snapshot{}, errors.New("synthetic refresh failure")
	}
	out := demo.Snapshot("healthy")
	out.Namespaces[0].Labels = map[string]string{"team": "synthetic"}
	out.Pods[0].Spec.Affinity = &core.Affinity{PodAffinity: &core.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{{TopologyKey: core.LabelHostname, LabelSelector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "nginx"}}, NamespaceSelector: &meta.LabelSelector{MatchLabels: map[string]string{"team": "synthetic"}}}}}}
	if s.calls > 1 {
		out.Capabilities[model.NamespacesResource] = model.Capability{State: model.Forbidden}
	}
	return out, nil
}

func TestRefreshCapabilityChangeWithdrawsAllowedEvidence(t *testing.T) {
	previous := demo.Snapshot("healthy")
	source := &capabilitySource{}
	first := collectResult(context.Background(), source, previous)
	second := collectResult(context.Background(), source, first.snapshot)
	for _, tc := range []struct {
		name string
		r    result
		want string
	}{{"available", first, "ALLOWED"}, {"forbidden", second, "UNKNOWN"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.r.candidates["worker-01"]["demo/nginx-1-01"]
			for _, n := range f.Evaluations {
				if n.Status != tc.want {
					t.Fatalf("stale candidate %s: %s", n.Node, n.Status)
				}
			}
			if len(f.Evaluations) != 2 {
				t.Fatal("candidate detail lost")
			}
		})
	}
	if first.snapshot.Capability(model.NamespacesResource).State != model.Available {
		t.Fatal("refresh mutated prior capabilities")
	}
	source.fail = true
	failed := collectResult(context.Background(), source, first.snapshot)
	if failed.snapshot.Capability(model.NamespacesResource).State != model.Unavailable || failed.err == nil || !strings.Contains(strings.Join(failed.snapshot.Issues, " "), "Latest refresh failed") {
		t.Fatal("failed refresh retained Available capability")
	}
}
