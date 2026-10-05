package report

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/hcie123/guard9s/internal/demo"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBasicRedactionPreservesEvidence(t *testing.T) {
	s := demo.Snapshot("mixed")
	s.Context = "synthetic-private-context"
	s.KubeconfigPath = "/synthetic/private-kubeconfig"
	s.Nodes[0].Status.Addresses = []core.NodeAddress{{Type: core.NodeInternalIP, Address: "192.0.2.10"}}
	s.Nodes[1].Labels[core.LabelTopologyZone] = "synthetic-zone-private"
	s.Events = append(s.Events, &core.Event{ObjectMeta: meta.ObjectMeta{Namespace: "demo", Name: "redaction-event"}, Type: core.EventTypeWarning, Reason: "FailedMount", Message: "worker-01 demo/nginx-1-01 nginx:stable /var/lib/demo-data https://service.example.invalid:8443 192.0.2.10 2001:db8::1 synthetic-zone-private", InvolvedObject: core.ObjectReference{Kind: "Pod", Namespace: "demo", Name: s.Pods[0].Name}, Count: 17, LastTimestamp: meta.NewTime(s.At)})
	plain, err := Build(context.Background(), s, "worker-01", "test")
	if err != nil {
		t.Fatal(err)
	}
	redacted, err := Build(context.Background(), s, "worker-01", "test", Options{Redact: "basic"})
	if err != nil {
		t.Fatal(err)
	}
	var jsonText, markdown bytes.Buffer
	if err := WriteJSON(&jsonText, redacted); err != nil {
		t.Fatal(err)
	}
	if err := WriteMarkdown(&markdown, redacted); err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{s.Context, "worker-01", "payment-api", "redis-data", "redis-local-pv", "production", "nginx:stable", "/var/lib/demo-data", "service.example.invalid", "192.0.2.10", "2001:db8::1", "synthetic-zone-private", s.KubeconfigPath} {
		if strings.Contains(jsonText.String()+markdown.String(), canary) {
			t.Fatalf("redaction leaked %s", canary)
		}
	}
	if redacted.Redaction != "basic" || redacted.Context != "context-001" || redacted.Node != "node-001" || redacted.Readiness != plain.Readiness || !reflect.DeepEqual(redacted.Capacity.Demand, plain.Capacity.Demand) || !reflect.DeepEqual(redacted.Capacity.EligibleFree, plain.Capacity.EligibleFree) {
		t.Fatal("redaction altered safety or quantities")
	}
	for i, f := range redacted.Findings {
		old := plain.Findings[i]
		if f.Severity != old.Severity || f.Unknown != old.Unknown || f.Category != old.Category {
			t.Fatal("redaction changed severity/type/UNKNOWN")
		}
	}
	for i, p := range redacted.Scheduling {
		for j, n := range p.Nodes {
			if n.Status != plain.Scheduling[i].Nodes[j].Status {
				t.Fatal("redaction changed candidate status")
			}
		}
	}
	if !strings.Contains(jsonText.String(), "count=17") || !strings.Contains(redacted.Plan, "BASIC REDACTION") || !strings.Contains(redacted.Limitations[3], "Arbitrary business prose") {
		t.Fatal("evidence or limitations lost")
	}
	if !strings.Contains(redacted.Plan, "--field-selector 'spec.nodeName=<redacted-node>'") || !strings.Contains(jsonText.String(), "status.conditions") {
		t.Fatal("redaction changed Kubernetes field paths")
	}
	again, err := Build(context.Background(), s, "worker-01", "test", Options{Redact: "basic"})
	if err != nil || !reflect.DeepEqual(again, redacted) {
		t.Fatal("unstable per-report aliases")
	}
	if _, err := Build(context.Background(), s, "worker-01", "test", Options{Redact: "invalid"}); err == nil {
		t.Fatal("invalid mode accepted")
	}
}
func TestRedactionBoundariesAndNetworkTokens(t *testing.T) {
	r := newRedactor(demo.Snapshot("healthy"))
	text := r.text("nginx nginx-more demo/nginx-1-01 worker-01 worker-011 192.0.2.1 192.0.2.1 999.999.999.999 2001:db8::2 https://service.example.invalid kubernetes.io topology.kubernetes.io")
	if strings.Contains(text, "demo/nginx-1-01") || strings.Contains(text, "worker-01 ") || !strings.Contains(text, "worker-011") || !strings.Contains(text, "nginx-more") || !strings.Contains(text, "999.999.999.999") || !strings.Contains(text, "topology.kubernetes.io") {
		t.Fatal(text)
	}
	if r.id("pod", "demo", "missing") != "pod-redacted" || r.id("namespace", "", "") != "" {
		t.Fatal("fallback alias")
	}
}
func TestRedactionPreservesFieldNamesAndCounts(t *testing.T) {
	s := demo.Snapshot("healthy")
	s.Nodes[0].Labels["synthetic-count"] = "1000"
	r := newRedactor(s)
	fields := "status.conditions status.phase spec.unschedulable spec.nodeName count=1000 CPU=1000m"
	if got := r.text(fields); got != fields {
		t.Fatalf("field names or quantities changed: %s", got)
	}
	for _, address := range []string{"notkubernetes.io", "service.example.invalid", "https://service.example.invalid"} {
		if r.text(address) == address {
			t.Fatalf("domain was incorrectly exempted: %s", address)
		}
	}
}
func FuzzRedactionAndSerialization(f *testing.F) {
	f.Add("worker-01 demo/nginx-1-01 https://service.example.invalid")
	f.Add("```\n192.0.2.1\x00")
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 2048 {
			return
		}
		r := newRedactor(demo.Snapshot("healthy"))
		masked := r.text(text)
		var b bytes.Buffer
		value := Report{Findings: []ExportFinding{{Severity: "HIGH", Category: "Events", Evidence: masked}}}
		if err := WriteJSON(&b, value); err != nil {
			t.Fatal(err)
		}
		var parsed Report
		if err := json.Unmarshal(b.Bytes(), &parsed); err != nil || parsed.Findings[0].Severity != "HIGH" {
			t.Fatal("serialization lost severity")
		}
		b.Reset()
		if err := WriteMarkdown(&b, value); err != nil {
			t.Fatal(err)
		}
	})
}
