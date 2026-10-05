package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
)

func TestExportEvidenceAndSafety(t *testing.T) {
	for _, tc := range []struct {
		name, scenario, readiness string
		change                    func(*model.Snapshot)
	}{
		{"healthy", "healthy", "READY", func(*model.Snapshot) {}},
		{"blocked", "mixed", "BLOCKED", func(*model.Snapshot) {}},
		{"pending", "healthy", "NOT RECOMMENDED", func(s *model.Snapshot) { s.Pods[len(s.Pods)-1].Spec.NodeName = "" }},
		{"incomplete", "healthy", "NOT RECOMMENDED", func(s *model.Snapshot) { s.Issues = []string{"watch failed"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := demo.Snapshot(tc.scenario)
			s.Namespace = "unrelated-display-filter"
			s.KubeconfigPath = "/private/operator-config"
			s.Pods[0].Spec.Containers[0].Env = []core.EnvVar{{Name: "PRIVATE_VALUE", Value: "synthetic-env-canary"}}
			tc.change(&s)
			r, err := Build(context.Background(), s, "worker-01", "test-version")
			if err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			if err := WriteJSON(&b, r); err != nil {
				t.Fatal(err)
			}
			var got Report
			if err := json.Unmarshal(b.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.SchemaVersion != "guard9s/v1" || got.Version != "test-version" || !got.ReadOnly || got.AnalysisScope != "all namespaces" || !got.CapturedAt.Equal(s.At) || got.Readiness != tc.readiness || len(got.Findings) == 0 {
				t.Fatalf("incomplete report: %+v", got)
			}
			if !strings.Contains(b.String(), `"severity": "`) || !strings.Contains(b.String(), `"cpuMillicores"`) || !strings.Contains(b.String(), `"memoryBytes"`) {
				t.Fatal("export schema lost readable severities or explicit resource units")
			}
			if strings.Contains(got.Plan, "' drain '") != (tc.readiness == "READY") {
				t.Fatalf("commands disagree with readiness: %s", got.Plan)
			}
			if err := WriteMarkdown(&b, r); err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"synthetic-env-canary", "PRIVATE_VALUE", "/private/operator-config"} {
				if strings.Contains(b.String(), forbidden) {
					t.Fatalf("export included %s", forbidden)
				}
			}
			for _, required := range []string{"# guard9s maintenance report", "## Capacity", "## Findings", "## Maintenance plan", "## Limitations", "Evidence:"} {
				if !strings.Contains(b.String(), required) {
					t.Fatalf("markdown missing %s", required)
				}
			}
		})
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestExportFailuresAndMarkdownLiterals(t *testing.T) {
	s := demo.Snapshot("healthy")
	for _, node := range []string{"", "missing"} {
		if _, err := Build(context.Background(), s, node, "test"); err == nil {
			t.Fatal("missing target accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, s, "worker-01", "test"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	r, err := Build(context.Background(), s, "worker-01", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(brokenWriter{}, r); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	if err := WriteMarkdown(brokenWriter{}, r); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	r.Findings[0].Evidence = "event\n```\n<script>untrusted</script>\n````\nend"
	var b bytes.Buffer
	if err := WriteMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "`````text\n") || !strings.Contains(b.String(), r.Findings[0].Evidence) {
		t.Fatal("untrusted evidence escaped its literal block or was lost")
	}
}
