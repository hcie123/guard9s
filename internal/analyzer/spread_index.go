package analyzer

import (
	"context"
	"encoding/json"
	"maps"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
)

type spreadAggregate struct {
	base      []spreadEvidence
	removed   map[string][]map[string]int
	projected map[string][]spreadEvidence
}

func prepareSpread(ctx context.Context, s model.Snapshot, p *core.Pod, target string, index *schedulingEvidence) []spreadEvidence {
	if len(p.Spec.TopologySpreadConstraints) == 0 {
		return nil
	}
	var required *core.NodeSelector
	if p.Spec.Affinity != nil && p.Spec.Affinity.NodeAffinity != nil {
		required = p.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	}
	merged := map[string]string{}
	for _, c := range p.Spec.TopologySpreadConstraints {
		for _, k := range c.MatchLabelKeys {
			if v, ok := p.Labels[k]; ok {
				merged[k] = v
			}
		}
	}
	encoded, _ := json.Marshal(struct {
		Constraints  []core.TopologySpreadConstraint
		NodeSelector map[string]string
		Required     *core.NodeSelector
		Merged       map[string]string
	}{p.Spec.TopologySpreadConstraints, p.Spec.NodeSelector, required, merged})
	key := p.Namespace + "\x00" + string(encoded)
	a := index.spread[key]
	if a == nil {
		a = buildSpread(ctx, s, p)
		if ctx.Err() != nil {
			return []spreadEvidence{{unknown: "analysis cancelled"}}
		}
		index.spread[key] = a
	}
	exclude := index.inventory[p] && p.Spec.NodeName != "" && !departing(s, p, target)
	if result, ok := a.projected[target]; ok && !exclude {
		return result
	}
	result := make([]spreadEvidence, len(a.base))
	for i, base := range a.base {
		x := base
		x.counts = maps.Clone(base.counts)
		x.proofs = map[string]spreadProof{}
		if r := a.removed[target]; r != nil {
			for v, n := range r[i] {
				x.counts[v] -= n
			}
		}
		if exclude && base.countedNodes[p.Spec.NodeName] && p.DeletionTimestamp == nil && model.Active(p) && base.selector.Matches(labels.Set(p.Labels)) {
			x.counts[s.Node(p.Spec.NodeName).Labels[base.raw.TopologyKey]]--
		}
		domains := int32(1)
		if x.raw.MinDomains != nil {
			domains = *x.raw.MinDomains
		}
		if len(x.counts) >= int(domains) {
			first := true
			for _, count := range x.counts {
				if first || count < x.minimum {
					x.minimum = count
					first = false
				}
			}
		}
		result[i] = x
	}
	if !exclude {
		a.projected[target] = result
	}
	return result
}

func buildSpread(ctx context.Context, s model.Snapshot, p *core.Pod) *spreadAggregate {
	a := &spreadAggregate{removed: map[string][]map[string]int{}, projected: map[string][]spreadEvidence{}}
	for _, c := range p.Spec.TopologySpreadConstraints {
		if c.WhenUnsatisfiable != core.DoNotSchedule {
			continue
		}
		x := spreadEvidence{raw: c, counts: map[string]int{}, countedNodes: map[string]bool{}}
		selector, err := meta.LabelSelectorAsSelector(c.LabelSelector)
		x.selector = selector
		if err != nil || c.MaxSkew < 1 || c.TopologyKey == "" || (c.MinDomains != nil && *c.MinDomains < 1) {
			x.unknown = "invalid hard topology spread constraint"
			a.base = append(a.base, x)
			continue
		}
		if c.NodeAffinityPolicy != nil && *c.NodeAffinityPolicy != core.NodeInclusionPolicyHonor {
			x.unknown = "topology nodeAffinityPolicy feature gate is unavailable"
		}
		if c.NodeTaintsPolicy != nil && *c.NodeTaintsPolicy != core.NodeInclusionPolicyIgnore {
			x.unknown = "topology nodeTaintsPolicy feature gate is unavailable"
		}
		for _, k := range c.MatchLabelKeys {
			if v, ok := p.Labels[k]; ok && !mergedLabel(c.LabelSelector, k, v) {
				x.unknown = "topology matchLabelKeys is not merged; feature gate is unavailable"
			}
		}
		required := nodeaffinity.GetRequiredNodeAffinity(p)
		for _, n := range s.Nodes {
			if ctx.Err() != nil {
				x.unknown = "analysis cancelled"
				break
			}
			allKeys := true
			for _, other := range p.Spec.TopologySpreadConstraints {
				if other.WhenUnsatisfiable == core.DoNotSchedule {
					if _, ok := n.Labels[other.TopologyKey]; !ok {
						allKeys = false
					}
				}
			}
			if !allKeys {
				continue
			}
			match, err := required.Match(n)
			if err != nil {
				x.unknown = "invalid node affinity in topology domains"
				continue
			}
			if !match {
				continue
			}
			x.countedNodes[n.Name] = true
			value := n.Labels[c.TopologyKey]
			if _, ok := x.counts[value]; !ok {
				x.counts[value] = 0
			}
			for _, other := range s.NodePods(n.Name) {
				if other.Namespace != p.Namespace || other.DeletionTimestamp != nil || !selector.Matches(labels.Set(other.Labels)) {
					continue
				}
				x.counts[value]++
				if model.Movable(s, other) {
					r := a.removed[n.Name]
					for len(r) <= len(a.base) {
						r = append(r, map[string]int{})
					}
					r[len(a.base)][value]++
					a.removed[n.Name] = r
				}
			}
		}
		for _, other := range s.NamespacePods(p.Namespace) {
			if other.Spec.NodeName != "" && s.Node(other.Spec.NodeName) == nil && selector.Matches(labels.Set(other.Labels)) {
				x.unknown = "topology peer node is absent from snapshot"
			}
		}
		a.base = append(a.base, x)
	}
	for n, r := range a.removed {
		for len(r) < len(a.base) {
			r = append(r, map[string]int{})
		}
		a.removed[n] = r
	}
	return a
}
