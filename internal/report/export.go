package report

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/hcie123/guard9s/internal/analyzer"
	"github.com/hcie123/guard9s/internal/model"
)

// Report is an explicit export schema, not a serialized Kubernetes snapshot.
// It excludes raw manifests, environment values and kubeconfig paths/contents.
type Report struct {
	SchemaVersion    string                      `json:"schemaVersion"`
	Version          string                      `json:"version"`
	CapturedAt       time.Time                   `json:"capturedAt"`
	Context          string                      `json:"context"`
	Node             string                      `json:"node"`
	AnalysisScope    string                      `json:"analysisScope"`
	ReadOnly         bool                        `json:"readOnly"`
	Readiness        string                      `json:"readiness"`
	Redaction        string                      `json:"redaction"`
	Redacted         bool                        `json:"redacted,omitempty"`
	RedactionMode    string                      `json:"redactionMode,omitempty"`
	CommandsRedacted bool                        `json:"commandsRedacted,omitempty"`
	Capabilities     map[string]model.Capability `json:"capabilities,omitempty"`
	Scheduling       []SchedulingEvidence        `json:"schedulingCandidates"`
	Findings         []ExportFinding             `json:"findings"`
	Capacity         ExportCapacity              `json:"capacity"`
	Plan             string                      `json:"plan"`
	Limitations      []string                    `json:"limitations"`
}

type SchedulingEvidence struct {
	Namespace string                         `json:"namespace"`
	Pod       string                         `json:"pod"`
	Nodes     []analyzer.CandidateEvaluation `json:"nodes"`
}
type Options struct{ Redact string }

type ExportFinding struct {
	Severity       string `json:"severity"`
	Unknown        bool   `json:"unknown"`
	Category       string `json:"category"`
	Namespace      string `json:"namespace"`
	Resource       string `json:"resource"`
	ResourceKind   string `json:"resourceKind,omitempty"`
	Reason         string `json:"reason"`
	Evidence       string `json:"evidence"`
	Recommendation string `json:"recommendation"`
}

type ResourceSummary struct {
	CPUMillicores int64 `json:"cpuMillicores"`
	MemoryBytes   int64 `json:"memoryBytes"`
	Pods          int64 `json:"pods"`
}

type ExportCapacity struct {
	Assessment    string          `json:"assessment"`
	Detail        string          `json:"detail"`
	Demand        ResourceSummary `json:"demand"`
	EligibleFree  ResourceSummary `json:"eligibleFree"`
	PendingDemand ResourceSummary `json:"pendingDemand"`
	EligibleNodes []string        `json:"eligibleNodes"`
}

func resources(r model.Resources) ResourceSummary {
	return ResourceSummary{CPUMillicores: r.CPU, MemoryBytes: r.Memory, Pods: r.Pods}
}

func Build(ctx context.Context, s model.Snapshot, node, version string, options ...Options) (Report, error) {
	mode := "none"
	if len(options) > 0 && options[0].Redact != "" {
		mode = options[0].Redact
	}
	if mode != "none" && mode != "basic" {
		return Report{}, fmt.Errorf("--redact must be none or basic")
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if node == "" || s.Node(node) == nil {
		return Report{}, fmt.Errorf("--node must name an existing node in the snapshot")
	}
	in := analyzer.Input{Snapshot: s, Node: node}
	diagnosis := analyzer.Diagnose(ctx, in)
	findings, capacity := diagnosis.Findings, diagnosis.Capacity
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	// Portable reports must not expose the operator's local kubeconfig path.
	planSnapshot := s
	planSnapshot.KubeconfigPath = ""
	capabilities := make(map[string]model.Capability, len(s.Capabilities))
	for resource, capability := range s.Capabilities {
		capabilities[resource] = capability
	}
	r := Report{
		SchemaVersion: "guard9s/v1", Version: version, CapturedAt: s.At,
		Context: s.Context, Node: node, AnalysisScope: "all namespaces", ReadOnly: true,
		Readiness: model.Readiness(findings), Findings: []ExportFinding{},
		Redaction: mode, Scheduling: []SchedulingEvidence{},
		Capabilities: capabilities,
		Capacity: ExportCapacity{Assessment: capacity.Assessment, Detail: capacity.Detail,
			Demand: resources(capacity.Demand), EligibleFree: resources(capacity.Free),
			PendingDemand: resources(capacity.Pending), EligibleNodes: append([]string{}, capacity.Eligible...)},
		Plan: plan(planSnapshot, node, findings, mode == "basic"),
		Limitations: []string{
			"Point-in-time evidence; informer snapshots are not cross-resource transactions and readiness does not guarantee availability.",
			analyzer.SchedulingDisclaimer,
			"Capacity uses requests, not live usage. Pending demand is reported but not allocated; preemption, unobserved CSI attachments and concurrent changes are not simulated.",
			"Evidence may contain workload names and event text. Review before sharing; arbitrary sensitive business text is not automatically redacted.",
			"Commands are suggestions only and must use the same trusted kubeconfig as collection; guard9s never executes them.",
		},
	}
	for _, f := range findings {
		r.Findings = append(r.Findings, ExportFinding{Severity: f.Severity.String(), Unknown: f.Unknown,
			Category: f.Category, Namespace: f.Namespace, Resource: f.Resource,
			ResourceKind: f.ResourceKind,
			Reason:       f.Reason, Evidence: f.Evidence, Recommendation: f.Recommendation})
	}
	for _, p := range s.NodePods(node) {
		if f, ok := diagnosis.Candidates[p.Namespace+"/"+p.Name]; ok {
			r.Scheduling = append(r.Scheduling, SchedulingEvidence{Namespace: p.Namespace, Pod: p.Name, Nodes: f.Evaluations})
		}
	}
	sort.Slice(r.Scheduling, func(i, j int) bool {
		return r.Scheduling[i].Namespace+"/"+r.Scheduling[i].Pod < r.Scheduling[j].Namespace+"/"+r.Scheduling[j].Pod
	})
	if mode == "basic" {
		r = basicRedaction(r, s)
	}
	return r, nil
}

// A longer fence keeps untrusted resource/event text inside literal blocks.
func literal(text string) string {
	longest, run := 0, 0
	for _, c := range text {
		if c == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + "text\n" + text + "\n" + fence + "\n\n"
}

func WriteMarkdown(w io.Writer, r Report) error {
	var b strings.Builder
	b.WriteString("# guard9s maintenance report\n\n")
	b.WriteString(literal(fmt.Sprintf("Schema: %s\nVersion: %s\nCaptured at: %s\nContext: %s\nNode: %s\nAnalysis scope: %s\nReadiness: %s\nREAD ONLY — commands are never executed.", r.SchemaVersion, r.Version, r.CapturedAt.Format(time.RFC3339), r.Context, r.Node, r.AnalysisScope, r.Readiness)))
	b.WriteString("## Capacity\n\n")
	for _, item := range []struct {
		name string
		r    ResourceSummary
	}{{"Relocation demand", r.Capacity.Demand}, {"Eligible free capacity", r.Capacity.EligibleFree}, {"Unscheduled demand", r.Capacity.PendingDemand}} {
		fmt.Fprintf(&b, "- %s: %d millicores CPU, %d bytes memory, %d pods\n", item.name, item.r.CPUMillicores, item.r.MemoryBytes, item.r.Pods)
	}
	b.WriteString("\n" + literal(fmt.Sprintf("Assessment: %s\nEligible nodes: %s\n%s", r.Capacity.Assessment, strings.Join(r.Capacity.EligibleNodes, ", "), r.Capacity.Detail)))
	b.WriteString("## Findings\n\n")
	for i, f := range r.Findings {
		fmt.Fprintf(&b, "### Finding %d\n\n", i+1)
		b.WriteString(literal(fmt.Sprintf("%s · %s · UNKNOWN=%t\nResource: %s/%s\nReason: %s\nEvidence: %s\nRecommendation: %s", f.Severity, f.Category, f.Unknown, f.Namespace, f.Resource, f.Reason, f.Evidence, f.Recommendation)))
	}
	b.WriteString("## Scheduling candidates\n\n")
	for _, pod := range r.Scheduling {
		var evidence strings.Builder
		fmt.Fprintf(&evidence, "Pod: %s/%s\n", pod.Namespace, pod.Pod)
		for _, n := range pod.Nodes {
			fmt.Fprintf(&evidence, "%s %s: %s\n", n.Node, n.Status, strings.Join(n.Reasons, "; "))
		}
		b.WriteString(literal(evidence.String()))
	}
	b.WriteString("## Maintenance plan\n\n" + literal(r.Plan))
	b.WriteString("## Limitations\n\n" + literal(strings.Join(r.Limitations, "\n")))
	_, err := io.WriteString(w, b.String())
	return err
}
