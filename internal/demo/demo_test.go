package demo

import (
	"context"
	"testing"

	"github.com/hcie123/guard9s/internal/analyzer"
	"github.com/hcie123/guard9s/internal/model"
)

func TestScenarios(t *testing.T) {
	for _, scenario := range []string{"mixed", "healthy"} {
		t.Run(scenario, func(t *testing.T) {
			s := Snapshot(scenario)
			if len(s.Nodes) != 3 || len(s.Pods) < 20 {
				t.Fatal("incomplete demo")
			}
			for _, n := range s.Nodes {
				if used, alloc := s.Used(n.Name), model.Allocatable(n); !alloc.Fits(used) {
					t.Fatalf("unrealistic demo allocation on %s: %s > %s", n.Name, used, alloc)
				}
			}
			f := analyzer.Run(context.Background(), analyzer.Input{Snapshot: s, Node: "worker-01"})
			want := "READY"
			if scenario == "mixed" {
				want = "BLOCKED"
			}
			if got := model.Readiness(f); got != want {
				t.Fatalf("readiness %s != %s", got, want)
			}
		})
	}
}
