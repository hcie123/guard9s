// Package analyzer evaluates evidence without using a Kubernetes client or shell.
package analyzer

import (
	"context"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
)

type Input struct {
	Snapshot model.Snapshot
	Node     string
	// Memoized evidence belongs to one diagnosis only, never across snapshots.
	capacity   *Capacity
	candidates map[*core.Pod]Feasibility
	scheduling *schedulingEvidence
	ctx        context.Context
}

type Diagnosis struct {
	Findings   []model.Finding
	Capacity   Capacity
	Candidates map[string]Feasibility
}

// Diagnose shares placement evidence between scheduling, capacity and consumers.
func Diagnose(ctx context.Context, in Input) Diagnosis {
	in.ctx = ctx
	if ctx.Err() == nil {
		in.Snapshot = in.Snapshot.Indexed()
		if in.scheduling == nil {
			in.scheduling = newSchedulingEvidence(in.Snapshot)
		}
	}
	in.capacity = nil
	in.candidates = make(map[*core.Pod]Feasibility)
	c := Capacity{Assessment: "UNKNOWN", Detail: "Analysis cancelled; capacity is unavailable."}
	if ctx.Err() == nil {
		c = CalculateCapacity(in)
	}
	in.capacity = &c
	findings := Run(ctx, in)
	evidence := map[string]Feasibility{}
	for p, f := range in.candidates {
		evidence[p.Namespace+"/"+p.Name] = f
	}
	return Diagnosis{Findings: findings, Capacity: c, Candidates: evidence}
}

type Analyzer interface {
	Name() string
	Analyze(context.Context, Input) ([]model.Finding, error)
}

// AnalyzeAll shares immutable indexes and compiled existing anti-affinity terms.
// Candidate/capacity caches remain isolated to each node diagnosis.
func AnalyzeAll(ctx context.Context, s model.Snapshot) map[string]Diagnosis {
	out := map[string]Diagnosis{}
	if ctx.Err() != nil {
		return out
	}
	s = s.Indexed()
	x := newSchedulingEvidence(s)
	for _, n := range s.Nodes {
		if ctx.Err() != nil {
			break
		}
		out[n.Name] = Diagnose(ctx, Input{Snapshot: s, Node: n.Name, scheduling: x})
	}
	return out
}

func Default() []Analyzer {
	return []Analyzer{NodeHealthAnalyzer{}, PDBAnalyzer{}, ReplicaAnalyzer{}, StorageAnalyzer{}, SchedulingAnalyzer{}, CapacityAnalyzer{}, PodHealthAnalyzer{}, EventAnalyzer{}}
}

func Run(ctx context.Context, in Input, checks ...Analyzer) []model.Finding {
	in.ctx = ctx
	if ctx.Err() == nil {
		in.Snapshot = in.Snapshot.Indexed()
		if in.scheduling == nil {
			in.scheduling = newSchedulingEvidence(in.Snapshot)
		}
	}
	if len(checks) == 0 {
		checks = Default()
	}
	var result []model.Finding
	for _, issue := range in.Snapshot.Issues {
		result = append(result, unknown("Collection", in.Node, issue, "Restore read access and a healthy watch, then restart guard9s to resynchronize."))
	}
	if c, recorded := in.Snapshot.Capabilities[model.NamespacesResource]; recorded && c.State != model.Available && len(in.Snapshot.Issues) == 0 {
		result = append(result, unknown("Collection", in.Node, "Namespace collection evidence is "+string(c.State)+".", "Restore namespace get/list/watch access and resynchronize collection."))
	}
	for _, a := range checks {
		if err := ctx.Err(); err != nil {
			result = append(result, unknown(a.Name(), in.Node, "Analysis cancelled.", "Re-run diagnosis."))
			break
		}
		f, err := a.Analyze(ctx, in)
		if err != nil {
			result = append(result, unknown(a.Name(), in.Node, "Analyzer could not complete.", "Re-run diagnosis and inspect the analyzer."))
			continue
		}
		result = append(result, f...)
	}
	model.SortFindings(result)
	return result
}
func finding(level model.Severity, category, ns, resource, reason, evidence, recommendation string) model.Finding {
	kind := ""
	if ns == "" {
		kind = "Node"
	}
	return model.Finding{Severity: level, ResourceKind: kind, Category: category, Namespace: ns, Resource: resource, Reason: reason, Evidence: evidence, Recommendation: recommendation}
}
func unknown(category, resource, reason, recommendation string) model.Finding {
	x := finding(model.High, category, "", resource, reason, "UNKNOWN: incomplete evidence", recommendation)
	x.Unknown = true
	return x
}
func pods(in Input) []*core.Pod { return in.Snapshot.NodePods(in.Node) }
func podFinding(level model.Severity, category string, p *core.Pod, reason, evidence, recommendation string) model.Finding {
	f := finding(level, category, p.Namespace, p.Name, reason, evidence, recommendation)
	f.ResourceKind = "Pod"
	return f
}
func podUnknown(category string, p *core.Pod, reason string) model.Finding {
	f := unknown(category, p.Name, reason, "Review this workload and re-run diagnosis.")
	f.Namespace = p.Namespace
	f.ResourceKind = "Pod"
	return f
}
