package analyzer

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Required affinity peers match ALL incoming terms; anti-affinity matches ANY.
type affinityTerm struct {
	raw                         core.PodAffinityTerm
	ns, key                     string
	selector, namespaceSelector labels.Selector
	namespaceAvailable          bool
	namespaceMatches            map[string]bool
	unknown                     string
}

func compileTerm(t core.PodAffinityTerm, ns string) affinityTerm {
	a := affinityTerm{raw: t, ns: ns}
	var err error
	a.selector, err = meta.LabelSelectorAsSelector(t.LabelSelector)
	if err != nil || a.selector == nil || t.TopologyKey == "" {
		a.unknown = "invalid affinity term"
	}
	if len(t.MatchLabelKeys) > 0 || len(t.MismatchLabelKeys) > 0 {
		a.unknown = "affinity matchLabelKeys/mismatchLabelKeys feature gate is unavailable"
	}
	if t.NamespaceSelector != nil {
		a.namespaceSelector, err = meta.LabelSelectorAsSelector(t.NamespaceSelector)
		if err != nil {
			a.unknown = "invalid namespace selector"
		}
	}
	return a
}
func (a affinityTerm) matches(p *core.Pod) (bool, bool) {
	if a.unknown != "" {
		return false, true
	}
	if !a.selector.Matches(labels.Set(p.Labels)) {
		return false, false
	}
	if slices.Contains(a.raw.Namespaces, p.Namespace) {
		return true, false
	}
	if a.namespaceSelector != nil {
		if a.namespaceSelector.Empty() {
			return true, false
		}
		if !a.namespaceAvailable {
			return false, true
		}
		match, observed := a.namespaceMatches[p.Namespace]
		if !observed {
			return false, true
		}
		return match, false
	}
	if len(a.raw.Namespaces) == 0 {
		return p.Namespace == a.ns, false
	}
	return false, false
}
func allMatch(terms []affinityTerm, p *core.Pod) (bool, bool) {
	uncertain := false
	for _, t := range terms {
		match, u := t.matches(p)
		if !match && !u {
			return false, false
		}
		uncertain = uncertain || u
	}
	return len(terms) > 0 && !uncertain, uncertain
}

type domainTerm struct {
	term       affinityTerm
	counts     map[string]int
	uncertain  map[string]bool
	allUnknown bool
}
type affinityEvidence struct {
	affinity, anti, existing []domainTerm
	selfBootstrap, dynamic   bool
	bootstrapUnknown         bool
	unknown                  []string
}

func domain(t affinityTerm) domainTerm {
	return domainTerm{term: t, counts: map[string]int{}, uncertain: map[string]bool{}}
}
func departing(s model.Snapshot, p *core.Pod, target string) bool {
	return p.Spec.NodeName == target && model.Movable(s, p)
}
func prepareAffinity(ctx context.Context, s model.Snapshot, p *core.Pod, target string, x *schedulingEvidence) affinityEvidence {
	a := affinityEvidence{}
	var terms, anti []affinityTerm
	if f := p.Spec.Affinity; f != nil {
		if f.PodAffinity != nil {
			for _, t := range f.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
				terms = append(terms, x.compile(s, t, p.Namespace))
			}
		}
		if f.PodAntiAffinity != nil {
			for _, t := range f.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
				anti = append(anti, x.compile(s, t, p.Namespace))
			}
		}
	}
	a.affinity = x.domains(ctx, s, terms, p, target, true)
	for _, t := range anti {
		a.anti = append(a.anti, x.domains(ctx, s, []affinityTerm{t}, p, target, false)...)
	}
	a.dynamic = len(terms)+len(anti) > 0 || x.existingDepartures[target]
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
	existing := map[string]*domainTerm{}
	for _, i := range x.existingIndexes(p) {
		if ctx.Err() != nil {
			break
		}
		e := x.existing[i]
		match, u := e.term.matches(p)
		if !match && !u {
			continue
		}
		key := e.term.raw.TopologyKey
		projected := e.counts.project(s, p, target, true)[0]
		if len(projected.counts) == 0 && len(projected.uncertain) == 0 && !projected.allUnknown {
			continue
		}
		d := existing[key]
		if d == nil {
			v := domain(e.term)
			v.term.unknown = ""
			d = &v
			existing[key] = d
		}
		if e.term.unknown != "" {
			a.unknown = append(a.unknown, "existing pod's required anti-affinity: "+e.term.unknown)
		}
		for v, n := range projected.counts {
			if u {
				d.uncertain[v] = true
			} else {
				d.counts[v] += n
			}
		}
		for v := range projected.uncertain {
			d.uncertain[v] = true
		}
		d.allUnknown = d.allUnknown || projected.allUnknown
	}
	keys := make([]string, 0, len(existing))
	for key := range existing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		a.existing = append(a.existing, *existing[key])
	}
	if ctx.Err() != nil {
		a.unknown = append(a.unknown, "analysis cancelled")
	}
	return a
}
func (a affinityEvidence) check(n *core.Node) (rejected, unknown, checked []string) {
	unknown = append(unknown, a.unknown...)
	for _, d := range a.affinity {
		v, exists := n.Labels[d.term.raw.TopologyKey]
		if d.term.unknown != "" {
			unknown = append(unknown, d.term.unknown)
			continue
		}
		if !exists {
			rejected = append(rejected, "required podAffinity topology label is missing: "+d.term.raw.TopologyKey)
			continue
		}
		if d.counts[v] > 0 {
			checked = append(checked, fmt.Sprintf("required podAffinity %s=%s peers=%d", d.term.raw.TopologyKey, v, d.counts[v]))
		} else if d.uncertain[v] || d.allUnknown || a.bootstrapUnknown {
			unknown = append(unknown, "required podAffinity peers or namespace labels are unavailable")
		} else if a.selfBootstrap {
			checked = append(checked, "required podAffinity first self-matching pod bootstrap")
		} else {
			rejected = append(rejected, "no remaining peer satisfies all required podAffinity terms in "+d.term.raw.TopologyKey+"="+v)
		}
	}
	for group, terms := range [][]domainTerm{a.anti, a.existing} {
		kind := "required podAntiAffinity"
		if group == 1 {
			kind = "existing pod's required anti-affinity"
		}
		for _, d := range terms {
			if d.term.unknown != "" {
				unknown = append(unknown, kind+": "+d.term.unknown)
				continue
			}
			v, exists := n.Labels[d.term.raw.TopologyKey]
			if !exists {
				unknown = append(unknown, kind+" topology label is missing: "+d.term.raw.TopologyKey)
				continue
			}
			if d.counts[v] > 0 {
				rejected = append(rejected, fmt.Sprintf("%s conflict %s=%s matching peers=%d", kind, d.term.raw.TopologyKey, v, d.counts[v]))
			} else if d.uncertain[v] || d.allUnknown {
				unknown = append(unknown, kind+" peers or namespace labels are unavailable")
			} else {
				checked = append(checked, kind+" has no matching peer in "+d.term.raw.TopologyKey+"="+v)
			}
		}
	}
	return
}
