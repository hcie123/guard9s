package analyzer

import (
	"fmt"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	podresource "k8s.io/component-helpers/resource"
)

func podUncertainties(s model.Snapshot, p *core.Pod) []string {
	var out []string
	if p.Spec.SchedulerName != "" && p.Spec.SchedulerName != "default-scheduler" {
		out = append(out, "custom scheduler")
	}
	if len(p.Spec.SchedulingGates) > 0 {
		out = append(out, "scheduling gates")
	}
	if len(p.Spec.ResourceClaims) > 0 {
		out = append(out, "dynamic resource claims")
	}
	if p.Spec.HostNetwork {
		out = append(out, "host network constraints")
	}
	for _, containers := range [][]core.Container{p.Spec.Containers, p.Spec.InitContainers} {
		for _, c := range containers {
			for _, port := range c.Ports {
				if port.HostPort > 0 {
					out = append(out, "hostPort availability")
				}
			}
		}
	}
	for name, q := range podresource.PodRequests(p, podresource.PodResourcesOptions{UseStatusResources: true}) {
		if name != core.ResourceCPU && name != core.ResourceMemory && !q.IsZero() {
			out = append(out, "unmodeled resource "+string(name))
		}
	}
	for _, t := range p.Spec.Tolerations {
		if t.Operator != "" && t.Operator != core.TolerationOpEqual && t.Operator != core.TolerationOpExists {
			out = append(out, "feature-gated toleration operator")
		}
	}
	for _, v := range p.Spec.Volumes {
		if v.CSI != nil || v.Ephemeral != nil {
			out = append(out, "inline or generic ephemeral storage evidence unavailable")
		}
	}
	if pinnedTemplate(s, p) {
		out = append(out, "controller template pins spec.nodeName")
	}
	return out
}

type spreadEvidence struct {
	raw          core.TopologySpreadConstraint
	selector     labels.Selector
	counts       map[string]int
	minimum      int
	unknown      string
	countedNodes map[string]bool
	proofs       map[string]spreadProof
}

func mergedLabel(s *meta.LabelSelector, key, value string) bool {
	if s == nil {
		return false
	}
	if v, ok := s.MatchLabels[key]; ok && v == value {
		return true
	}
	for _, r := range s.MatchExpressions {
		if r.Key == key && r.Operator == meta.LabelSelectorOpIn && len(r.Values) == 1 && r.Values[0] == value {
			return true
		}
	}
	return false
}

type spreadProof struct{ rejected, checked string }

func (x spreadEvidence) check(n *core.Node, p *core.Pod) (rejected, unknown, checked string) {
	if x.unknown != "" {
		return "", x.unknown, ""
	}
	v, ok := n.Labels[x.raw.TopologyKey]
	if !ok {
		return "hard topology spread label is missing: " + x.raw.TopologyKey, "", ""
	}
	self := 0
	if x.selector.Matches(labels.Set(p.Labels)) {
		self = 1
	}
	key := fmt.Sprintf("%s/%d", v, self)
	if proof, ok := x.proofs[key]; ok {
		return proof.rejected, "", proof.checked
	}
	count := x.counts[v]
	skew := count + self - x.minimum
	checked = fmt.Sprintf("topology %s=%s count=%d incoming=%d global minimum=%d eligible domains=%d skew=%d maxSkew=%d", x.raw.TopologyKey, v, count, self, x.minimum, len(x.counts), skew, x.raw.MaxSkew)
	if skew > int(x.raw.MaxSkew) {
		rejected = "hard topology spread exceeds maxSkew: " + checked
	}
	if x.proofs != nil {
		x.proofs[key] = spreadProof{rejected, checked}
	}
	return
}
