package analyzer

import (
	"testing"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func namespaceFixture() model.Snapshot {
	s := affinityFixture()
	s.Namespaces = []*core.Namespace{
		{ObjectMeta: meta.ObjectMeta{Name: "demo", Labels: map[string]string{"team": "red", "enabled": "yes"}}},
		{ObjectMeta: meta.ObjectMeta{Name: "blue", Labels: map[string]string{"team": "blue", "enabled": "yes"}}},
		{ObjectMeta: meta.ObjectMeta{Name: "unlabeled"}},
	}
	s.Pods[1].Namespace = "blue"
	return s
}

func TestNamespaceSelectorSemantics(t *testing.T) {
	for _, tc := range []struct {
		name           string
		namespaces     []string
		selector       *meta.LabelSelector
		namespace      string
		state          model.CapabilityState
		match, unknown bool
	}{
		{"default_current", nil, nil, "demo", model.Available, true, false},
		{"default_other", nil, nil, "blue", model.Available, false, false},
		{"empty_list_current", []string{}, nil, "demo", model.Available, true, false},
		{"explicit_other", []string{"blue"}, nil, "blue", model.Available, true, false},
		{"explicit_excludes_current", []string{"blue"}, nil, "demo", model.Available, false, false},
		{"empty_selector_all", nil, &meta.LabelSelector{}, "blue", model.Available, true, false},
		{"empty_selector_no_namespace_labels_needed", nil, &meta.LabelSelector{}, "blue", model.Forbidden, true, false},
		{"labels_match", nil, &meta.LabelSelector{MatchLabels: map[string]string{"team": "blue"}}, "blue", model.Available, true, false},
		{"labels_do_not_match", nil, &meta.LabelSelector{MatchLabels: map[string]string{"team": "red"}}, "blue", model.Available, false, false},
		{"union_explicit", []string{"blue"}, &meta.LabelSelector{MatchLabels: map[string]string{"team": "red"}}, "blue", model.Available, true, false},
		{"union_selector", []string{"demo"}, &meta.LabelSelector{MatchLabels: map[string]string{"team": "blue"}}, "blue", model.Available, true, false},
		{"union_proof_without_labels", []string{"blue"}, &meta.LabelSelector{MatchLabels: map[string]string{"team": "red"}}, "blue", model.Forbidden, true, false},
		{"missing_namespace", nil, &meta.LabelSelector{MatchLabels: map[string]string{"team": "blue"}}, "absent", model.Available, false, true},
		{"incomplete_collection", nil, &meta.LabelSelector{MatchLabels: map[string]string{"team": "blue"}}, "blue", model.Unavailable, false, true},
		{"forbidden_collection", nil, &meta.LabelSelector{MatchLabels: map[string]string{"team": "blue"}}, "blue", model.Forbidden, false, true},
		{"unlabeled_namespace", nil, &meta.LabelSelector{MatchLabels: map[string]string{"team": "blue"}}, "unlabeled", model.Available, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := namespaceFixture()
			s.Capabilities = map[string]model.Capability{model.NamespacesResource: {State: tc.state}}
			term := affinityRequirement()
			term.Namespaces = tc.namespaces
			term.NamespaceSelector = tc.selector
			p := s.Pods[1].DeepCopy()
			p.Namespace = tc.namespace
			a := newSchedulingEvidence(s.WithLookups()).compile(s, term, "demo")
			match, u := a.matches(p)
			if match != tc.match || u != tc.unknown {
				t.Fatalf("match=%t unknown=%t", match, u)
			}
		})
	}
	for _, op := range []meta.LabelSelectorOperator{meta.LabelSelectorOpIn, meta.LabelSelectorOpNotIn, meta.LabelSelectorOpExists, meta.LabelSelectorOpDoesNotExist} {
		t.Run(string(op), func(t *testing.T) {
			s := namespaceFixture()
			term := affinityRequirement()
			r := meta.LabelSelectorRequirement{Key: "team", Operator: op}
			if op == meta.LabelSelectorOpIn {
				r.Values = []string{"blue"}
			}
			if op == meta.LabelSelectorOpNotIn {
				r.Values = []string{"red"}
			}
			term.NamespaceSelector = &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{r}}
			a := newSchedulingEvidence(s).compile(s, term, "demo")
			match, u := a.matches(s.Pods[1])
			if u || match != (op != meta.LabelSelectorOpDoesNotExist) {
				t.Fatal(match, u)
			}
		})
	}
}

func TestCrossNamespacePlacementAndCapabilityChange(t *testing.T) {
	for _, kind := range []string{"affinity", "incoming-anti", "existing-anti"} {
		t.Run(kind, func(t *testing.T) {
			s := namespaceFixture()
			term := affinityRequirement()
			term.NamespaceSelector = &meta.LabelSelector{MatchLabels: map[string]string{"team": "blue"}}
			want := "REJECTED"
			switch kind {
			case "affinity":
				s.Pods[0].Spec.Affinity = &core.Affinity{PodAffinity: &core.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{term}}}
				want = "ALLOWED"
			case "incoming-anti":
				s.Pods[0].Spec.Affinity = &core.Affinity{PodAntiAffinity: &core.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{term}}}
			case "existing-anti":
				term.LabelSelector = &meta.LabelSelector{MatchLabels: map[string]string{"app": "incoming"}}
				term.NamespaceSelector.MatchLabels["team"] = "red"
				s.Pods[1].Spec.Affinity = &core.Affinity{PodAntiAffinity: &core.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{term}}}
			}
			if f := Candidates(s, s.Pods[0], "worker-01"); status(f, "worker-02") != want {
				t.Fatal(f)
			}
			// A new immutable snapshot must not reuse Available namespace evidence.
			s.Capabilities = map[string]model.Capability{model.NamespacesResource: {State: model.Forbidden}}
			if f := Candidates(s, s.Pods[0], "worker-01"); status(f, "worker-02") != "UNKNOWN" {
				t.Fatalf("stale namespace evidence: %+v", f)
			}
			s.Capabilities = map[string]model.Capability{model.NamespacesResource: {State: model.Available}}
			s.Namespaces = nil
			if f := Candidates(s, s.Pods[0], "worker-01"); status(f, "worker-02") != "UNKNOWN" {
				t.Fatalf("missing namespaces treated as an empty selector: %+v", f)
			}
		})
	}
}

func TestCoreNamespaceFailureCannotAllowUnconstrainedPod(t *testing.T) {
	s := namespaceFixture()
	s.Pods[0].Spec.Affinity = nil
	s.Capabilities = map[string]model.Capability{model.NamespacesResource: {State: model.Forbidden}}
	if f := Candidates(s, s.Pods[0], "worker-01"); status(f, "worker-02") != "UNKNOWN" {
		t.Fatal("core collection failure produced confirmed candidate", f)
	}
}
