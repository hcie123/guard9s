package analyzer

import (
	"context"
	"fmt"
	"time"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
)

type NodeHealthAnalyzer struct{}

func (NodeHealthAnalyzer) Name() string { return "Node" }
func (NodeHealthAnalyzer) Analyze(_ context.Context, in Input) ([]model.Finding, error) {
	n := in.Snapshot.Node(in.Node)
	if n == nil {
		return []model.Finding{unknown("Node", in.Node, "Node is missing from the snapshot.", "Refresh the cluster cache.")}, nil
	}
	var out []model.Finding
	for _, typ := range []core.NodeConditionType{core.NodeReady, core.NodeMemoryPressure, core.NodeDiskPressure, core.NodePIDPressure, core.NodeNetworkUnavailable} {
		value := model.Condition(n, typ)
		expected := core.ConditionFalse
		if typ == core.NodeReady {
			expected = core.ConditionTrue
		}
		if value == core.ConditionUnknown {
			if typ == core.NodeNetworkUnavailable {
				out = append(out, finding(model.Info, "Node", "", n.Name, "Network condition not reported.", "NetworkUnavailable=UNKNOWN; not all CNIs publish this condition.", "Validate connectivity using your network runbook."))
				continue
			}
			out = append(out, unknown("Node", n.Name, string(typ)+" condition is UNKNOWN.", "Inspect kubelet and node conditions."))
			continue
		}
		level := model.Pass
		reason := string(typ) + "=" + string(value)
		recommendation := "Recheck immediately before maintenance."
		if value != expected {
			level = model.High
			recommendation = "Resolve node health issues before maintenance."
		}
		out = append(out, finding(level, "Node", "", n.Name, reason, "status.conditions: "+reason, recommendation))
	}
	if n.Spec.Unschedulable {
		out = append(out, finding(model.Info, "Node", "", n.Name, "Node is already unschedulable.", "spec.unschedulable=true", "Confirm this is intentional; preserve its original scheduling state."))
	}
	if _, ok := n.Labels["node-role.kubernetes.io/control-plane"]; ok {
		out = append(out, finding(model.High, "Node", "", n.Name, "Control-plane maintenance needs a quorum-aware runbook.", "Control-plane role label is present.", "Validate etcd quorum and API availability; V1 does not evaluate quorum."))
	}
	return out, nil
}

type PodHealthAnalyzer struct{}

func (PodHealthAnalyzer) Name() string { return "PodHealth" }
func (PodHealthAnalyzer) Analyze(_ context.Context, in Input) ([]model.Finding, error) {
	var out []model.Finding
	for _, p := range pods(in) {
		if p.DeletionTimestamp != nil && in.Snapshot.At.Sub(p.DeletionTimestamp.Time) > 5*time.Minute {
			out = append(out, podFinding(model.High, "PodHealth", p, "Pod is stuck Terminating.", "Deletion timestamp older than 5 minutes.", "Review finalizers, storage and kubelet health."))
		}
		if p.Status.Phase == core.PodPending || p.Status.Phase == core.PodUnknown {
			out = append(out, podFinding(model.High, "PodHealth", p, "Pod phase is "+string(p.Status.Phase)+".", "status.phase="+string(p.Status.Phase), "Review events and scheduling constraints."))
		}
		statuses := append(append([]core.ContainerStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...)
		for _, c := range statuses {
			if w := c.State.Waiting; w != nil {
				bad := w.Reason == "CrashLoopBackOff" || w.Reason == "ImagePullBackOff" || w.Reason == "ErrImagePull"
				longCreating := w.Reason == "ContainerCreating" && in.Snapshot.At.Sub(p.CreationTimestamp.Time) > 10*time.Minute
				if bad || longCreating {
					out = append(out, podFinding(model.High, "PodHealth", p, w.Reason, "container="+c.Name+"; waiting.reason="+w.Reason, "Review container events, image access and volume mounts."))
				}
			}
			if (c.State.Terminated != nil && c.State.Terminated.Reason == "OOMKilled") || (c.LastTerminationState.Terminated != nil && c.LastTerminationState.Terminated.Reason == "OOMKilled") {
				out = append(out, podFinding(model.High, "PodHealth", p, "OOMKilled recorded.", "container="+c.Name+"; current or last termination reason=OOMKilled (historical)", "Check memory requests/limits and workload memory usage; confirm recovery."))
			}
			if c.RestartCount >= 5 {
				out = append(out, podFinding(model.Warn, "PodHealth", p, "High restart count.", fmt.Sprintf("container=%s; cumulative restarts=%d", c.Name, c.RestartCount), "Correlate restart timestamps with events; cumulative counts do not prove an active incident."))
			}
		}
		if p.Status.Phase == core.PodRunning && !model.Ready(p) && p.DeletionTimestamp == nil {
			out = append(out, podFinding(model.High, "PodHealth", p, "Running pod is not Ready.", "Pod Ready condition is not True.", "Restore readiness before maintenance."))
		}
	}
	return out, nil
}

type EventAnalyzer struct{}

func (EventAnalyzer) Name() string { return "Events" }
func (EventAnalyzer) Analyze(_ context.Context, in Input) ([]model.Finding, error) {
	var out []model.Finding
	for _, e := range model.RelevantEvents(in.Snapshot, in.Node) {
		age := in.Snapshot.At.Sub(model.EventTime(e))
		if age > time.Hour {
			continue
		}
		f := finding(model.Warn, "Events", e.InvolvedObject.Namespace, e.InvolvedObject.Name, "Recent "+e.Reason+" event.", fmt.Sprintf("type=%s; count=%d; last seen=%s; %s", e.Type, e.Count, model.EventTime(e).Format(time.RFC3339), e.Message), "Correlate this event with current conditions; historical events alone do not prove an active failure.")
		f.ResourceKind = e.InvolvedObject.Kind
		out = append(out, f)
	}
	return out, nil
}
