package analyzer

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
)

const SchedulingDisclaimer = "Best-effort scheduling feasibility analysis; not a kube-scheduler simulation."

type CandidateEvaluation struct {
	Node    string   `json:"node"`
	Status  string   `json:"status"`
	Reasons []string `json:"reasons"`
}
type Feasibility struct {
	Nodes, Unknown []string
	Evaluations    []CandidateEvaluation
	Dynamic        bool // Interacting placements are not covered by resource packing.
}

func Candidates(s model.Snapshot, p *core.Pod, target string) Feasibility {
	s = s.WithLookups()
	return candidates(context.Background(), s, p, target, newSchedulingEvidence(s))
}

func candidates(ctx context.Context, s model.Snapshot, p *core.Pod, target string, x *schedulingEvidence) Feasibility {
	f := Feasibility{Nodes: make([]string, 0, len(s.Nodes)), Evaluations: make([]CandidateEvaluation, 0, len(s.Nodes))}
	u := podUncertainties(s, p)
	if c, recorded := s.Capabilities[model.NamespacesResource]; recorded && !c.Usable() {
		u = append(u, "Namespace collection evidence is unavailable")
	}
	if len(s.Issues) > 0 {
		u = append(u, "collection evidence is incomplete")
	}
	affinity := prepareAffinity(ctx, s, p, target, x)
	spread := prepareSpread(ctx, s, p, target, x)
	csi := podCSIVolumes(s, p)
	f.Dynamic = affinity.dynamic || len(spread) > 0
	var selectors []*core.NodeSelector
	for _, v := range p.Spec.Volumes {
		if v.PersistentVolumeClaim == nil {
			continue
		}
		pvc := s.PVC(p.Namespace, v.PersistentVolumeClaim.ClaimName)
		if pvc == nil {
			u = append(u, "PVC unavailable")
			continue
		}
		pv := s.PV(pvc.Spec.VolumeName)
		if pvc.Status.Phase != core.ClaimBound || pvc.DeletionTimestamp != nil || pv == nil {
			u = append(u, "unbound, terminating or unavailable PV")
			continue
		}
		if pv.DeletionTimestamp != nil || pv.Status.Phase == core.VolumeReleased || pv.Status.Phase == core.VolumeFailed {
			u = append(u, "PV is terminating or unavailable")
		}
		if pv.Spec.NodeAffinity != nil && pv.Spec.NodeAffinity.Required != nil {
			selectors = append(selectors, pv.Spec.NodeAffinity.Required)
		}
		if migratedStorage(pv) {
			u = append(u, "in-tree storage migration driver and attachment evidence unavailable")
		}
		if pv.Spec.Local != nil || pv.Spec.HostPath != nil {
			u = append(u, "node-local PV relocation")
		}
	}
	u = unique(u)
	f.Unknown = append(f.Unknown, u...)
	required := nodeaffinity.GetRequiredNodeAffinity(p)
	for _, n := range x.nodes {
		if n.Name == target {
			continue
		}
		e := CandidateEvaluation{Node: n.Name, Status: "ALLOWED"}
		var rejected, unresolved []string
		checked := make([]string, 0, 4)
		unresolved = append(unresolved, u...)
		if ctx.Err() != nil {
			unresolved = append(unresolved, "analysis cancelled")
		} else {
			if !model.Eligible(n) {
				rejected = append(rejected, "node is not Ready, schedulable and free of reported pressure")
			}
			if n.DeletionTimestamp != nil {
				rejected = append(rejected, "node is terminating")
			}
			for _, condition := range []core.NodeConditionType{core.NodeMemoryPressure, core.NodeDiskPressure, core.NodePIDPressure} {
				if model.Condition(n, condition) == core.ConditionUnknown {
					unresolved = append(unresolved, "destination "+string(condition)+" is UNKNOWN")
				}
			}
			ok, err := required.Match(n)
			if err != nil {
				unresolved = append(unresolved, "invalid node affinity")
			} else if !ok {
				rejected = append(rejected, "nodeSelector or required nodeAffinity does not match")
			} else {
				checked = append(checked, "nodeSelector and required nodeAffinity match")
			}
			hardTaint, taintUnknown := untolerated(p, n)
			if taintUnknown {
				unresolved = append(unresolved, "hard taint toleration is feature-gated or time-limited")
			} else if hardTaint {
				rejected = append(rejected, "untolerated NoSchedule/NoExecute taint")
			} else {
				checked = append(checked, "hard taints are tolerated")
			}
			for _, selector := range selectors {
				match, err := nodeaffinity.NewLazyErrorNodeSelector(selector).Match(n)
				if err != nil {
					unresolved = append(unresolved, "invalid PV affinity")
				} else if !match {
					rejected = append(rejected, "PV required node affinity does not match")
				} else {
					checked = append(checked, "PV required node affinity matches")
				}
			}
			ar, au, ac := affinity.check(n)
			rejected = append(rejected, ar...)
			unresolved = append(unresolved, au...)
			checked = append(checked, ac...)
			cr, cu, cc := csi.check(s, n, x)
			rejected = append(rejected, cr...)
			unresolved = append(unresolved, cu...)
			checked = append(checked, cc...)
			for _, c := range spread {
				r, v, proof := c.check(n, p)
				if r != "" {
					rejected = append(rejected, r)
				}
				if v != "" {
					unresolved = append(unresolved, v)
				}
				if proof != "" && r == "" {
					checked = append(checked, proof)
				}
			}
		}
		if len(rejected) > 0 {
			e.Status = "REJECTED"
		} else {
			f.Nodes = append(f.Nodes, n.Name)
			if len(unresolved) > 0 {
				e.Status = "UNKNOWN"
			}
		}
		e.Reasons = append(checked, rejected...)
		e.Reasons = append(e.Reasons, unresolved...)
		e.Reasons = unique(e.Reasons)
		f.Unknown = append(f.Unknown, unresolved...)
		f.Evaluations = append(f.Evaluations, e)
	}
	f.Unknown = unique(f.Unknown)
	return f
}

func unique(v []string) []string { sort.Strings(v); return slices.Compact(v) }
func CandidateText(f Feasibility) string {
	var b strings.Builder
	size := 100
	for _, e := range f.Evaluations {
		size += len(e.Node) + len(e.Status) + 8
		for _, r := range e.Reasons {
			size += len(r) + 5
		}
	}
	b.Grow(size)
	b.WriteString("Candidate nodes (post-removal projection; ALLOWED covers checked constraints only):")
	b.WriteByte('\n')
	for _, state := range []string{"ALLOWED", "REJECTED", "UNKNOWN"} {
		count := 0
		for _, e := range f.Evaluations {
			if e.Status == state {
				count++
			}
		}
		fmt.Fprintf(&b, "\n%s (%d)\n", state, count)
		if count == 0 {
			b.WriteString("  (none observed)\n")
			continue
		}
		for _, e := range f.Evaluations {
			if e.Status != state {
				continue
			}
			b.WriteString("  " + e.Node + "\n")
			for _, r := range e.Reasons {
				b.WriteString("    " + r + "\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func podCandidates(in Input, p *core.Pod) Feasibility {
	if f, ok := in.candidates[p]; ok {
		return f
	}
	x := in.scheduling
	if x == nil {
		x = newSchedulingEvidence(in.Snapshot)
	}
	ctx := in.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	f := candidates(ctx, in.Snapshot, p, in.Node, x)
	if in.candidates != nil {
		in.candidates[p] = f
	}
	return f
}

func pinnedTemplate(s model.Snapshot, p *core.Pod) bool {
	o := s.Owner(p)
	switch o.Kind {
	case "Deployment":
		for _, x := range s.Deployments {
			if x.Namespace == p.Namespace && x.Name == o.Name {
				return x.Spec.Template.Spec.NodeName != ""
			}
		}
	case "ReplicaSet":
		for _, x := range s.ReplicaSets {
			if x.Namespace == p.Namespace && x.Name == o.Name {
				return x.Spec.Template.Spec.NodeName != ""
			}
		}
	case "StatefulSet":
		for _, x := range s.StatefulSets {
			if x.Namespace == p.Namespace && x.Name == o.Name {
				return x.Spec.Template.Spec.NodeName != ""
			}
		}
	case "Job":
		for _, x := range s.Jobs {
			if x.Namespace == p.Namespace && x.Name == o.Name {
				return x.Spec.Template.Spec.NodeName != ""
			}
		}
	case "CronJob":
		for _, x := range s.CronJobs {
			if x.Namespace == p.Namespace && x.Name == o.Name {
				return x.Spec.JobTemplate.Spec.Template.Spec.NodeName != ""
			}
		}
	}
	return false
}

type SchedulingAnalyzer struct{}

func (SchedulingAnalyzer) Name() string { return "Scheduling" }
func (SchedulingAnalyzer) Analyze(ctx context.Context, in Input) ([]model.Finding, error) {
	in.ctx = ctx
	var out []model.Finding
	for _, p := range pods(in) {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if !model.Movable(in.Snapshot, p) {
			continue
		}
		f := podCandidates(in, p)
		var item model.Finding
		switch {
		case len(f.Unknown) > 0:
			item = podUnknown("Scheduling", p, strings.Join(f.Unknown, ", ")+" is not fully evaluated.")
		case len(f.Nodes) == 0:
			item = podFinding(model.High, "Scheduling", p, "No other eligible node satisfies checked hard constraints.", "", "Review selectors, affinity, taints, topology and storage or add suitable capacity.")
		default:
			item = podFinding(model.Pass, "Scheduling", p, "Other nodes satisfy checked hard constraints.", "", "Validate placement with the current scheduler configuration; preferred constraints are not simulated.")
		}
		allowed, rejected, unresolved := 0, 0, 0
		for _, e := range f.Evaluations {
			switch e.Status {
			case "ALLOWED":
				allowed++
			case "REJECTED":
				rejected++
			default:
				unresolved++
			}
		}
		item.Evidence = fmt.Sprintf("Candidate nodes: ALLOWED=%d REJECTED=%d UNKNOWN=%d. Detailed reasons are in resource detail and report schedulingCandidates. %s", allowed, rejected, unresolved, SchedulingDisclaimer)
		out = append(out, item)
	}
	return out, nil
}

func tolerates(t core.Toleration, taint core.Taint) bool {
	if t.Effect != "" && t.Effect != taint.Effect {
		return false
	}
	if t.Key != "" && t.Key != taint.Key {
		return false
	}
	switch t.Operator {
	case core.TolerationOpExists:
		return true
	case "", core.TolerationOpEqual:
		return t.Value == taint.Value
	}
	return false
}
func untolerated(p *core.Pod, n *core.Node) (bool, bool) {
	for _, taint := range n.Spec.Taints {
		if taint.Effect != core.TaintEffectNoSchedule && taint.Effect != core.TaintEffectNoExecute {
			continue
		}
		ok, uncertain := false, false
		for _, t := range p.Spec.Tolerations {
			if tolerates(t, taint) {
				if taint.Effect == core.TaintEffectNoExecute && t.TolerationSeconds != nil {
					return false, true
				}
				ok = true
				break
			}
			if t.Operator != "" && t.Operator != core.TolerationOpEqual && t.Operator != core.TolerationOpExists {
				uncertain = true
			}
		}
		if !ok {
			return true, uncertain
		}
	}
	return false, false
}
