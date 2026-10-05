package analyzer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
)

type Capacity struct {
	Demand, Free, Pending model.Resources
	Eligible              []string
	Assessment, Detail    string
}

func CalculateCapacity(in Input) (c Capacity) {
	if in.ctx == nil {
		in.ctx = context.Background()
	}
	if in.ctx.Err() != nil {
		c.Assessment = "UNKNOWN"
		c.Detail = "Analysis cancelled; capacity is unavailable."
		return c
	}
	in.Snapshot = in.Snapshot.Indexed()
	if in.scheduling == nil {
		in.scheduling = newSchedulingEvidence(in.Snapshot)
	}
	c.Assessment = "PASS"
	if in.Node == "" || in.Snapshot.Node(in.Node) == nil {
		c.Assessment = "UNKNOWN"
		c.Detail = "Target node is absent from the snapshot; relocation capacity cannot be assessed."
		return c
	}
	var uncertainties []string
	if len(in.Snapshot.Issues) > 0 {
		uncertainties = append(uncertainties, "Collection evidence is incomplete.")
	}
	c.Pending = in.Snapshot.PendingRequests()
	if c.Pending.Pods > 0 {
		uncertainties = append(uncertainties, fmt.Sprintf("Unscheduled demand (%s) is not reserved in this estimate; review competing demand before maintenance.", c.Pending))
	}
	// Incomplete evidence must remain UNKNOWN even for an empty target node.
	defer func() {
		if in.ctx.Err() != nil {
			uncertainties = append(uncertainties, "Analysis cancelled.")
		}
		if len(uncertainties) > 0 {
			c.Assessment = "UNKNOWN"
			c.Detail += " " + strings.Join(uncertainties, " ")
		}
	}()
	free := map[string]model.Resources{}
	candidates := map[string]Feasibility{}
	requests := map[*core.Pod]model.Resources{}
	volumes := map[*core.Pod]csiVolumes{}
	var moving []*core.Pod
	unknown := false
	dynamic := false
	for _, p := range pods(in) {
		if in.ctx.Err() != nil {
			return c
		}
		if !model.Movable(in.Snapshot, p) {
			continue
		}
		moving = append(moving, p)
		r := in.Snapshot.PodRequests(p)
		requests[p] = r
		volumes[p] = podCSIVolumes(in.Snapshot, p)
		c.Demand = c.Demand.Add(r)
		if r.CPU == 0 || r.Memory == 0 {
			unknown = true
		}
		f := podCandidates(in, p)
		dynamic = dynamic || f.Dynamic
		candidates[p.Namespace+"/"+p.Name] = f
		if len(f.Unknown) > 0 {
			unknown = true
		}
		for _, name := range f.Nodes {
			if _, checked := free[name]; !checked {
				free[name] = in.Snapshot.Free(in.Snapshot.Node(name))
			}
		}
	}
	if dynamic && len(moving) > 1 {
		uncertainties = append(uncertainties, "Interacting affinity/topology placements for multiple departing pods are not simulated; per-pod candidates are a static post-removal projection.")
	}
	for name, r := range free {
		c.Free = c.Free.Add(r)
		c.Eligible = append(c.Eligible, name)
	}
	sort.Strings(c.Eligible)
	if len(moving) == 0 {
		c.Detail = "No active relocatable pods; DaemonSet and static pods are excluded."
		return c
	}
	if !c.Free.Fits(c.Demand) {
		c.Assessment = "HIGH"
		if unknown {
			c.Assessment = "UNKNOWN"
		}
		c.Detail = "Aggregate eligible CPU, memory or pod slots are insufficient for current requests."
		return c
	}
	for _, p := range moving {
		fits := false
		for _, name := range candidates[p.Namespace+"/"+p.Name].Nodes {
			if free[name].Fits(requests[p]) {
				fits = true
				break
			}
		}
		if !fits {
			c.Assessment = "HIGH"
			if unknown {
				c.Assessment = "UNKNOWN"
			}
			c.Detail = "At least one pod cannot fit on any checked eligible node: " + p.Namespace + "/" + p.Name
			return c
		}
	}
	// Try constrained/larger pods first. Failure of this heuristic is UNKNOWN,
	// not proof that a different packing cannot work. Each node is counted once.
	sort.SliceStable(moving, func(i, j int) bool {
		a, b := len(candidates[moving[i].Namespace+"/"+moving[i].Name].Nodes), len(candidates[moving[j].Namespace+"/"+moving[j].Name].Nodes)
		if a != b {
			return a < b
		}
		ra, rb := requests[moving[i]], requests[moving[j]]
		if ra.CPU != rb.CPU {
			return ra.CPU > rb.CPU
		}
		if ra.Memory != rb.Memory {
			return ra.Memory > rb.Memory
		}
		return moving[i].Namespace+"/"+moving[i].Name < moving[j].Namespace+"/"+moving[j].Name
	})
	reserved := relocationAttachments{}
	for _, p := range moving {
		placed := false
		r := requests[p]
		for _, name := range candidates[p.Namespace+"/"+p.Name].Nodes {
			if free[name].Fits(r) && volumes[p].reserve(in.Snapshot, name, in.scheduling, reserved) {
				free[name] = free[name].Sub(r)
				placed = true
				break
			}
		}
		if !placed {
			c.Assessment = "UNKNOWN"
			c.Detail = "Aggregate capacity fits, but this placement heuristic found no complete allocation. Fragmentation, overlapping eligibility or combined CSI attachment limits need review."
			return c
		}
	}
	if unknown {
		c.Assessment = "UNKNOWN"
		c.Detail = "CPU/memory/pod-slot placement fits, but some requests, scheduling constraints or collection evidence are incomplete."
		return c
	}
	c.Detail = "A request-based placement fits the snapshot. Actual utilization, ephemeral storage, backend attachment transitions, scheduler plugins, pending demand and concurrent changes are not simulated."
	return c
}

type CapacityAnalyzer struct{}

func (CapacityAnalyzer) Name() string { return "Capacity" }
func (CapacityAnalyzer) Analyze(_ context.Context, in Input) ([]model.Finding, error) {
	var c Capacity
	if in.capacity != nil {
		c = *in.capacity
	} else {
		c = CalculateCapacity(in)
	}
	level := model.Pass
	if c.Assessment != "PASS" {
		level = model.High
	}
	f := finding(level, "Capacity", "", in.Node, c.Detail, fmt.Sprintf("Relocate: %s; eligible remaining: %s; eligible nodes=%v", c.Demand, c.Free, c.Eligible), "Recheck immediately before drain; requests are not live usage and this is not an exact scheduler simulation.")
	f.Unknown = c.Assessment == "UNKNOWN"
	return []model.Finding{f}, nil
}
