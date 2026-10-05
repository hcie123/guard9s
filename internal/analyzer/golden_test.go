package analyzer

import (
	"testing"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	"github.com/hcie123/guard9s/internal/testfixture"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSchedulingEvidenceGoldens(t *testing.T) {
	s := affinityFixture()
	s.Pods[0].Spec.Affinity = &core.Affinity{PodAffinity: &core.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{affinityRequirement()}}}
	testfixture.Golden(t, "scheduling-affinity.txt", []byte(CandidateText(Candidates(s, s.Pods[0], "worker-01"))+"\n"))
	s = affinityFixture()
	s.Pods[0].Labels["app"] = "peer"
	s.Pods[0].Spec.TopologySpreadConstraints = []core.TopologySpreadConstraint{{TopologyKey: core.LabelTopologyZone, MaxSkew: 1, WhenUnsatisfiable: core.DoNotSchedule, LabelSelector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "peer"}}}}
	testfixture.Golden(t, "topology-spread.txt", []byte(CandidateText(Candidates(s, s.Pods[0], "worker-01"))+"\n"))
}
func TestStoragePhasesAndAffinityEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*model.Snapshot)
		reason string
	}{
		{"claim_lost", func(s *model.Snapshot) { s.PVCs[0].Status.Phase = core.ClaimLost }, "PVC is terminating or Lost"},
		{"claim_terminating", func(s *model.Snapshot) { s.PVCs[0].DeletionTimestamp = ptr(meta.NewTime(s.At)) }, "PVC is terminating or Lost"},
		{"volume_released", func(s *model.Snapshot) { s.PVs[0].Status.Phase = core.VolumeReleased }, "PV is terminating, Released or Failed"},
		{"volume_failed", func(s *model.Snapshot) { s.PVs[0].Status.Phase = core.VolumeFailed }, "PV is terminating, Released or Failed"},
		{"volume_terminating", func(s *model.Snapshot) { s.PVs[0].DeletionTimestamp = ptr(meta.NewTime(s.At)) }, "PV is terminating, Released or Failed"},
		{"affinity_conflict", func(s *model.Snapshot) {
			s.PVs[0].Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"worker-03"}
		}, "PV affinity conflicts"},
		{"invalid_affinity", func(s *model.Snapshot) {
			s.PVs[0].Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Operator = "invalid"
		}, "PV affinity is invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := demo.Snapshot("mixed")
			tc.change(&s)
			if f := run(t, StorageAnalyzer{}, s); !has(f, model.High, tc.reason) {
				t.Fatal(f)
			}
			if tc.name == "claim_lost" || tc.name == "claim_terminating" || tc.name == "volume_terminating" {
				for _, p := range s.Pods {
					if p.Name == "redis-0" && len(Candidates(s, p, "worker-01").Unknown) == 0 {
						t.Fatal("storage state did not affect candidates")
					}
				}
			}
		})
	}
}
