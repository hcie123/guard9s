package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hcie123/guard9s/internal/analyzer"
	"github.com/hcie123/guard9s/internal/model"
	"github.com/hcie123/guard9s/internal/report"
	core "k8s.io/api/core/v1"
)

func maximum(f []model.Finding) model.Severity {
	s := model.Pass
	for _, x := range f {
		if x.Severity > s {
			s = x.Severity
		}
	}
	return s
}
func (u *UI) visible(ns string) bool {
	return ns == "" || u.snapshot.Namespace == "" || u.snapshot.Namespace == "all" || u.snapshot.Namespace == ns
}
func percent(used, total int64) string {
	if total == 0 {
		return "UNKNOWN"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(used)/float64(total))
}
func age(at, now time.Time) string {
	if at.IsZero() {
		return "unknown"
	}
	d := now.Sub(at)
	if d < 0 {
		d = 0
	}
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return d.Round(time.Minute).String()
}

func (u *UI) buildRows() ([]string, []row) {
	s := u.snapshot
	var rows []row
	switch u.view {
	case "nodes":
		for _, n := range s.Nodes {
			role := "worker"
			for label := range n.Labels {
				if strings.HasPrefix(label, "node-role.kubernetes.io/") {
					role = strings.TrimPrefix(label, "node-role.kubernetes.io/")
					if role == "control-plane" {
						break
					}
				}
			}
			status := "NotReady"
			if model.Condition(n, core.NodeReady) == core.ConditionTrue {
				status = "Ready"
			}
			if n.Spec.Unschedulable {
				status += "/cordoned"
			}
			used, alloc := s.Used(n.Name), model.Allocatable(n)
			sev := maximum(u.findings[n.Name])
			detail := fmt.Sprintf("Node: %s\nMaintenance: %s\n\nReady: %s\nMemoryPressure: %s\nDiskPressure: %s\nPIDPressure: %s\nNetworkUnavailable: %s\nUnschedulable: %t\n\nKubernetes: %s\nKernel: %s\nRuntime: %s\nOS: %s\nArchitecture: %s\nLabels: %d\nTaints: %d\nActive pods: %d\n\nRequested: %s\nAllocatable: %s\n\nEnter: Pods\nd: Diagnosis\np: Maintenance plan", n.Name, model.Readiness(u.findings[n.Name]), model.Condition(n, core.NodeReady), model.Condition(n, core.NodeMemoryPressure), model.Condition(n, core.NodeDiskPressure), model.Condition(n, core.NodePIDPressure), model.Condition(n, core.NodeNetworkUnavailable), n.Spec.Unschedulable, n.Status.NodeInfo.KubeletVersion, n.Status.NodeInfo.KernelVersion, n.Status.NodeInfo.ContainerRuntimeVersion, n.Status.NodeInfo.OSImage, n.Status.NodeInfo.Architecture, len(n.Labels), len(n.Spec.Taints), len(s.NodePods(n.Name)), used, alloc)
			rows = append(rows, row{name: n.Name, state: status, category: "Node", cpu: used.CPU, memory: used.Memory, podCount: used.Pods, key: n.Name + "/" + string(n.UID), cells: []string{n.Name, status, role, n.Status.NodeInfo.KubeletVersion, percent(used.CPU, alloc.CPU), percent(used.Memory, alloc.Memory), fmt.Sprint(used.Pods), age(n.CreationTimestamp.Time, s.At), sev.String()}, detail: detail, node: n.Name, severity: sev})
		}
		if u.compact {
			for i := range rows {
				c := rows[i].cells
				rows[i].cells = []string{c[0], c[1], c[8], c[4], c[5], c[6]}
			}
			return []string{"NAME", "STATUS", "RISK", "CPU REQ", "MEM REQ", "PODS"}, rows
		}
		return []string{"NAME", "STATUS", "ROLE", "VERSION", "CPU REQ", "MEM REQ", "PODS", "AGE", "RISK"}, rows
	case "pods":
		for _, p := range s.Pods {
			if p.Spec.NodeName != u.node || !u.visible(p.Namespace) {
				continue
			}
			o := s.Owner(p)
			r := model.Requests(p)
			ready, restarts := 0, int32(0)
			status := string(p.Status.Phase)
			for _, c := range p.Status.ContainerStatuses {
				if c.Ready {
					ready++
				}
				restarts += c.RestartCount
				if c.State.Waiting != nil {
					status = c.State.Waiting.Reason
				}
			}
			if p.DeletionTimestamp != nil {
				status = "Terminating"
			}
			var findings []model.Finding
			for _, f := range u.findings[u.node] {
				if f.Namespace == p.Namespace && f.Resource == p.Name {
					findings = append(findings, f)
				}
			}
			sev := maximum(findings)
			storage := "none"
			var volumes []string
			for _, v := range p.Spec.Volumes {
				switch {
				case v.EmptyDir != nil:
					volumes = append(volumes, "emptyDir")
				case v.HostPath != nil:
					volumes = append(volumes, "hostPath")
				case v.PersistentVolumeClaim != nil:
					volumes = append(volumes, "PVC:"+v.PersistentVolumeClaim.ClaimName)
				case v.Ephemeral != nil:
					volumes = append(volumes, "ephemeral")
				}
			}
			if len(volumes) > 0 {
				storage = strings.Join(volumes, ",")
			}
			budgets, err := model.MatchingPDBs(s, p)
			pdb := fmt.Sprint(len(budgets))
			if err != nil {
				pdb = "UNKNOWN"
			}
			detail := fmt.Sprintf("Pod: %s/%s\nNode: %s\nOwner: %s/%s (resolved=%t)\nStatus: %s\nRequests: %s\nStorage: %s\n\n", p.Namespace, p.Name, p.Spec.NodeName, o.Kind, o.Name, o.Resolved, status, r, storage)
			for _, f := range findings {
				detail += report.Finding(f) + "\n\n"
			}
			rows = append(rows, row{name: p.Name, namespace: p.Namespace, state: status, category: "Pod", node: p.Spec.NodeName, cpu: r.CPU, memory: r.Memory, restarts: int64(restarts), key: p.Namespace + "/" + p.Name + "/" + string(p.UID), cells: []string{p.Namespace, p.Name, o.Kind, fmt.Sprintf("%d/%d", ready, len(p.Spec.Containers)), status, fmt.Sprint(restarts), fmt.Sprintf("%dm", r.CPU), fmt.Sprintf("%dMi", r.Memory/(1<<20)), storage, pdb, sev.String()}, detail: detail, severity: sev})
		}
		if u.compact {
			for i := range rows {
				c := rows[i].cells
				rows[i].cells = []string{c[0], c[1], c[4], c[10]}
			}
			return []string{"NAMESPACE", "NAME", "STATUS", "RISK"}, rows
		}
		return []string{"NAMESPACE", "NAME", "OWNER", "READY", "STATUS", "RESTARTS", "CPU REQ", "MEM REQ", "STORAGE", "PDB", "RISK"}, rows
	case "risks", "diagnosis", "storage":
		for _, n := range s.Nodes {
			if u.view != "risks" && n.Name != u.node {
				continue
			}
			for _, f := range u.findings[n.Name] {
				if u.view == "risks" && (f.Severity < model.Warn && !f.Unknown) {
					continue
				}
				if u.view == "storage" && f.Category != "Storage" {
					continue
				}
				if u.view != "diagnosis" && !u.visible(f.Namespace) {
					continue
				}
				state := f.Severity.String()
				if f.Unknown {
					state += "/UNKNOWN"
				}
				rows = append(rows, row{name: f.Resource, namespace: f.Namespace, state: state, category: f.Category, key: strings.Join([]string{n.Name, f.Category, f.Namespace, f.Resource, f.Reason}, "\x00"), cells: []string{state, f.Namespace, f.Resource, f.Category, f.Reason}, detail: report.Finding(f) + "\n\nTarget node: " + n.Name + "\nMaintenance: " + model.Readiness(u.findings[n.Name]), node: n.Name, severity: f.Severity})
			}
		}
		if u.view == "risks" {
			// Pending pods have no node, so cannot occur in a node diagnosis.
			for _, p := range s.Pods {
				if p.Spec.NodeName == "" && model.Active(p) && u.visible(p.Namespace) {
					rows = append(rows, row{name: p.Name, namespace: p.Namespace, state: string(p.Status.Phase), category: "Scheduling", key: "unscheduled/" + p.Namespace + "/" + p.Name + "/" + string(p.UID), cells: []string{"HIGH", p.Namespace, p.Name, "Scheduling", "Unscheduled pod"}, detail: "Resource: " + p.Namespace + "/" + p.Name + "\nEvidence: spec.nodeName is empty; phase=" + string(p.Status.Phase) + "\nRecommendation: review FailedScheduling events and pending cluster demand.", severity: model.High})
				}
			}
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].severity > rows[j].severity })
		return []string{"SEVERITY", "NAMESPACE", "RESOURCE", "TYPE", "REASON"}, rows
	case "events":
		for _, e := range model.RelevantEvents(s, "") {
			if !u.visible(e.Namespace) {
				continue
			}
			detail := fmt.Sprintf("%s/%s\nType: %s\nReason: %s\nLast seen: %s\nCount: %d\nObject: %s/%s/%s\nMessage: %s", e.Namespace, e.Name, e.Type, e.Reason, model.EventTime(e).Format(time.RFC3339), e.Count, e.InvolvedObject.Kind, e.InvolvedObject.Namespace, e.InvolvedObject.Name, e.Message)
			rows = append(rows, row{name: e.Name, namespace: e.Namespace, state: e.Type, category: e.Reason, key: e.Namespace + "/" + e.Name + "/" + string(e.UID), cells: []string{age(model.EventTime(e), s.At), e.Type, e.Reason, e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name, e.Message}, detail: detail, severity: model.Warn})
		}
		return []string{"LAST SEEN", "TYPE", "REASON", "OBJECT", "MESSAGE"}, rows
	case "capacity":
		for _, n := range s.Nodes {
			c, ok := u.capacity[n.Name]
			if !ok {
				c = analyzer.Capacity{Assessment: "UNKNOWN", Detail: "No capacity diagnosis is available for this snapshot. Refresh and retry."}
			}
			sev := model.Pass
			if c.Assessment != "PASS" {
				sev = model.High
			}
			detail := fmt.Sprintf("Drain capacity estimate: %s\n\nPods to relocate: %d\nCPU requested: %.2f cores\nMemory requested: %.2f GiB\n\nEligible remaining: %s\nEligible nodes: %v\nAssessment: %s\n\n%s\n\n%s", n.Name, c.Demand.Pods, float64(c.Demand.CPU)/1000, float64(c.Demand.Memory)/(1<<30), c.Free, c.Eligible, c.Assessment, c.Detail, analyzer.SchedulingDisclaimer)
			rows = append(rows, row{name: n.Name, state: c.Assessment, category: "Capacity", key: n.Name + "/" + string(n.UID), cells: []string{n.Name, fmt.Sprint(c.Demand.Pods), fmt.Sprintf("%.2f", float64(c.Demand.CPU)/1000), fmt.Sprintf("%.2fGi", float64(c.Demand.Memory)/(1<<30)), fmt.Sprintf("%.2f", float64(c.Free.CPU)/1000), fmt.Sprintf("%.2fGi", float64(c.Free.Memory)/(1<<30)), fmt.Sprint(c.Free.Pods), c.Assessment}, node: n.Name, detail: detail, severity: sev})
		}
		return []string{"NODE", "RELOCATE", "CPU REQ", "MEM REQ", "FREE CPU", "FREE MEM", "SLOTS", "ASSESSMENT"}, rows
	case "pdb":
		seen := map[string]bool{}
		for _, p := range s.NodePods(u.node) {
			if !model.Movable(s, p) {
				continue
			}
			budgets, _ := model.MatchingPDBs(s, p)
			for _, b := range budgets {
				key := b.Namespace + "/" + b.Name
				if seen[key] || !u.visible(b.Namespace) {
					continue
				}
				seen[key] = true
				sev, stale := analyzer.PDBAssessment(b)
				state := sev.String()
				if stale {
					state += "/UNKNOWN"
				}
				min, max := "-", "-"
				if b.Spec.MinAvailable != nil {
					min = b.Spec.MinAvailable.String()
				}
				if b.Spec.MaxUnavailable != nil {
					max = b.Spec.MaxUnavailable.String()
				}
				rows = append(rows, row{name: b.Name, namespace: b.Namespace, state: state, category: "PDB", key: key + "/" + string(b.UID), cells: []string{b.Namespace, b.Name, min, max, fmt.Sprint(b.Status.CurrentHealthy), fmt.Sprint(b.Status.DesiredHealthy), fmt.Sprint(b.Status.ExpectedPods), fmt.Sprint(b.Status.DisruptionsAllowed), state}, detail: "Assessment: " + state + "\n" + analyzer.PDBEvidence(b), severity: sev})
			}
		}
		return []string{"NAMESPACE", "PDB", "MIN", "MAX", "HEALTHY", "DESIRED", "EXPECTED", "ALLOWED", "RISK"}, rows
	case "plan":
		f := report.PlanFindings(s, u.node, u.findings[u.node])
		text := report.Plan(s, u.node, u.findings[u.node])
		return []string{"TARGET", "READINESS", "ACTION"}, []row{{name: u.node, node: u.node, state: model.Readiness(f), category: "Plan", key: u.node, cells: []string{u.node, model.Readiness(f), "Enter: full maintenance plan"}, detail: text, severity: maximum(f)}}
	}
	return []string{"RESOURCE"}, rows
}
