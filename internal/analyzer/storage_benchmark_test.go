package analyzer

import (
	"context"
	"github.com/hcie123/guard9s/internal/testfixture"
	"testing"
)

func BenchmarkPathologicalUniqueSelectors(b *testing.B) {
	s := testfixture.PathologicalUniqueSelectors(100, 5000)
	for _, operation := range []string{"AnalyzeNode", "AnalyzeAllNodes"} {
		b.Run(operation, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if operation == "AnalyzeNode" {
					Diagnose(context.Background(), Input{Snapshot: s, Node: s.Nodes[0].Name})
				} else {
					AnalyzeAll(context.Background(), s)
				}
			}
		})
	}
}
func BenchmarkCSIHeavy(b *testing.B) {
	s := testfixture.CSIHeavy(50, 2000)
	for _, operation := range []string{"AnalyzeNode", "AnalyzeAllNodes"} {
		b.Run(operation, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if operation == "AnalyzeNode" {
					Diagnose(context.Background(), Input{Snapshot: s, Node: s.Nodes[0].Name})
				} else {
					AnalyzeAll(context.Background(), s)
				}
			}
		})
	}
}
