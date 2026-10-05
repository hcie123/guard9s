package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
)

func TestStreamingJSONMatchesV1Encoder(t *testing.T) {
	for _, scenario := range []string{"mixed", "healthy", "scheduling"} {
		for _, mode := range []string{"none", "basic"} {
			t.Run(scenario+"/"+mode, func(t *testing.T) {
				r, err := Build(context.Background(), demo.Snapshot(scenario), "worker-01", "v0.4", Options{Redact: mode})
				if err != nil {
					t.Fatal(err)
				}
				var want, got bytes.Buffer
				enc := json.NewEncoder(&want)
				enc.SetIndent("", "  ")
				if err = enc.Encode(r); err != nil {
					t.Fatal(err)
				}
				if err = WriteJSON(&got, r); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(want.Bytes(), got.Bytes()) {
					t.Fatal("formatted v1 JSON changed")
				}
			})
		}
	}
	for _, r := range []Report{{}, {Capabilities: map[string]model.Capability{}}, {Scheduling: []SchedulingEvidence{}, Findings: []ExportFinding{}}} {
		var a, b bytes.Buffer
		enc := json.NewEncoder(&a)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r)
		if err := WriteJSON(&b, r); err != nil || a.String() != b.String() {
			t.Fatal("nil/empty contract changed", err)
		}
	}
}

type failWriter struct{ remaining int }

func (w *failWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, errors.New("fixture write failure")
	}
	w.remaining -= len(p)
	return len(p), nil
}
func TestStreamingJSONWriterFailure(t *testing.T) {
	r, err := Build(context.Background(), demo.Snapshot("scheduling"), "worker-01", "test")
	if err != nil {
		t.Fatal(err)
	}
	if WriteJSON(&failWriter{remaining: 0}, r) == nil {
		t.Fatal("flush error lost")
	}
	fw := fragmentWriter{shortWriter{}}
	if _, err = fw.Write([]byte("{}\n")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }
