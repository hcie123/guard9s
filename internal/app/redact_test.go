package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hcie123/guard9s/internal/report"
)

func TestRedactionFlagsAndSchedulingDemo(t *testing.T) {
	for _, flag := range []string{"--redact", "--redact=basic", "--redact=none"} {
		cmd := NewCommand("test")
		cmd.SetArgs([]string{"--demo", "--demo-scenario", "scheduling", "--node", "worker-01", "--output", "json", flag})
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var r report.Report
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if len(r.Scheduling) == 0 || r.Readiness == "READY" {
			t.Fatal("scheduling demo evidence missing")
		}
		if flag != "--redact=none" && (r.Redaction != "basic" || strings.Contains(out.String(), "worker-01")) {
			t.Fatal("redaction flag did not mask names")
		}
		if flag == "--redact=none" && (r.Redaction != "none" || r.Node != "worker-01") {
			t.Fatal("none mode changed names")
		}
	}
	for _, args := range [][]string{{"--redact=invalid"}, {"--demo", "--redact"}} {
		cmd := NewCommand("test")
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--redact") {
			t.Fatal("invalid redaction accepted")
		}
	}
}
