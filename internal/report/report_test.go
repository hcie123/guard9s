package report

import (
	"context"
	"strings"
	"testing"

	"github.com/hcie123/guard9s/internal/analyzer"
	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
)

func TestMaintenancePlans(t *testing.T) {
	for _, tc := range []struct {
		name, scenario, node string
		mutate               func(*model.Snapshot)
		writeCommands        bool
	}{
		{"blocked", "mixed", "worker-01", func(s *model.Snapshot) {}, false},
		{"healthy", "healthy", "worker-01", func(s *model.Snapshot) {}, true},
		{"preserve_cordon", "healthy", "worker-01", func(s *model.Snapshot) { s.Nodes[0].Spec.Unschedulable = true }, true},
		{"incomplete", "healthy", "worker-01", func(s *model.Snapshot) { s.Issues = []string{"watch failed"} }, false},
		{"missing_node", "healthy", "missing", func(s *model.Snapshot) {}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := demo.Snapshot(tc.scenario)
			tc.mutate(&s)
			f := analyzer.Run(context.Background(), analyzer.Input{Snapshot: s, Node: tc.node})
			plan := Plan(s, tc.node, f)
			if strings.Contains(plan, "' cordon '") != tc.writeCommands || strings.Contains(plan, "' drain '") != tc.writeCommands {
				t.Fatalf("unsafe or missing suggestions: %s", plan)
			}
			if tc.name == "preserve_cordon" && strings.Contains(plan, "' uncordon '") {
				t.Fatal("original scheduling state changed")
			}
			if !strings.Contains(plan, "never executed") || !strings.Contains(plan, "get pdb -A") {
				t.Fatal("missing safety or validation")
			}
			diagnosis := Diagnosis(tc.node, f)
			if !strings.Contains(diagnosis, "Maintenance readiness:") || !strings.Contains(diagnosis, "Evidence:") {
				t.Fatal("incomplete diagnosis")
			}
		})
	}
	s := demo.Snapshot("healthy")
	if plan := Plan(s, "worker-01", nil); strings.Contains(plan, "' drain '") {
		t.Fatal("empty diagnosis generated mutation suggestions")
	}
}
func TestShellQuoting(t *testing.T) {
	if quote("ctx'$(touch injected)") != "'ctx'\"'\"'$(touch injected)'" {
		t.Fatal("unsafe shell quote")
	}
}

func TestExplicitKubeconfigInSuggestions(t *testing.T) {
	s := demo.Snapshot("healthy")
	s.KubeconfigPath = "/tmp/demo config"
	f := analyzer.Run(context.Background(), analyzer.Input{Snapshot: s, Node: "worker-01"})
	plan := Plan(s, "worker-01", f)
	if !strings.Contains(plan, "kubectl --kubeconfig '/tmp/demo config' --context 'demo-cluster'") {
		t.Fatal("suggestions lost explicitly selected kubeconfig")
	}
}
