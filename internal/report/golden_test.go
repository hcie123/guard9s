package report

import (
	"bytes"
	"context"
	"testing"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	"github.com/hcie123/guard9s/internal/testfixture"
)

func TestReportAndMaintenanceGoldens(t *testing.T) {
	s := demo.Snapshot("healthy")
	findings := []model.Finding{{Severity: model.Critical, Category: "PDB", Namespace: "demo", Resource: "nginx-1-01", Reason: "PDB prevents eviction.", Evidence: "budget=nginx-budget; disruptionsAllowed=0", Recommendation: "Restore budget allowance before maintenance."}, {Severity: model.High, Category: "Scheduling", Namespace: "demo", Resource: "nginx-1-01", Unknown: true, Reason: "Placement evidence is incomplete.", Evidence: "UNKNOWN: namespace labels unavailable", Recommendation: "Review placement constraints."}, {Severity: model.Warn, Category: "Storage", Namespace: "demo", Resource: "nginx-1-01", Reason: "Data requires validation.", Evidence: "volume=emptyDir", Recommendation: "Verify data is disposable."}}
	findings = append(findings, findings[0])
	testfixture.Golden(t, "maintenance-blocked.txt", []byte(Plan(s, "worker-01", findings)+"\n"))
	s.Pods = s.Pods[:1]
	r, err := Build(context.Background(), s, "worker-01", "golden", Options{Redact: "basic"})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := WriteJSON(&b, r); err != nil {
		t.Fatal(err)
	}
	testfixture.Golden(t, "report-redacted.json", b.Bytes())
}
