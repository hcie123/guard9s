package ui

import (
	"context"
	"fmt"
	"testing"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	"github.com/hcie123/guard9s/internal/report"
	"github.com/hcie123/guard9s/internal/testfixture"
	"k8s.io/apimachinery/pkg/types"
)

// Both benchmarks use only a reproducible 12-node, 240-pod synthetic snapshot.
func benchmarkSnapshot() model.Snapshot {
	seed := demo.Snapshot("healthy")
	s := seed
	s.Nodes, s.Pods = nil, nil
	for i := 1; i <= 12; i++ {
		n := seed.Nodes[0].DeepCopy()
		n.Name = fmt.Sprintf("worker-%02d", i)
		n.UID = types.UID("benchmark-" + n.Name)
		n.Labels["kubernetes.io/hostname"] = n.Name
		s.Nodes = append(s.Nodes, n)
		for j := 1; j <= 20; j++ {
			p := seed.Pods[0].DeepCopy()
			p.Name = fmt.Sprintf("nginx-%02d-%03d", i, j)
			p.UID = types.UID("benchmark-" + p.Name)
			p.Spec.NodeName = n.Name
			s.Pods = append(s.Pods, p)
		}
	}
	return s
}

func BenchmarkUIBuildRows(b *testing.B) {
	for _, size := range testfixture.Sizes {
		b.Run(size.Name, func(b *testing.B) {
			u := New(testfixture.Snapshot(size.Nodes, size.Pods))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				u.buildRows()
			}
		})
	}
}

func BenchmarkCapacityRedraw(b *testing.B) {
	u := New(benchmarkSnapshot())
	u.command("capacity")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.render()
	}
}

func BenchmarkReportBuild(b *testing.B) {
	s := benchmarkSnapshot()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := report.Build(context.Background(), s, "worker-01", "benchmark"); err != nil {
			b.Fatal(err)
		}
	}
}
