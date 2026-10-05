package analyzer

import (
	"context"
	"fmt"

	"github.com/hcie123/guard9s/internal/model"
	policy "k8s.io/api/policy/v1"
)

type PDBAnalyzer struct{}

func (PDBAnalyzer) Name() string { return "PDB" }

// PDBAssessment is shared with the budget view so stale allowances never pass.
func PDBAssessment(b *policy.PodDisruptionBudget) (model.Severity, bool) {
	if b.Status.ObservedGeneration < b.Generation {
		return model.High, true
	}
	if b.Status.DisruptionsAllowed <= 0 {
		return model.Critical, false
	}
	return model.Pass, false
}

func PDBEvidence(b *policy.PodDisruptionBudget) string {
	minAvailable, maxUnavailable := "unset", "unset"
	if b.Spec.MinAvailable != nil {
		minAvailable = b.Spec.MinAvailable.String()
	}
	if b.Spec.MaxUnavailable != nil {
		maxUnavailable = b.Spec.MaxUnavailable.String()
	}
	return fmt.Sprintf("PDB=%s/%s; minAvailable=%s; maxUnavailable=%s; currentHealthy=%d; desiredHealthy=%d; expectedPods=%d; disruptionsAllowed=%d; generation=%d; observedGeneration=%d", b.Namespace, b.Name, minAvailable, maxUnavailable, b.Status.CurrentHealthy, b.Status.DesiredHealthy, b.Status.ExpectedPods, b.Status.DisruptionsAllowed, b.Generation, b.Status.ObservedGeneration)
}
func (PDBAnalyzer) Analyze(_ context.Context, in Input) ([]model.Finding, error) {
	var out []model.Finding
	counts := map[string]int{}
	for _, p := range pods(in) {
		if !model.Movable(in.Snapshot, p) {
			continue
		}
		budgets, err := model.MatchingPDBs(in.Snapshot, p)
		if err != nil {
			out = append(out, podUnknown("PDB", p, "PDB selector could not be evaluated."))
			continue
		}
		if len(budgets) == 0 {
			out = append(out, podFinding(model.Info, "PDB", p, "No matching PDB.", "No policy/v1 PDB selects this pod in its namespace.", "Review availability requirements; absence of a PDB does not establish safety."))
			continue
		}
		if len(budgets) > 1 {
			out = append(out, podFinding(model.Critical, "PDB", p, "Multiple PDBs select this pod.", fmt.Sprintf("matched PDBs=%d", len(budgets)), "Resolve overlapping PDB selectors; the eviction API does not support multiple budgets for a pod."))
		}
		for _, b := range budgets {
			counts[b.Namespace+"/"+b.Name]++
			severity, stale := PDBAssessment(b)
			if stale {
				f := podUnknown("PDB", p, "PDB status is stale.")
				f.Evidence = PDBEvidence(b)
				out = append(out, f)
				continue
			}
			if severity == model.Critical {
				reason := "PodDisruptionBudget currently prevents voluntary eviction."
				if !model.Ready(p) {
					reason = "PDB has no disruption allowance; unhealthy eviction policy requires review."
				}
				evidence := PDBEvidence(b)
				if b.Spec.UnhealthyPodEvictionPolicy != nil {
					evidence += "; unhealthyPodEvictionPolicy=" + string(*b.Spec.UnhealthyPodEvictionPolicy)
				}
				out = append(out, podFinding(model.Critical, "PDB", p, reason, evidence, "Restore healthy replicas and recheck the budget. Unhealthy eviction exceptions do not override guard9s' conservative BLOCKED result."))
			} else {
				out = append(out, podFinding(model.Pass, "PDB", p, "PDB currently permits a disruption.", PDBEvidence(b), "Evict sequentially and re-evaluate the budget as replacements become Ready."))
			}
		}
	}
	for _, b := range in.Snapshot.PDBs {
		n := counts[b.Namespace+"/"+b.Name]
		severity, _ := PDBAssessment(b)
		if severity == model.Pass && n > int(b.Status.DisruptionsAllowed) {
			out = append(out, finding(model.Warn, "PDB", b.Namespace, b.Name, "Node has more matching pods than the current disruption allowance.", fmt.Sprintf("pods on target=%d; disruptionsAllowed=%d", n, b.Status.DisruptionsAllowed), "A drain must proceed sequentially and wait for replacements and budget recovery."))
		}
	}
	return out, nil
}
