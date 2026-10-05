package report

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func redactionCorpus() model.Snapshot {
	s := demo.Snapshot("healthy")
	s.Namespaces = append(s.Namespaces, &core.Namespace{ObjectMeta: meta.ObjectMeta{Name: "synthetic-shared-node-namespace"}})
	n := s.Nodes[0].DeepCopy()
	n.Name = "synthetic-shared-node-namespace"
	s.Nodes = append(s.Nodes, n)
	p := s.Pods[0].DeepCopy()
	p.Name = "synthetic-shared-pod-deployment"
	s.Pods = append(s.Pods, p)
	s.Deployments = append(s.Deployments, &apps.Deployment{ObjectMeta: meta.ObjectMeta{Name: p.Name, Namespace: p.Namespace}})
	s.PVCs = append(s.PVCs, &core.PersistentVolumeClaim{ObjectMeta: meta.ObjectMeta{Name: p.Name, Namespace: p.Namespace}})
	return s
}

func TestRedactionV2TypeCollisionsAndDeterminism(t *testing.T) {
	s := redactionCorpus()
	r := newRedactor(s)
	for _, tc := range []struct {
		name, ns string
		kinds    []string
	}{
		{"synthetic-shared-pod-deployment", "demo", []string{"pod", "deployment", "pvc"}},
		{"synthetic-shared-node-namespace", "", []string{"node", "namespace"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[string]bool{}
			for _, kind := range tc.kinds {
				alias := r.id(kind, tc.ns, tc.name)
				if !strings.HasPrefix(alias, kind+"-") || seen[alias] {
					t.Fatal("kind collision", kind, alias)
				}
				seen[alias] = true
			}
		})
	}
	typed := r.text("Pod/demo/synthetic-shared-pod-deployment Deployment/demo/synthetic-shared-pod-deployment")
	if !strings.Contains(typed, "Pod/namespace-") || !strings.Contains(typed, "/pod-") || !strings.Contains(typed, "Deployment/namespace-") || !strings.Contains(typed, "/deployment-") {
		t.Fatal("typed references lost identity", typed)
	}
	report := Report{Context: s.Context, Node: s.Nodes[0].Name, Findings: []ExportFinding{
		{Namespace: "demo", Resource: "synthetic-shared-pod-deployment", ResourceKind: "Pod"},
		{Namespace: "demo", Resource: "synthetic-shared-pod-deployment", ResourceKind: "Deployment"},
		{Namespace: "demo", Resource: "synthetic-shared-pod-deployment", ResourceKind: "PersistentVolumeClaim"},
	}}
	masked := basicRedaction(report, s)
	if !strings.HasPrefix(masked.Findings[0].Resource, "pod-") || !strings.HasPrefix(masked.Findings[1].Resource, "deployment-") || !strings.HasPrefix(masked.Findings[2].Resource, "pvc-") {
		t.Fatal("structured resource kinds conflated")
	}
	first := r.text("demo/synthetic-shared-pod-deployment synthetic-shared-node-namespace")
	for range 20 {
		if got := newRedactor(s).text("demo/synthetic-shared-pod-deployment synthetic-shared-node-namespace"); got != first {
			t.Fatalf("map iteration changed aliases: %s vs %s", first, got)
		}
	}
	slices.Reverse(s.Nodes)
	slices.Reverse(s.Pods)
	slices.Reverse(s.Deployments)
	slices.Reverse(s.Namespaces)
	if !reflect.DeepEqual(r.ids, newRedactor(s).ids) {
		t.Fatal("inventory order changed typed identities")
	}
}

func TestNodeAndCSINodeReferencesRetainNodeAlias(t *testing.T) {
	s := demo.Snapshot("healthy")
	r := newRedactor(s)
	n := s.Nodes[0].Name
	if got := r.text(n); got != r.id("node", "", n) {
		t.Fatal("CSI metadata changed the physical node alias", got)
	}
	if got := r.text("CSINode/" + n); got != "CSINode/"+r.id("csinode", "", n) {
		t.Fatal("typed CSINode identity lost", got)
	}
}

func TestOrphanedVolumeAttachmentReferencesAreRedacted(t *testing.T) {
	s := demo.Snapshot("healthy")
	pv := "synthetic-orphan-private-pv"
	s.VolumeAttachments = []*storage.VolumeAttachment{{
		ObjectMeta: meta.ObjectMeta{Name: "synthetic-private-attachment"},
		Spec: storage.VolumeAttachmentSpec{
			NodeName: "synthetic-orphan-private-node",
			Attacher: "synthetic.private.csi",
			Source:   storage.VolumeAttachmentSource{PersistentVolumeName: &pv},
		},
	}}
	r := newRedactor(s)
	plain := "VolumeAttachment/synthetic-private-attachment synthetic-orphan-private-node synthetic-orphan-private-pv synthetic.private.csi"
	masked := r.text(plain)
	for _, secret := range []string{"synthetic-private-attachment", "synthetic-orphan-private-node", "synthetic-orphan-private-pv", "synthetic.private.csi"} {
		if strings.Contains(masked, secret) {
			t.Fatalf("orphaned attachment reference leaked: %s in %s", secret, masked)
		}
	}
	if !strings.Contains(masked, "node-") || !strings.Contains(masked, "pv-") || !strings.Contains(masked, "csidriver-") {
		t.Fatalf("typed attachment aliases missing: %s", masked)
	}
}

func TestRedactedMaintenanceCommandsAreDocumentationOnly(t *testing.T) {
	for _, scenario := range []string{"healthy", "mixed"} {
		t.Run(scenario, func(t *testing.T) {
			s := demo.Snapshot(scenario)
			r, err := Build(context.Background(), s, "worker-01", "test", Options{Redact: "basic"})
			if err != nil {
				t.Fatal(err)
			}
			if !r.Redacted || !r.CommandsRedacted || r.RedactionMode != "basic" || !strings.Contains(r.Plan, "Redacted commands are documentation-only and must not be executed verbatim.") {
				t.Fatal("redacted command metadata or notice lost")
			}
			for _, line := range strings.Split(r.Plan, "\n") {
				if strings.HasPrefix(line, "kubectl ") {
					if strings.Contains(line, "worker-") || strings.Contains(line, s.Context) || !strings.Contains(line, "<redacted-context>") {
						t.Fatal("real command identifier leaked", line)
					}
				}
			}
			if !strings.Contains(r.Plan, "spec.nodeName=<redacted-node>") {
				t.Fatal("field selector or placeholder lost")
			}
			if scenario == "mixed" && strings.Contains(r.Plan, "' drain '") {
				t.Fatal("blocked redacted plan gained drain")
			}
			again, err := Build(context.Background(), s, "worker-01", "test", Options{Redact: "basic"})
			if err != nil || !reflect.DeepEqual(r, again) {
				t.Fatal("repeated export changed mappings")
			}
		})
	}
}

func TestJSONV1RemainsReadableByLegacyConsumer(t *testing.T) {
	s := demo.Snapshot("scheduling")
	r, err := Build(context.Background(), s, "worker-01", "test", Options{Redact: "basic"})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := WriteJSON(&b, r); err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		SchemaVersion, Redaction, Readiness string
		ReadOnly                            bool
		Findings                            []struct {
			Severity, Category, Resource, Reason, Evidence string
			Unknown                                        bool
		}
		Capacity ExportCapacity
		Plan     string
	}
	if err := json.Unmarshal(b.Bytes(), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.SchemaVersion != "guard9s/v1" || !legacy.ReadOnly || legacy.Redaction != "basic" || len(legacy.Findings) != len(r.Findings) || legacy.Plan != r.Plan || !reflect.DeepEqual(legacy.Capacity, r.Capacity) {
		t.Fatal("JSON v1 contract changed")
	}
	var old Report
	if err := json.Unmarshal([]byte(`{"schemaVersion":"guard9s/v1","readOnly":true,"redaction":"none","findings":[],"schedulingCandidates":[],"plan":"legacy"}`), &old); err != nil || old.SchemaVersion != "guard9s/v1" || old.Redacted || old.CommandsRedacted {
		t.Fatal("legacy JSON cannot be read", err)
	}
}
