package analyzer

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/hcie123/guard9s/internal/model"
	"github.com/hcie123/guard9s/internal/testfixture"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This oracle directly scans observed peers. It shares selector parsing, which
// has separate truth-table tests, but never uses aggregate/projected counters.
func scanAffinity(s model.Snapshot, p *core.Pod, target string, x *schedulingEvidence) affinityEvidence {
	a := affinityEvidence{}
	var terms []affinityTerm
	if f := p.Spec.Affinity; f != nil {
		if f.PodAffinity != nil {
			for _, raw := range f.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
				t := x.compile(s, raw, p.Namespace)
				terms = append(terms, t)
				a.affinity = append(a.affinity, domain(t))
			}
		}
		if f.PodAntiAffinity != nil {
			for _, raw := range f.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
				a.anti = append(a.anti, domain(x.compile(s, raw, p.Namespace)))
			}
		}
	}
	a.dynamic = len(a.affinity)+len(a.anti) > 0
	add := func(d *domainTerm, other *core.Pod, u, affinity bool) {
		n := s.Node(other.Spec.NodeName)
		if n == nil {
			d.allUnknown = true
			return
		}
		v, ok := n.Labels[d.term.raw.TopologyKey]
		if !ok {
			d.allUnknown = true
			return
		}
		if u || !model.Active(other) || (affinity && other.DeletionTimestamp != nil) {
			d.uncertain[v] = true
		} else {
			d.counts[v]++
		}
	}
	existing := map[string]*domainTerm{}
	for _, other := range s.Pods {
		if other.Spec.NodeName == "" || other == p {
			continue
		}
		if departing(s, other, target) {
			if other.Spec.Affinity != nil && other.Spec.Affinity.PodAntiAffinity != nil && len(other.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) > 0 {
				a.dynamic = true
			}
			continue
		}
		match, u := allMatch(terms, other)
		if match || u {
			for i := range a.affinity {
				add(&a.affinity[i], other, u, true)
			}
		}
		for i := range a.anti {
			match, u := a.anti[i].term.matches(other)
			if match || u {
				add(&a.anti[i], other, u, false)
			}
		}
		if f := other.Spec.Affinity; f != nil && f.PodAntiAffinity != nil {
			for _, raw := range f.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
				t := x.compile(s, raw, other.Namespace)
				match, u := t.matches(p)
				if !match && !u {
					continue
				}
				d := existing[t.raw.TopologyKey]
				if d == nil {
					v := domain(t)
					v.term.unknown = ""
					d = &v
					existing[t.raw.TopologyKey] = d
				}
				if t.unknown != "" {
					a.unknown = append(a.unknown, "existing pod's required anti-affinity: "+t.unknown)
				}
				add(d, other, u, false)
			}
		}
	}
	self, u := allMatch(terms, p)
	a.selfBootstrap = self && !u
	a.bootstrapUnknown = u
	for _, d := range a.affinity {
		if len(d.counts) > 0 || len(d.uncertain) > 0 || d.allUnknown {
			a.selfBootstrap = false
		}
		if len(d.counts) > 0 {
			a.bootstrapUnknown = false
		}
	}
	keys := make([]string, 0, len(existing))
	for k := range existing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a.existing = append(a.existing, *existing[k])
	}
	return a
}

func TestIndexedAffinityMatchesIndependentScan(t *testing.T) {
	for _, base := range []struct {
		name  string
		build func(int, int) model.Snapshot
	}{
		{"PeerDense", testfixture.PeerDense}, {"PathologicalUniqueSelectors", testfixture.PathologicalUniqueSelectors},
	} {
		t.Run(base.name, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				change func(*model.Snapshot)
			}{
				{"peer_dense", func(*model.Snapshot) {}},
				{"missing_peer_node", func(s *model.Snapshot) { s.Pods[1].Spec.NodeName = "absent-node" }},
				{"missing_domain", func(s *model.Snapshot) { delete(s.Nodes[1].Labels, core.LabelTopologyZone) }},
				{"terminal_peer", func(s *model.Snapshot) { s.Pods[1].Status.Phase = core.PodSucceeded }},
				{"negative_key_present", func(s *model.Snapshot) { s.Pods[1].Labels["absent-00000"] = "synthetic-present" }},
				{"negative_key_on_candidate", func(s *model.Snapshot) { s.Pods[0].Labels["absent-00001"] = "synthetic-present" }},
				{"terminating_peer", func(s *model.Snapshot) { s.Pods[1].DeletionTimestamp = ptr(meta.NewTime(s.At)) }},
				{"namespace_unavailable", func(s *model.Snapshot) {
					s.Capabilities[model.NamespacesResource] = model.Capability{State: model.Unavailable}
				}},
				{"namespace_failed", func(s *model.Snapshot) {
					s.Capabilities[model.NamespacesResource] = model.Capability{State: model.Available, Synced: true, Failed: true}
				}},
				{"namespace_missing", func(s *model.Snapshot) { s.Namespaces = s.Namespaces[1:] }},
				{"invalid_existing_term", func(s *model.Snapshot) {
					s.Pods[1].Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].MatchLabelKeys = []string{"unknown-feature"}
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := base.build(8, 160)
					tc.change(&s)
					s = s.Indexed()
					x := newSchedulingEvidence(s)
					for _, p := range s.Pods[:24] {
						for _, target := range s.Nodes[:3] {
							got := prepareAffinity(context.Background(), s, p, target.Name, x)
							want := scanAffinity(s, p, target.Name, x)
							if got.dynamic != want.dynamic {
								t.Fatalf("dynamic evidence differs for %s", p.Name)
							}
							for _, n := range s.Nodes {
								gr, gu, gc := got.check(n)
								wr, wu, wc := want.check(n)
								if !reflect.DeepEqual(unique(gr), unique(wr)) || !reflect.DeepEqual(unique(gu), unique(wu)) || !reflect.DeepEqual(unique(gc), unique(wc)) {
									t.Fatalf("pod=%s target=%s candidate=%s\nindexed=%v %v %v\nscan=%v %v %v", p.Name, target.Name, n.Name, gr, gu, gc, wr, wu, wc)
								}
							}
						}
					}
				})
			}
		})
	}
}

func TestEmptyTopologySelectorCountsAllPeers(t *testing.T) {
	s := affinityFixture()
	s.Pods[0].Labels["app"] = "peer"
	s.Pods[0].Spec.TopologySpreadConstraints = []core.TopologySpreadConstraint{{TopologyKey: core.LabelTopologyZone, MaxSkew: 1, WhenUnsatisfiable: core.DoNotSchedule, LabelSelector: &meta.LabelSelector{}}}
	f := Candidates(s, s.Pods[0], "worker-01")
	if status(f, "worker-02") != "REJECTED" || status(f, "worker-03") != "ALLOWED" {
		t.Fatalf("empty selector produced a false PASS: %+v", f)
	}
	s.Pods[0].Spec.TopologySpreadConstraints[0].LabelSelector = nil
	f = Candidates(s, s.Pods[0], "worker-01")
	if status(f, "worker-02") != "ALLOWED" {
		t.Fatal("nil and empty selectors conflated", f)
	}
}

func assertAffinityParity(t *testing.T, s model.Snapshot, p *core.Pod, target string, x *schedulingEvidence) {
	t.Helper()
	got, want := prepareAffinity(context.Background(), s, p, target, x), scanAffinity(s, p, target, x)
	for _, n := range s.Nodes {
		gr, gu, gc := got.check(n)
		wr, wu, wc := want.check(n)
		if !reflect.DeepEqual(unique(gr), unique(wr)) || !reflect.DeepEqual(unique(gu), unique(wu)) || !reflect.DeepEqual(unique(gc), unique(wc)) {
			t.Fatalf("candidate=%s indexed=%v %v %v scan=%v %v %v", n.Name, gr, gu, gc, wr, wu, wc)
		}
	}
}

func TestExistingAntiAffinitySelectorIndexBoundaries(t *testing.T) {
	label := func(values map[string]string, requirements ...meta.LabelSelectorRequirement) *meta.LabelSelector {
		return &meta.LabelSelector{MatchLabels: values, MatchExpressions: requirements}
	}
	expr := func(key string, op meta.LabelSelectorOperator, values ...string) meta.LabelSelectorRequirement {
		return meta.LabelSelectorRequirement{Key: key, Operator: op, Values: values}
	}
	for _, tc := range []struct {
		name     string
		selector *meta.LabelSelector
		want     string
	}{
		{"single_match_label", label(map[string]string{"app": "incoming"}), "REJECTED"},
		{"multiple_match_labels", label(map[string]string{"app": "incoming", "tier": "web"}), "REJECTED"},
		{"in_single", label(nil, expr("app", meta.LabelSelectorOpIn, "incoming")), "REJECTED"},
		{"in_multiple", label(nil, expr("app", meta.LabelSelectorOpIn, "other", "incoming")), "REJECTED"},
		{"label_and_in", label(map[string]string{"app": "incoming"}, expr("tier", meta.LabelSelectorOpIn, "web", "api")), "REJECTED"},
		{"label_and_notin", label(map[string]string{"app": "incoming"}, expr("tier", meta.LabelSelectorOpNotIn, "database")), "REJECTED"},
		{"label_and_doesnotexist", label(map[string]string{"app": "incoming"}, expr("absent", meta.LabelSelectorOpDoesNotExist)), "REJECTED"},
		{"pure_notin", label(nil, expr("tier", meta.LabelSelectorOpNotIn, "database")), "REJECTED"},
		{"pure_doesnotexist", label(nil, expr("absent", meta.LabelSelectorOpDoesNotExist)), "REJECTED"},
		{"nil_selector", nil, "ALLOWED"},
		{"empty_selector", &meta.LabelSelector{}, "REJECTED"},
		{"invalid_selector", label(map[string]string{"app": "nonmatching"}, expr("tier", "invalid")), "UNKNOWN"},
		{"match_label_keys", label(map[string]string{"app": "nonmatching"}), "UNKNOWN"},
		{"mismatch_label_keys", label(map[string]string{"app": "nonmatching"}), "UNKNOWN"},
		{"namespace_available", label(map[string]string{"app": "incoming"}), "REJECTED"},
		{"namespace_unavailable", label(map[string]string{"app": "incoming"}), "UNKNOWN"},
		{"namespace_failed", label(map[string]string{"app": "incoming"}), "UNKNOWN"},
		{"namespace_missing", label(map[string]string{"app": "incoming"}), "UNKNOWN"},
		{"candidate_missing_label", label(map[string]string{"absent": "value"}), "ALLOWED"},
		{"partial_positive_match", label(map[string]string{"app": "incoming", "tier": "database"}), "ALLOWED"},
		{"multiple_groups", label(map[string]string{"app": "incoming"}), "REJECTED"},
		{"duplicate_group", label(map[string]string{"app": "incoming"}), "REJECTED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := affinityFixture()
			p, peer := s.Pods[0], s.Pods[1]
			p.Labels = map[string]string{"app": "incoming", "tier": "web"}
			p.Spec.Affinity, p.Spec.TopologySpreadConstraints, p.Spec.Volumes = nil, nil, nil
			peer.Namespace = p.Namespace
			s.Namespaces = []*core.Namespace{{ObjectMeta: meta.ObjectMeta{Name: p.Namespace, Labels: map[string]string{"tenant": "shared"}}}}
			s.Capabilities[model.NamespacesResource] = model.Capability{State: model.Available, Synced: true}
			raw := core.PodAffinityTerm{TopologyKey: core.LabelTopologyZone, LabelSelector: tc.selector}
			switch tc.name {
			case "match_label_keys":
				raw.MatchLabelKeys = []string{"feature"}
			case "mismatch_label_keys":
				raw.MismatchLabelKeys = []string{"feature"}
			case "namespace_available", "namespace_unavailable", "namespace_missing", "namespace_failed":
				raw.NamespaceSelector = label(map[string]string{"tenant": "shared"})
				if tc.name == "namespace_unavailable" {
					s.Capabilities[model.NamespacesResource] = model.Capability{State: model.Unavailable}
				}
				if tc.name == "namespace_missing" {
					s.Namespaces = nil
				}
				if tc.name == "namespace_failed" {
					s.Capabilities[model.NamespacesResource] = model.Capability{State: model.Available, Synced: true, Failed: true}
				}
			}
			peer.Spec.Affinity = &core.Affinity{PodAntiAffinity: &core.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{raw}}}
			if tc.name == "multiple_groups" {
				extra := *raw.DeepCopy()
				extra.LabelSelector = label(map[string]string{"app": "nonmatching"})
				peer.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(peer.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution, extra)
			}
			if tc.name == "duplicate_group" {
				other := peer.DeepCopy()
				other.Name = "synthetic-second-peer"
				s.Pods = append(s.Pods, other)
			}
			s = s.Indexed()
			x := newSchedulingEvidence(s)
			indexes := x.existingIndexes(p)
			for i, group := range x.existing {
				match, unknown := group.term.matches(p)
				if (match || unknown) && !slices.Contains(indexes, i) {
					t.Fatalf("prefilter dropped possible/unknown group %d", i)
				}
			}
			assertAffinityParity(t, s, p, p.Spec.NodeName, x)
			if got := status(Candidates(s, p, p.Spec.NodeName), "worker-02"); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestExistingAntiAffinityDuplicateSelfTerms(t *testing.T) {
	s := affinityFixture()
	p := s.Pods[0]
	raw := core.PodAffinityTerm{TopologyKey: core.LabelTopologyZone, LabelSelector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "incoming"}}}
	p.Spec.Affinity = &core.Affinity{PodAntiAffinity: &core.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{raw, raw}}}
	s = s.Indexed()
	// The candidate is already in inventory but is not departing from this target.
	// Both copies of its own existing term must be excluded.
	assertAffinityParity(t, s, p, "worker-03", newSchedulingEvidence(s))
}

func TestPeerAggregateKeyOnlySharesProvenUniversalSelectors(t *testing.T) {
	s := affinityFixture().Indexed()
	x := newSchedulingEvidence(s)
	for _, tc := range []struct {
		name   string
		change func(*core.PodAffinityTerm)
		share  bool
	}{
		{"absent_keys", func(*core.PodAffinityTerm) {}, true},
		{"observed_key", func(t *core.PodAffinityTerm) { t.LabelSelector.MatchExpressions[0].Key = "app" }, false},
		{"positive_requirement", func(t *core.PodAffinityTerm) { t.LabelSelector.MatchLabels = map[string]string{"app": "peer"} }, false},
		{"unknown_feature", func(t *core.PodAffinityTerm) { t.MatchLabelKeys = []string{"feature"} }, false},
		{"invalid_expression", func(t *core.PodAffinityTerm) { t.LabelSelector.MatchExpressions[0].Values = []string{"invalid"} }, false},
		{"nil_selector", func(t *core.PodAffinityTerm) { t.LabelSelector = nil }, false},
		{"other_operator", func(t *core.PodAffinityTerm) {
			t.LabelSelector.MatchExpressions[0].Operator = meta.LabelSelectorOpExists
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := core.PodAffinityTerm{TopologyKey: core.LabelTopologyZone, NamespaceSelector: &meta.LabelSelector{},
				LabelSelector: &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{{Key: "synthetic-absent", Operator: meta.LabelSelectorOpDoesNotExist}}}}
			tc.change(&raw)
			term := x.compile(s, raw, "demo")
			if shared := x.peerAggregateKey(term) != term.key; shared != tc.share {
				t.Fatalf("aggregate sharing=%v want=%v", shared, tc.share)
			}
			if tc.share {
				other := raw.DeepCopy()
				other.TopologyKey = core.LabelHostname
				if x.peerAggregateKey(x.compile(s, *other, "demo")) == x.peerAggregateKey(term) {
					t.Fatal("topology domains collided")
				}
				other = raw.DeepCopy()
				other.NamespaceSelector = nil
				if x.peerAggregateKey(x.compile(s, *other, "synthetic-other")) == x.peerAggregateKey(term) {
					t.Fatal("namespace scopes collided")
				}
			}
		})
	}
}

func FuzzExistingAntiAffinityPrefilter(f *testing.F) {
	for _, op := range []uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9} {
		f.Add("app", "incoming", "incoming", op)
	}
	f.Fuzz(func(t *testing.T, key, value, candidate string, mode uint8) {
		if len(key)+len(value)+len(candidate) > 256 {
			t.Skip()
		}
		ops := []meta.LabelSelectorOperator{meta.LabelSelectorOpIn, meta.LabelSelectorOpNotIn, meta.LabelSelectorOpDoesNotExist, meta.LabelSelectorOpExists, "invalid"}
		raw := core.PodAffinityTerm{TopologyKey: core.LabelTopologyZone, NamespaceSelector: &meta.LabelSelector{},
			LabelSelector: &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{{Key: key, Operator: ops[int(mode)%len(ops)]}}}}
		if mode%5 < 2 {
			raw.LabelSelector.MatchExpressions[0].Values = []string{value, "synthetic-other"}
		}
		if mode&16 != 0 {
			raw.LabelSelector.MatchLabels = map[string]string{"pool": "shared"}
		}
		if mode&32 != 0 {
			raw.MatchLabelKeys = []string{key}
		}
		if mode&64 != 0 {
			raw.MismatchLabelKeys = []string{key}
		}
		if mode == 8 {
			raw.LabelSelector = nil
		}
		if mode == 9 {
			raw.LabelSelector = &meta.LabelSelector{}
		}
		s := affinityFixture()
		p, peer := s.Pods[0], s.Pods[1]
		p.Labels = map[string]string{key: candidate, "pool": "shared"}
		if mode&128 != 0 {
			delete(p.Labels, key)
		}
		p.Spec.Affinity = nil
		peer.Spec.Affinity = &core.Affinity{PodAntiAffinity: &core.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{raw}}}
		s = s.Indexed()
		x := newSchedulingEvidence(s)
		match, unknown := x.existing[0].term.matches(p)
		if (match || unknown) && !slices.Contains(x.existingIndexes(p), 0) {
			t.Fatal("prefilter dropped a matching or UNKNOWN term")
		}
		assertAffinityParity(t, s, p, p.Spec.NodeName, x)
	})
}
