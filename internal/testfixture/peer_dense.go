package testfixture

import (
	"fmt"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PeerDense deliberately gives every assigned pod required placement terms.
// Selectors recur across controllers and match many cross-namespace peers.
func PeerDense(nodes, pods int) model.Snapshot {
	s := Snapshot(nodes, pods)
	for i, p := range s.Pods {
		p.Labels["pool"] = "shared"
		p.Labels["peer-group"] = fmt.Sprintf("group-%02d", i%32)
		affinity := core.PodAffinityTerm{TopologyKey: core.LabelTopologyZone,
			LabelSelector:     &meta.LabelSelector{MatchLabels: map[string]string{"pool": "shared"}},
			NamespaceSelector: &meta.LabelSelector{MatchLabels: map[string]string{"tenant": "shared"}}}
		anti := core.PodAffinityTerm{TopologyKey: core.LabelHostname,
			LabelSelector:     &meta.LabelSelector{MatchLabels: map[string]string{"peer-group": p.Labels["peer-group"]}},
			NamespaceSelector: &meta.LabelSelector{}}
		p.Spec.Affinity = &core.Affinity{
			PodAffinity:     &core.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{affinity}},
			PodAntiAffinity: &core.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{anti}},
		}
		p.Spec.TopologySpreadConstraints = []core.TopologySpreadConstraint{{
			TopologyKey: core.LabelTopologyZone, MaxSkew: 8, WhenUnsatisfiable: core.DoNotSchedule,
			LabelSelector: &meta.LabelSelector{MatchLabels: map[string]string{"pool": "shared"}},
		}}
	}
	return s
}
