package analyzer_test

import (
	"context"
	"testing"

	"github.com/hcie123/guard9s/internal/analyzer"
	"github.com/hcie123/guard9s/internal/model"
	"github.com/hcie123/guard9s/internal/testfixture"
)

func benchmark(b *testing.B, run func(model.Snapshot)) {
	for _, size := range testfixture.Sizes {
		b.Run(size.Name, func(b *testing.B) {
			s := testfixture.Snapshot(size.Nodes, size.Pods)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				run(s)
			}
		})
	}
}
func BenchmarkAnalyzeNode(b *testing.B) {
	benchmark(b, func(s model.Snapshot) {
		analyzer.Diagnose(context.Background(), analyzer.Input{Snapshot: s, Node: s.Nodes[0].Name})
	})
}
func BenchmarkAnalyzeAllNodes(b *testing.B) {
	benchmark(b, func(s model.Snapshot) {
		analyzer.AnalyzeAll(context.Background(), s)
	})
}
func BenchmarkCapacity(b *testing.B) {
	benchmark(b, func(s model.Snapshot) { analyzer.CalculateCapacity(analyzer.Input{Snapshot: s, Node: s.Nodes[0].Name}) })
}
func BenchmarkSchedulingCandidates(b *testing.B) {
	benchmark(b, func(s model.Snapshot) {
		analyzer.Candidates(s, s.Pods[len(s.Nodes)], s.Pods[len(s.Nodes)].Spec.NodeName)
	})
}
