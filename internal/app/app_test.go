package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hcie123/guard9s/internal/report"
)

func TestCLIValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"invalid_scenario", []string{"--demo", "--demo-scenario", "invalid"}, "demo-scenario"},
		{"invalid_timeout", []string{"--demo", "--timeout", "0s"}, "timeout"},
		{"unexpected_argument", []string{"oops"}, "unknown command"},
		{"missing_config", []string{"--kubeconfig", filepath.Join(t.TempDir(), "missing")}, "kubeconfig"},
		{"invalid_output_before_config", []string{"--output", "yaml"}, "--output"},
		{"report_requires_node_before_config", []string{"--output", "json"}, "--node"},
		{"tui_rejects_report_node", []string{"--demo", "--node", "worker-01"}, "--node"},
		{"report_missing_target", []string{"--demo", "--output", "json", "--node", "absent"}, "existing node"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewCommand("test")
			cmd.SetArgs(tc.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.ExecuteContext(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestOfflineCLIExportWithoutTerminal(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
	t.Setenv("TERM", "")
	for _, output := range []string{"json", "markdown"} {
		t.Run(output, func(t *testing.T) {
			cmd := NewCommand("export-test")
			cmd.SetArgs([]string{"--demo", "--demo-scenario", "healthy", "--namespace", "nonexistent", "--node", "worker-01", "--output", output})
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected diagnostics: %s", stderr.String())
			}
			if output == "json" {
				var r report.Report
				if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
					t.Fatal(err)
				}
				if r.Readiness != "READY" || r.AnalysisScope != "all namespaces" || r.Version != "export-test" {
					t.Fatalf("%+v", r)
				}
			} else if !strings.HasPrefix(stdout.String(), "# guard9s maintenance report") {
				t.Fatal("markdown report missing")
			}
		})
	}
	cmd := NewCommand("test")
	cmd.SetArgs([]string{"--demo", "--node", "worker-01", "--output", "json"})
	cmd.SetOut(failingWriter{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("export write error discarded")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestDemoNoKubeconfig(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
	s, err := (demoSource{scenario: "mixed", namespace: "production"}).Snapshot(context.Background())
	if err != nil || len(s.Nodes) != 3 || s.Namespace != "production" {
		t.Fatal(err)
	}
}
func TestVersion(t *testing.T) {
	cmd := NewCommand("v0.1.0")
	cmd.SetArgs([]string{"--version"})
	cmd.SetOut(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}
