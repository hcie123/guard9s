package analyzer

import (
	"context"
	"fmt"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
)

type ReplicaAnalyzer struct{}

func (ReplicaAnalyzer) Name() string { return "Replica" }
func (ReplicaAnalyzer) Analyze(_ context.Context, in Input) ([]model.Finding, error) {
	var out []model.Finding
	seen := map[string]bool{}
	for _, p := range pods(in) {
		o := in.Snapshot.Owner(p)
		if !o.Resolved {
			out = append(out, podUnknown("Replica", p, "Owner chain could not be resolved ("+o.Kind+")."))
			continue
		}
		switch o.Kind {
		case "StaticPod":
			out = append(out, podFinding(model.High, "Replica", p, "Static pod requires a separate maintenance runbook.", "Mirror pod annotation is present; drain skips mirror pods.", "Validate node-local static pod configuration and service availability."))
		case "Standalone":
			out = append(out, podFinding(model.High, "Replica", p, "Standalone pod has no controller to recreate it.", "No controlling ownerReference.", "Create and validate an appropriate controller; do not bypass drain safeguards."))
		case "DaemonSet":
			out = append(out, podFinding(model.Info, "Replica", p, "DaemonSet pod is not relocated by drain.", "owner="+o.Kind+"/"+o.Name, "Verify node-local agents recover after maintenance."))
		case "Job", "CronJob":
			out = append(out, podFinding(model.Warn, "Replica", p, "Batch workload may be interrupted or retried.", "owner="+o.Kind+"/"+o.Name, "Check job idempotency, active deadlines and completion before maintenance."))
		default:
			key := p.Namespace + "/" + o.Kind + "/" + o.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			ready, elsewhere := 0, 0
			for _, other := range in.Snapshot.OwnerPods(p.Namespace, o) {
				if other.Namespace != p.Namespace || !model.Ready(other) {
					continue
				}
				node := in.Snapshot.Node(other.Spec.NodeName)
				if node == nil || model.Condition(node, core.NodeReady) != core.ConditionTrue {
					continue
				}
				owner := in.Snapshot.Owner(other)
				if owner.Kind == o.Kind && owner.Name == o.Name && owner.UID == o.UID {
					ready++
					if other.Spec.NodeName != in.Node {
						elsewhere++
					}
				}
			}
			evidence := fmt.Sprintf("owner=%s/%s; desired=%d; observed ready pods=%d; ready on other nodes=%d; controller ready=%d", o.Kind, o.Name, o.Replicas, ready, elsewhere, o.ReadyReplicas)
			level := model.Pass
			reason := "Ready replicas exist on other nodes."
			recommendation := "Confirm application-level quorum and failover before maintenance."
			if ready <= 1 || o.Replicas == 1 {
				level = model.High
				reason = "Only one ready replica is available."
				if ready == 0 {
					reason = "No ready replicas are available."
				}
				if o.Replicas == 1 && ready > 1 {
					reason = "Workload is configured for a single replica."
				}
				recommendation = "Validate additional healthy replicas and application availability before maintenance."
			} else if elsewhere == 0 {
				level = model.High
				reason = "All ready replicas are on the target node."
				recommendation = "Spread replicas across healthy nodes before maintenance."
			}
			out = append(out, podFinding(level, "Replica", p, reason, evidence, recommendation))
			if o.Kind == "StatefulSet" && o.Replicas > 1 {
				out = append(out, podFinding(model.Warn, "Replica", p, "StatefulSet replicas do not prove application quorum.", evidence, "Confirm leader placement, replication health and workload-specific failover."))
			}
		}
	}
	return out, nil
}
