package analyzer

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type labelPair struct{ key, value string }
type existingGroup struct {
	term   affinityTerm
	counts *domainAggregate
}

// SchedulingIndex v2 is private to one immutable snapshot and one sequential
// analysis worker. Only recurring selectors/domains are cached; no refresh can
// reuse this evidence. Candidate matrices still scale with pods * nodes.
type schedulingEvidence struct {
	existing           []existingGroup
	existingByLabel    map[labelPair][]int
	existingBroad      []int
	terms              map[string]affinityTerm
	peers              map[string]*domainAggregate
	spread             map[string]*spreadAggregate
	byLabel            map[labelPair][]*core.Pod
	labelKeys          map[string]bool
	assigned           []*core.Pod
	inventory          map[*core.Pod]bool
	existingDepartures map[string]bool
	nodes              []*core.Node
	csiUsage           map[string]volumeUsage
}

func termKey(t core.PodAffinityTerm, ns string) string {
	if t.NamespaceSelector != nil || len(t.Namespaces) != 0 {
		ns = ""
	}
	b, _ := json.Marshal(t)
	return ns + "\x00" + string(b)
}

func (x *schedulingEvidence) compile(s model.Snapshot, t core.PodAffinityTerm, ns string) affinityTerm {
	key := termKey(t, ns)
	if a, ok := x.terms[key]; ok {
		return a
	}
	a := compileTerm(t, ns)
	a.key = key
	if t.NamespaceSelector != nil && a.namespaceSelector != nil && !a.namespaceSelector.Empty() {
		a.namespaceAvailable = s.Capability(model.NamespacesResource).Usable()
		a.namespaceMatches = map[string]bool{}
		if a.namespaceAvailable {
			for _, n := range s.Namespaces {
				a.namespaceMatches[n.Name] = a.namespaceSelector.Matches(labels.Set(n.Labels))
			}
		}
	}
	x.terms[key] = a
	return a
}

func newSchedulingEvidence(s model.Snapshot) *schedulingEvidence {
	s = s.WithLookups()
	x := &schedulingEvidence{
		terms: map[string]affinityTerm{}, peers: map[string]*domainAggregate{},
		spread: map[string]*spreadAggregate{}, byLabel: map[labelPair][]*core.Pod{},
		existingByLabel: map[labelPair][]int{},
		labelKeys:       map[string]bool{},
		inventory:       map[*core.Pod]bool{}, existingDepartures: map[string]bool{},
		csiUsage: map[string]volumeUsage{}, nodes: slices.Clone(s.Nodes),
	}
	sort.Slice(x.nodes, func(i, j int) bool { return x.nodes[i].Name < x.nodes[j].Name })
	groups := map[string]int{}
	for _, p := range s.Pods {
		x.inventory[p] = true
		if p.Spec.NodeName == "" {
			continue
		}
		x.assigned = append(x.assigned, p)
		for k, v := range p.Labels {
			x.labelKeys[k] = true
			pair := labelPair{k, v}
			x.byLabel[pair] = append(x.byLabel[pair], p)
		}
		if p.Spec.Affinity == nil || p.Spec.Affinity.PodAntiAffinity == nil {
			continue
		}
		for _, raw := range p.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
			t := x.compile(s, raw, p.Namespace)
			i, ok := groups[t.key]
			if !ok {
				i = len(x.existing)
				groups[t.key] = i
				x.existing = append(x.existing, existingGroup{t, newDomainAggregate([]affinityTerm{t}, false)})
				x.existing[i].counts.members = map[*core.Pod]int{}
				pairs := selectorIndexPairs(t)
				if len(pairs) == 0 {
					x.existingBroad = append(x.existingBroad, i)
				} else {
					for _, pair := range pairs {
						x.existingByLabel[pair] = append(x.existingByLabel[pair], i)
					}
				}
			}
			x.existing[i].counts.add(s, p, false)
			x.existing[i].counts.members[p]++
			if model.Movable(s, p) {
				x.existingDepartures[p.Spec.NodeName] = true
			}
		}
	}
	return x
}

func selectorIndexPairs(t affinityTerm) []labelPair {
	// Invalid/feature-gated terms must remain broad so they can still contribute
	// conservative UNKNOWN evidence. For valid selectors, any exact positive
	// requirement is a safe prefilter: a Pod that matches the whole selector
	// must match at least one of these indexed pairs.
	if t.unknown != "" || t.raw.LabelSelector == nil {
		return nil
	}
	seen := map[labelPair]bool{}
	var out []labelPair
	add := func(pair labelPair) {
		if !seen[pair] {
			seen[pair] = true
			out = append(out, pair)
		}
	}
	for k, v := range t.raw.LabelSelector.MatchLabels {
		add(labelPair{k, v})
	}
	for _, r := range t.raw.LabelSelector.MatchExpressions {
		if r.Operator != meta.LabelSelectorOpIn {
			continue
		}
		for _, v := range r.Values {
			add(labelPair{r.Key, v})
		}
	}
	return out
}

func (x *schedulingEvidence) existingIndexes(p *core.Pod) []int {
	if len(x.existing) == 0 {
		return nil
	}
	seen := make(map[int]bool, len(x.existingBroad)+len(p.Labels))
	out := make([]int, 0, len(x.existingBroad)+len(p.Labels))
	add := func(i int) {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	for _, i := range x.existingBroad {
		add(i)
	}
	for k, v := range p.Labels {
		for _, i := range x.existingByLabel[labelPair{k, v}] {
			add(i)
		}
	}
	sort.Ints(out)
	return out
}

func (x *schedulingEvidence) selectedPods(s model.Snapshot, terms []affinityTerm) []*core.Pod {
	selected := x.assigned
	for _, t := range terms {
		if t.unknown != "" || t.raw.LabelSelector == nil {
			continue
		}
		for k, v := range t.raw.LabelSelector.MatchLabels {
			if pods := x.byLabel[labelPair{k, v}]; len(pods) < len(selected) {
				selected = pods
			}
		}
		for _, r := range t.raw.LabelSelector.MatchExpressions {
			if r.Operator == meta.LabelSelectorOpIn && len(r.Values) == 1 {
				if pods := x.byLabel[labelPair{r.Key, r.Values[0]}]; len(pods) < len(selected) {
					selected = pods
				}
			}
		}
		if t.raw.NamespaceSelector == nil && len(t.raw.Namespaces) <= 1 {
			ns := t.ns
			if len(t.raw.Namespaces) == 1 {
				ns = t.raw.Namespaces[0]
			}
			if pods := s.NamespacePods(ns); len(pods) < len(selected) {
				selected = pods
			}
		}
	}
	return selected
}

type domainCounter struct {
	counts, uncertain map[string]int
	allUnknown        int
}

func counter() domainCounter {
	return domainCounter{counts: map[string]int{}, uncertain: map[string]int{}}
}
func (c *domainCounter) apply(s model.Snapshot, t affinityTerm, p *core.Pod, uncertain, affinity bool, delta int) {
	n := s.Node(p.Spec.NodeName)
	if n == nil {
		c.allUnknown += delta
		return
	}
	v, ok := n.Labels[t.raw.TopologyKey]
	if !ok {
		c.allUnknown += delta
		return
	}
	if uncertain || !model.Active(p) || (affinity && p.DeletionTimestamp != nil) {
		c.uncertain[v] += delta
	} else {
		c.counts[v] += delta
	}
}
func (c domainCounter) clone() domainCounter {
	return domainCounter{maps.Clone(c.counts), maps.Clone(c.uncertain), c.allUnknown}
}
func (c *domainCounter) subtract(b domainCounter) {
	for k, v := range b.counts {
		c.counts[k] -= v
	}
	for k, v := range b.uncertain {
		c.uncertain[k] -= v
	}
	c.allUnknown -= b.allUnknown
}

type domainAggregate struct {
	terms     []affinityTerm
	affinity  bool
	total     []domainCounter
	removed   map[string][]domainCounter
	projected map[string][]domainTerm
	members   map[*core.Pod]int // Multiplicity preserves exclusion of duplicate self terms.
}

func newDomainAggregate(terms []affinityTerm, affinity bool) *domainAggregate {
	d := &domainAggregate{terms: terms, affinity: affinity, removed: map[string][]domainCounter{}, projected: map[string][]domainTerm{}}
	for range terms {
		d.total = append(d.total, counter())
	}
	return d
}
func (d *domainAggregate) add(s model.Snapshot, p *core.Pod, uncertain bool) {
	for i, t := range d.terms {
		d.total[i].apply(s, t, p, uncertain, d.affinity, 1)
	}
	if model.Movable(s, p) {
		removed := d.removed[p.Spec.NodeName]
		if removed == nil {
			for range d.terms {
				removed = append(removed, counter())
			}
		}
		for i, t := range d.terms {
			removed[i].apply(s, t, p, uncertain, d.affinity, 1)
		}
		d.removed[p.Spec.NodeName] = removed
	}
}
func (d *domainAggregate) project(s model.Snapshot, p *core.Pod, target string, includeSelf bool) []domainTerm {
	exclude := includeSelf && p.Spec.NodeName != "" && !departing(s, p, target)
	if result, ok := d.projected[target]; ok && !exclude {
		return result
	}
	result := make([]domainTerm, len(d.terms))
	for i, t := range d.terms {
		c := d.total[i].clone()
		if removed := d.removed[target]; removed != nil {
			c.subtract(removed[i])
		}
		if exclude {
			match, u := allMatch(d.terms, p)
			multiplicity := 1
			if d.members != nil {
				multiplicity = d.members[p]
				match = multiplicity > 0
				u = false
			}
			if match || u {
				c.apply(s, t, p, u, d.affinity, -multiplicity)
			}
		}
		v := domain(t)
		for k, n := range c.counts {
			if n > 0 {
				v.counts[k] = n
			}
		}
		for k, n := range c.uncertain {
			if n > 0 {
				v.uncertain[k] = true
			}
		}
		v.allUnknown = c.allUnknown > 0
		result[i] = v
	}
	if !exclude {
		d.projected[target] = result
	}
	return result
}
func (x *schedulingEvidence) domains(ctx context.Context, s model.Snapshot, terms []affinityTerm, p *core.Pod, target string, affinity bool) []domainTerm {
	if len(terms) == 0 {
		return nil
	}
	keys := make([]string, len(terms))
	for i, t := range terms {
		keys[i] = x.peerAggregateKey(t)
	}
	key := strings.Join(keys, "\x01")
	if affinity {
		key = "affinity\x01" + key
	}
	d := x.peers[key]
	if d == nil {
		d = newDomainAggregate(terms, affinity)
		for _, other := range x.selectedPods(s, terms) {
			if ctx.Err() != nil {
				return nil
			}
			if other.Spec.NodeName == "" {
				continue
			}
			match, u := allMatch(terms, other)
			if match || u {
				d.add(s, other, u)
			}
		}
		x.peers[key] = d
	}
	return d.project(s, p, target, x.inventory[p])
}

// A valid DoesNotExist-only selector whose keys occur on no assigned Pod
// matches every observed peer. Its domain counts equal those of an empty
// selector, even when the raw absent keys differ. Reuse only the aggregate;
// keep original selectors for self-bootstrap/full matching and preserve all
// namespace/topology/UNKNOWN semantics. The key inventory is snapshot-local
// and includes terminal, terminating and missing-node assigned peers.
func (x *schedulingEvidence) peerAggregateKey(t affinityTerm) string {
	selector := t.raw.LabelSelector
	if t.unknown != "" || selector == nil || len(selector.MatchLabels) != 0 || len(selector.MatchExpressions) == 0 {
		return t.key
	}
	for _, r := range selector.MatchExpressions {
		if r.Operator != meta.LabelSelectorOpDoesNotExist || x.labelKeys[r.Key] {
			return t.key
		}
	}
	raw := t.raw
	raw.LabelSelector = &meta.LabelSelector{}
	return termKey(raw, t.ns)
}
