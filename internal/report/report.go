// Package report generates human-readable evidence and command suggestions only.
package report

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hcie123/guard9s/internal/model"
)

func Finding(f model.Finding) string {
	state := f.Severity.String()
	if f.Unknown {
		state += " / UNKNOWN"
	}
	return fmt.Sprintf("%s · %s\nResource: %s/%s\nReason: %s\nEvidence: %s\nRecommendation: %s", state, f.Category, f.Namespace, f.Resource, f.Reason, f.Evidence, f.Recommendation)
}
func Diagnosis(node string, findings []model.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Node diagnosis: %s\nMaintenance readiness: %s\n\n", node, model.Readiness(findings))
	for _, f := range findings {
		b.WriteString(Finding(f) + "\n\n")
	}
	return b.String()
}
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// PlanFindings protects both the plan summary and its suggested commands.
func PlanFindings(s model.Snapshot, node string, findings []model.Finding) []model.Finding {
	incomplete := len(s.Issues) > 0
	for _, f := range findings {
		if f.Category == "Collection" && f.Unknown {
			incomplete = false // The diagnosis already contains collection evidence.
		}
	}
	if node == "" || s.Node(node) == nil || len(findings) == 0 || incomplete {
		findings = append(append([]model.Finding{}, findings...), model.Finding{Severity: model.High, Category: "Node", Resource: node, Reason: "No complete node diagnosis is available.", Evidence: "UNKNOWN: target or analysis is missing, or collection is incomplete.", Recommendation: "Select an existing node, restore collection and re-run diagnosis.", Unknown: true})
	}
	return findings
}

func Plan(s model.Snapshot, node string, findings []model.Finding) string {
	return plan(s, node, findings, false)
}

func plan(s model.Snapshot, node string, findings []model.Finding, redacted bool) string {
	findings = PlanFindings(s, node, findings)
	state := model.Readiness(findings)
	displayNode, displayContext := node, s.Context
	if redacted {
		displayNode, displayContext = "<redacted-node>", "<redacted-context>"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "MAINTENANCE PLAN · %s\nContext: %s | Scope: ALL namespaces\nReadiness: %s\n\nDISPLAY ONLY — commands are never executed.\n\nPreflight evidence\n", displayNode, displayContext, state)
	ordered := slices.Clone(findings)
	model.SortFindings(ordered)
	for _, section := range []string{"BLOCKED BY", "UNKNOWN", "WARNINGS"} {
		b.WriteString("\n" + section + "\n")
		seen := map[string]bool{}
		count := 0
		for _, f := range ordered {
			include := (section == "UNKNOWN" && f.Unknown) || (section == "BLOCKED BY" && !f.Unknown && f.Severity >= model.High) || (section == "WARNINGS" && !f.Unknown && f.Severity == model.Warn)
			key := f.Namespace + "/" + f.Resource + "/" + f.Category + "/" + f.Reason + "/" + f.Evidence
			if include && !seen[key] {
				seen[key] = true
				count++
				fmt.Fprintf(&b, "%s %s/%s: %s\n  Evidence: %s\n  Review: %s\n", f.Severity, f.Namespace, f.Resource, f.Reason, f.Evidence, f.Recommendation)
			}
		}
		if count == 0 {
			b.WriteString("(none observed)\n")
		}
	}
	base := "kubectl"
	if s.KubeconfigPath != "" && !redacted {
		base += " --kubeconfig " + quote(s.KubeconfigPath)
	}
	base += " --context " + quote(displayContext)
	fmt.Fprintf(&b, "\nVALIDATE — Suggested validation (use the same kubeconfig as guard9s)\n%s get node %s -o wide\n%s get pods -A --field-selector %s\n%s get pdb -A\n", base, quote(displayNode), base, quote("spec.nodeName="+displayNode), base)
	if state == "BLOCKED" || state == "NOT RECOMMENDED" {
		b.WriteString("\nMaintenance cannot currently be recommended.\n1. Resolve blocking and UNKNOWN findings.\n2. Review workload availability, PDBs and storage relocation.\n3. Validate spare capacity and traffic convergence.\n4. Re-run diagnosis.\n\nNo cordon/drain commands are suggested for this result.")
		return b.String()
	}
	b.WriteString("\nOnly after validation — operator review and current application health are required.\n")
	b.WriteString("\nSuggested sequence (operator-reviewed, one node at a time)\n1. Validate Node and application health.\n2. Validate PDBs; plan sequential evictions and budget recovery.\n3. Validate spare capacity, storage and warnings above.\n4. Cordon the node.\n5. Wait for traffic and routing convergence using your runbook.\n6. Drain and monitor replacements. Stop on any safeguard/error.\n7. Perform approved maintenance.\n8. Validate kubelet and node-local agents.\n9. Validate Node Ready and network reachability.\n10. Validate workloads, replication and application traffic.\n11. Restore the node's original scheduling state.\n\nSuggested commands — manual review required\n")
	fmt.Fprintf(&b, "%s cordon %s\n%s drain %s --ignore-daemonsets --timeout=10m\n%s wait --for=condition=Ready %s --timeout=5m\n", base, quote(displayNode), base, quote(displayNode), base, quote("node/"+displayNode))
	if n := s.Node(node); n != nil && !n.Spec.Unschedulable {
		fmt.Fprintf(&b, "%s uncordon %s\n", base, quote(displayNode))
	} else {
		b.WriteString("Node was already unschedulable: keep it cordoned unless explicitly approved.\n")
	}
	b.WriteString("\nNo --force, --disable-eviction or --delete-emptydir-data is suggested. A drain can stop on emptyDir or unmanaged pods; resolve the reason manually. Readiness is evidence from a point-in-time snapshot, not an availability guarantee.")
	return b.String()
}
