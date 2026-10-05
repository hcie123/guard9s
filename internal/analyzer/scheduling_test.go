package analyzer

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func affinityFixture() model.Snapshot {
	s := fixture()
	for i, n := range s.Nodes {
		n.Labels[core.LabelTopologyZone] = []string{"zone-a", "zone-b", "zone-c"}[i]
	}
	s.Pods[0].Labels = map[string]string{"app": "incoming"}
	s.Pods[1].Labels = map[string]string{"app": "peer"}
	return s
}
func affinityRequirement() core.PodAffinityTerm {
	return core.PodAffinityTerm{TopologyKey: core.LabelTopologyZone, LabelSelector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "peer"}}}
}
func status(f Feasibility, node string) string {
	for _, e := range f.Evaluations {
		if e.Node == node {
			return e.Status
		}
	}
	return "missing"
}
func TestRequiredPodAffinity(t *testing.T) {
	for _, tc := range []struct {
		name       string
		change     func(*model.Snapshot, *core.PodAffinityTerm)
		two, three string
	}{
		{"peer_same_namespace", func(*model.Snapshot, *core.PodAffinityTerm) {}, "ALLOWED", "REJECTED"},
		{"explicit_other_namespace", func(s *model.Snapshot, t *core.PodAffinityTerm) {
			s.Pods[1].Namespace = "peer-ns"
			t.Namespaces = []string{"peer-ns"}
		}, "ALLOWED", "REJECTED"},
		{"other_namespace_default_excluded", func(s *model.Snapshot, t *core.PodAffinityTerm) { s.Pods[1].Namespace = "peer-ns" }, "REJECTED", "REJECTED"},
		{"empty_namespace_selector_all", func(s *model.Snapshot, t *core.PodAffinityTerm) {
			s.Pods[1].Namespace = "peer-ns"
			t.NamespaceSelector = &meta.LabelSelector{}
		}, "ALLOWED", "REJECTED"},
		{"namespace_labels_unknown", func(s *model.Snapshot, t *core.PodAffinityTerm) {
			s.Capabilities = nil
			t.NamespaceSelector = &meta.LabelSelector{MatchLabels: map[string]string{"team": "synthetic"}}
		}, "UNKNOWN", "REJECTED"},
		{"namespace_union_proves_peer", func(s *model.Snapshot, t *core.PodAffinityTerm) {
			t.Namespaces = []string{"demo"}
			t.NamespaceSelector = &meta.LabelSelector{MatchLabels: map[string]string{"team": "synthetic"}}
		}, "ALLOWED", "REJECTED"},
		{"namespace_invalid", func(s *model.Snapshot, t *core.PodAffinityTerm) {
			t.NamespaceSelector = &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{{Key: "team", Operator: "invalid"}}}
		}, "UNKNOWN", "UNKNOWN"},
		{"nil_selector_none", func(s *model.Snapshot, t *core.PodAffinityTerm) { t.LabelSelector = nil }, "REJECTED", "REJECTED"},
		{"empty_selector_all", func(s *model.Snapshot, t *core.PodAffinityTerm) { t.LabelSelector = &meta.LabelSelector{} }, "ALLOWED", "REJECTED"},
		{"selector_expression", func(s *model.Snapshot, t *core.PodAffinityTerm) {
			t.LabelSelector = &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{{Key: "app", Operator: meta.LabelSelectorOpIn, Values: []string{"peer"}}}}
		}, "ALLOWED", "REJECTED"},
		{"invalid_selector", func(s *model.Snapshot, t *core.PodAffinityTerm) {
			t.LabelSelector = &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{{Key: "app", Operator: "invalid"}}}
		}, "UNKNOWN", "UNKNOWN"},
		{"missing_topology", func(s *model.Snapshot, t *core.PodAffinityTerm) { delete(s.Nodes[1].Labels, core.LabelTopologyZone) }, "REJECTED", "UNKNOWN"},
		{"terminating_anchor_unknown", func(s *model.Snapshot, t *core.PodAffinityTerm) {
			s.Pods[1].DeletionTimestamp = ptr(meta.NewTime(s.At))
		}, "UNKNOWN", "REJECTED"},
		{"terminal_anchor_unknown", func(s *model.Snapshot, t *core.PodAffinityTerm) { s.Pods[1].Status.Phase = core.PodSucceeded }, "UNKNOWN", "REJECTED"},
		{"departing_anchor_excluded", func(s *model.Snapshot, t *core.PodAffinityTerm) { s.Pods[1].Spec.NodeName = "worker-01" }, "REJECTED", "REJECTED"},
		{"unassigned_anchor_excluded", func(s *model.Snapshot, t *core.PodAffinityTerm) { s.Pods[1].Spec.NodeName = "" }, "REJECTED", "REJECTED"},
		{"unknown_peer_node", func(s *model.Snapshot, t *core.PodAffinityTerm) { s.Pods[1].Spec.NodeName = "missing-node" }, "UNKNOWN", "UNKNOWN"},
		{"self_bootstrap", func(s *model.Snapshot, t *core.PodAffinityTerm) {
			s.Pods = s.Pods[:1]
			s.Pods[0].Labels["app"] = "peer"
		}, "ALLOWED", "ALLOWED"},
		{"self_bootstrap_namespace_unknown", func(s *model.Snapshot, t *core.PodAffinityTerm) {
			s.Capabilities = nil
			s.Pods = s.Pods[:1]
			s.Pods[0].Labels["app"] = "peer"
			t.NamespaceSelector = &meta.LabelSelector{MatchLabels: map[string]string{"team": "synthetic"}}
		}, "UNKNOWN", "UNKNOWN"},
		{"match_label_keys_unknown", func(s *model.Snapshot, t *core.PodAffinityTerm) { t.MatchLabelKeys = []string{"app"} }, "UNKNOWN", "UNKNOWN"},
		{"mismatch_label_keys_unknown", func(s *model.Snapshot, t *core.PodAffinityTerm) { t.MismatchLabelKeys = []string{"app"} }, "UNKNOWN", "UNKNOWN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := affinityFixture()
			term := affinityRequirement()
			tc.change(&s, &term)
			s.Pods[0].Spec.Affinity = &core.Affinity{PodAffinity: &core.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{term}}}
			f := Candidates(s, s.Pods[0], "worker-01")
			if status(f, "worker-02") != tc.two || status(f, "worker-03") != tc.three {
				t.Fatalf("%+v", f)
			}
			if len(f.Evaluations) != 2 || strings.Contains(CandidateText(f), "worker-01 ALLOWED") {
				t.Fatal("target candidate leaked")
			}
		})
	}
}
func TestAffinityRequiresPeersMatchingAllTerms(t *testing.T) {
	s := affinityFixture()
	a, b := affinityRequirement(), affinityRequirement()
	b.LabelSelector = &meta.LabelSelector{MatchLabels: map[string]string{"role": "anchor"}}
	s.Pods[0].Spec.Affinity = &core.Affinity{PodAffinity: &core.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{a, b}}}
	other := s.Pods[1].DeepCopy()
	other.Name = "other-peer"
	other.Labels = map[string]string{"role": "anchor"}
	s.Pods = append(s.Pods, other)
	if f := Candidates(s, s.Pods[0], "worker-01"); status(f, "worker-02") != "REJECTED" {
		t.Fatalf("separate peers incorrectly satisfy AND: %+v", f)
	}
	s.Pods[1].Labels["role"] = "anchor"
	if f := Candidates(s, s.Pods[0], "worker-01"); status(f, "worker-02") != "ALLOWED" {
		t.Fatal(f)
	}
}
func TestRequiredAndExistingAntiAffinity(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			change func(*model.Snapshot, *core.PodAffinityTerm)
			two    string
		}{
			{"conflict", func(*model.Snapshot, *core.PodAffinityTerm) {}, "REJECTED"},
			{"namespace_no_conflict", func(s *model.Snapshot, t *core.PodAffinityTerm) { s.Pods[1].Namespace = "other" }, "ALLOWED"},
			{"namespace_all_conflict", func(s *model.Snapshot, t *core.PodAffinityTerm) {
				s.Pods[1].Namespace = "other"
				t.NamespaceSelector = &meta.LabelSelector{}
			}, "REJECTED"},
			{"namespace_labels_unknown", func(s *model.Snapshot, t *core.PodAffinityTerm) {
				s.Capabilities = nil
				t.NamespaceSelector = &meta.LabelSelector{MatchLabels: map[string]string{"team": "synthetic"}}
			}, "UNKNOWN"},
			{"terminating_still_conflicts", func(s *model.Snapshot, t *core.PodAffinityTerm) {
				s.Pods[1].DeletionTimestamp = ptr(meta.NewTime(s.At))
			}, "REJECTED"},
			{"terminal_unknown", func(s *model.Snapshot, t *core.PodAffinityTerm) { s.Pods[1].Status.Phase = core.PodFailed }, "UNKNOWN"},
			{"no_match", func(s *model.Snapshot, t *core.PodAffinityTerm) {
				t.LabelSelector = &meta.LabelSelector{MatchLabels: map[string]string{"app": "absent"}}
			}, "ALLOWED"},
			{"missing_topology_unknown", func(s *model.Snapshot, t *core.PodAffinityTerm) { delete(s.Nodes[1].Labels, core.LabelTopologyZone) }, "UNKNOWN"},
		} {
			name := tc.name
			if existing {
				name = "existing_" + name
			}
			t.Run(name, func(t *testing.T) {
				s := affinityFixture()
				term := affinityRequirement()
				if existing {
					term.LabelSelector = &meta.LabelSelector{MatchLabels: map[string]string{"app": "incoming"}}
				}
				tc.change(&s, &term)
				idx := 0
				if existing {
					idx = 1
				}
				s.Pods[idx].Spec.Affinity = &core.Affinity{PodAntiAffinity: &core.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{term}}}
				f := Candidates(s, s.Pods[0], "worker-01")
				if status(f, "worker-02") != tc.two {
					t.Fatalf("%+v", f)
				}
				if tc.name == "missing_topology_unknown" && status(f, "worker-03") != "UNKNOWN" {
					t.Fatal("missing peer topology incorrectly allowed an unrelated domain")
				}
				if tc.name != "missing_topology_unknown" && status(f, "worker-03") == "REJECTED" {
					t.Fatal("unrelated domain rejected")
				}
			})
		}
	}
}

func TestSharedSchedulingEvidenceMatchesIndependentDiagnoses(t *testing.T) {
	s := demo.Snapshot("scheduling")
	shared := AnalyzeAll(context.Background(), s)
	for _, n := range s.Nodes {
		independent := Diagnose(context.Background(), Input{Snapshot: s, Node: n.Name})
		if !reflect.DeepEqual(shared[n.Name], independent) {
			t.Fatalf("target or namespace evidence leaked through shared cache for %s", n.Name)
		}
	}
}

func TestHardTopologySpread(t *testing.T) {
	for _, tc := range []struct {
		name       string
		change     func(*model.Snapshot, *core.TopologySpreadConstraint)
		two, three string
	}{
		{"skew", func(*model.Snapshot, *core.TopologySpreadConstraint) {}, "REJECTED", "ALLOWED"},
		{"max_skew_two", func(s *model.Snapshot, c *core.TopologySpreadConstraint) { c.MaxSkew = 2 }, "ALLOWED", "ALLOWED"},
		{"min_domains_zero_minimum", func(s *model.Snapshot, c *core.TopologySpreadConstraint) { c.MinDomains = ptr(int32(4)) }, "REJECTED", "ALLOWED"},
		{"default_affinity_policy", func(s *model.Snapshot, c *core.TopologySpreadConstraint) {
			c.NodeAffinityPolicy = ptr(core.NodeInclusionPolicyHonor)
		}, "REJECTED", "ALLOWED"},
		{"default_taints_policy", func(s *model.Snapshot, c *core.TopologySpreadConstraint) {
			c.NodeTaintsPolicy = ptr(core.NodeInclusionPolicyIgnore)
		}, "REJECTED", "ALLOWED"},
		{"nondefault_affinity_unknown", func(s *model.Snapshot, c *core.TopologySpreadConstraint) {
			c.NodeAffinityPolicy = ptr(core.NodeInclusionPolicyIgnore)
		}, "UNKNOWN", "UNKNOWN"},
		{"nondefault_taints_unknown", func(s *model.Snapshot, c *core.TopologySpreadConstraint) {
			c.NodeTaintsPolicy = ptr(core.NodeInclusionPolicyHonor)
		}, "UNKNOWN", "UNKNOWN"},
		{"match_label_keys_unmerged", func(s *model.Snapshot, c *core.TopologySpreadConstraint) {
			s.Pods[0].Labels["revision"] = "synthetic"
			c.MatchLabelKeys = []string{"revision"}
		}, "UNKNOWN", "UNKNOWN"},
		{"match_label_keys_merged", func(s *model.Snapshot, c *core.TopologySpreadConstraint) {
			s.Pods[0].Labels["revision"] = "synthetic"
			s.Pods[1].Labels["revision"] = "synthetic"
			c.MatchLabelKeys = []string{"revision"}
			c.LabelSelector.MatchLabels["revision"] = "synthetic"
		}, "REJECTED", "ALLOWED"},
		{"match_key_absent_no_effect", func(s *model.Snapshot, c *core.TopologySpreadConstraint) { c.MatchLabelKeys = []string{"revision"} }, "REJECTED", "ALLOWED"},
		{"terminating_not_counted", func(s *model.Snapshot, c *core.TopologySpreadConstraint) {
			s.Pods[1].DeletionTimestamp = ptr(meta.NewTime(s.At))
		}, "ALLOWED", "ALLOWED"},
		{"different_namespace_not_counted", func(s *model.Snapshot, c *core.TopologySpreadConstraint) { s.Pods[1].Namespace = "other" }, "ALLOWED", "ALLOWED"},
		{"missing_label", func(s *model.Snapshot, c *core.TopologySpreadConstraint) {
			delete(s.Nodes[2].Labels, core.LabelTopologyZone)
		}, "REJECTED", "REJECTED"},
		{"invalid_skew", func(s *model.Snapshot, c *core.TopologySpreadConstraint) { c.MaxSkew = 0 }, "UNKNOWN", "UNKNOWN"},
		{"invalid_domains", func(s *model.Snapshot, c *core.TopologySpreadConstraint) { c.MinDomains = ptr(int32(0)) }, "UNKNOWN", "UNKNOWN"},
		{"invalid_selector", func(s *model.Snapshot, c *core.TopologySpreadConstraint) {
			c.LabelSelector = &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{{Key: "app", Operator: "invalid"}}}
		}, "UNKNOWN", "UNKNOWN"},
		{"soft_spread", func(s *model.Snapshot, c *core.TopologySpreadConstraint) { c.WhenUnsatisfiable = core.ScheduleAnyway }, "ALLOWED", "ALLOWED"},
		{"departing_peer_excluded", func(s *model.Snapshot, c *core.TopologySpreadConstraint) { s.Pods[1].Spec.NodeName = "worker-01" }, "ALLOWED", "ALLOWED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := affinityFixture()
			s.Pods[0].Labels["app"] = "peer"
			c := core.TopologySpreadConstraint{TopologyKey: core.LabelTopologyZone, MaxSkew: 1, WhenUnsatisfiable: core.DoNotSchedule, LabelSelector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "peer"}}}
			tc.change(&s, &c)
			s.Pods[0].Spec.TopologySpreadConstraints = []core.TopologySpreadConstraint{c}
			f := Candidates(s, s.Pods[0], "worker-01")
			if status(f, "worker-02") != tc.two || status(f, "worker-03") != tc.three {
				t.Fatalf("%+v", f)
			}
		})
	}
}
func TestInteractingRelocationsAndCancellation(t *testing.T) {
	s := affinityFixture()
	s.Pods[1].Spec.NodeName = "worker-01"
	s.Pods[0].Spec.Affinity = &core.Affinity{PodAffinity: &core.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{{TopologyKey: core.LabelTopologyZone, LabelSelector: &meta.LabelSelector{}}}}}
	d := Diagnose(context.Background(), Input{Snapshot: s, Node: "worker-01"})
	if d.Capacity.Assessment != "UNKNOWN" || !strings.Contains(d.Capacity.Detail, "Interacting") {
		t.Fatal(d.Capacity)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := AnalyzeAll(ctx, s); len(got) != 0 {
		t.Fatal(got)
	}
	f := candidates(ctx, s.Indexed(), s.Pods[0], "worker-01", newSchedulingEvidence(s))
	if len(f.Unknown) == 0 || status(f, "worker-02") != "UNKNOWN" {
		t.Fatal(f)
	}
}

func FuzzSelectorsAndTolerations(f *testing.F) {
	f.Add("peer", "In", "Equal", "NoSchedule")
	f.Add("", "invalid", "Exists", "NoExecute")
	f.Fuzz(func(t *testing.T, value, operator, toleration, effect string) {
		if len(value)+len(operator)+len(toleration)+len(effect) > 256 {
			return
		}
		term := core.PodAffinityTerm{TopologyKey: core.LabelTopologyZone, LabelSelector: &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{{Key: "app", Operator: meta.LabelSelectorOperator(operator), Values: []string{value}}}}}
		a := compileTerm(term, "synthetic")
		a.matches(&core.Pod{ObjectMeta: meta.ObjectMeta{Namespace: "synthetic", Labels: map[string]string{"app": "peer"}}})
		tolerates(core.Toleration{Key: "synthetic", Operator: core.TolerationOperator(toleration), Value: value}, core.Taint{Key: "synthetic", Value: "peer", Effect: core.TaintEffect(effect)})
	})
}

func TestPinnedControllerTemplatesRemainUnknown(t *testing.T) {
	find := func(s model.Snapshot, kind string) *core.Pod {
		for _, p := range s.Pods {
			if s.Owner(p).Kind == kind {
				return p
			}
		}
		return nil
	}
	for _, kind := range []string{"Deployment", "StatefulSet", "CronJob"} {
		t.Run(kind, func(t *testing.T) {
			s := demo.Snapshot("mixed")
			p := find(s, kind)
			if p == nil || pinnedTemplate(s, p) {
				t.Fatalf("unexpected initial %s template state", kind)
			}
			o := s.Owner(p)
			switch kind {
			case "Deployment":
				for _, x := range s.Deployments {
					if x.Namespace == p.Namespace && x.Name == o.Name {
						x.Spec.Template.Spec.NodeName = "synthetic-pinned-node"
					}
				}
			case "StatefulSet":
				for _, x := range s.StatefulSets {
					if x.Namespace == p.Namespace && x.Name == o.Name {
						x.Spec.Template.Spec.NodeName = "synthetic-pinned-node"
					}
				}
			case "CronJob":
				for _, x := range s.CronJobs {
					if x.Namespace == p.Namespace && x.Name == o.Name {
						x.Spec.JobTemplate.Spec.Template.Spec.NodeName = "synthetic-pinned-node"
					}
				}
			}
			if !pinnedTemplate(s, p) {
				t.Fatalf("%s pinned spec.nodeName was not detected", kind)
			}
			if got := Candidates(s, p, p.Spec.NodeName); !strings.Contains(strings.Join(got.Unknown, " "), "controller template pins spec.nodeName") {
				t.Fatalf("%s pin did not propagate to scheduling UNKNOWN evidence: %+v", kind, got)
			}
		})
	}

	t.Run("ReplicaSet", func(t *testing.T) {
		s := demo.Snapshot("mixed")
		s.ReplicaSets[0].OwnerReferences = nil
		p := find(s, "ReplicaSet")
		if p == nil || pinnedTemplate(s, p) {
			t.Fatal("unexpected initial ReplicaSet template state")
		}
		o := s.Owner(p)
		for _, x := range s.ReplicaSets {
			if x.Namespace == p.Namespace && x.Name == o.Name {
				x.Spec.Template.Spec.NodeName = "synthetic-pinned-node"
			}
		}
		if !pinnedTemplate(s, p) {
			t.Fatal("ReplicaSet pinned spec.nodeName was not detected")
		}
		if got := Candidates(s, p, p.Spec.NodeName); !strings.Contains(strings.Join(got.Unknown, " "), "controller template pins spec.nodeName") {
			t.Fatalf("ReplicaSet pin did not propagate to scheduling UNKNOWN evidence: %+v", got)
		}
	})

	t.Run("Job", func(t *testing.T) {
		s := demo.Snapshot("mixed")
		s.Jobs[0].OwnerReferences = nil
		p := find(s, "Job")
		if p == nil || pinnedTemplate(s, p) {
			t.Fatal("unexpected initial Job template state")
		}
		o := s.Owner(p)
		for _, x := range s.Jobs {
			if x.Namespace == p.Namespace && x.Name == o.Name {
				x.Spec.Template.Spec.NodeName = "synthetic-pinned-node"
			}
		}
		if !pinnedTemplate(s, p) {
			t.Fatal("Job pinned spec.nodeName was not detected")
		}
		if got := Candidates(s, p, p.Spec.NodeName); !strings.Contains(strings.Join(got.Unknown, " "), "controller template pins spec.nodeName") {
			t.Fatalf("Job pin did not propagate to scheduling UNKNOWN evidence: %+v", got)
		}
	})

	s := demo.Snapshot("mixed")
	if p := find(s, "Standalone"); p == nil || pinnedTemplate(s, p) {
		t.Fatal("standalone pod should not report a pinned controller template")
	}
}
