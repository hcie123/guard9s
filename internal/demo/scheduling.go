package demo

import (
	"fmt"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func scheduling(s model.Snapshot) model.Snapshot {
	for i, n := range s.Nodes {
		n.Labels[core.LabelTopologyZone] = fmt.Sprintf("zone-%d", i+1)
	}
	s.Nodes[2].Spec.Taints = []core.Taint{{Key: "dedicated", Value: "synthetic", Effect: core.TaintEffectNoSchedule}}
	s.Pods[0].Spec.NodeSelector = map[string]string{"pool": "general"}
	s.Pods[1].Spec.Affinity = &core.Affinity{NodeAffinity: &core.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &core.NodeSelector{NodeSelectorTerms: []core.NodeSelectorTerm{{MatchExpressions: []core.NodeSelectorRequirement{{Key: core.LabelHostname, Operator: core.NodeSelectorOpIn, Values: []string{"worker-02"}}}}}}}}
	term := core.PodAffinityTerm{TopologyKey: core.LabelHostname, LabelSelector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "nginx"}}}
	s.Pods[2].Spec.Affinity = &core.Affinity{PodAffinity: &core.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{term}}}
	s.Pods[3].Spec.Affinity = &core.Affinity{PodAntiAffinity: &core.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{term}}}
	s.Pods[4].Spec.TopologySpreadConstraints = []core.TopologySpreadConstraint{{MaxSkew: 1, TopologyKey: core.LabelTopologyZone, WhenUnsatisfiable: core.DoNotSchedule, LabelSelector: term.LabelSelector}}
	s.Pods[5].Spec.Containers[0].Ports = []core.ContainerPort{{ContainerPort: 8080, HostPort: 8080}}
	s.Pods[6].Spec.Volumes = []core.Volume{{Name: "data", VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: "synthetic-local-claim"}}}}
	s.PVCs = []*core.PersistentVolumeClaim{{ObjectMeta: metadata("demo", "synthetic-local-claim"), Spec: core.PersistentVolumeClaimSpec{VolumeName: "synthetic-local-volume", AccessModes: []core.PersistentVolumeAccessMode{core.ReadWriteOnce}}, Status: core.PersistentVolumeClaimStatus{Phase: core.ClaimBound}}}
	s.PVs = []*core.PersistentVolume{{ObjectMeta: metadata("", "synthetic-local-volume"), Spec: core.PersistentVolumeSpec{PersistentVolumeSource: core.PersistentVolumeSource{Local: &core.LocalVolumeSource{Path: "/var/lib/synthetic"}}, NodeAffinity: &core.VolumeNodeAffinity{Required: &core.NodeSelector{NodeSelectorTerms: []core.NodeSelectorTerm{{MatchExpressions: []core.NodeSelectorRequirement{{Key: core.LabelHostname, Operator: core.NodeSelectorOpIn, Values: []string{"worker-01"}}}}}}}}, Status: core.PersistentVolumeStatus{Phase: core.VolumeBound}}}
	return s
}
