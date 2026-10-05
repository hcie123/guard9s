package report

import (
	"context"
	"io"
	"testing"

	"github.com/hcie123/guard9s/internal/testfixture"
)

func BenchmarkReport(b *testing.B) {
	for _, size := range testfixture.Sizes {
		b.Run(size.Name, func(b *testing.B) {
			s := testfixture.Snapshot(size.Nodes, size.Pods)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r, err := Build(context.Background(), s, s.Nodes[0].Name, "benchmark")
				if err != nil {
					b.Fatal(err)
				}
				if err = WriteJSON(io.Discard, r); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
