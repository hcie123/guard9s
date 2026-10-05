package analyzer

import (
	"context"
	"testing"

	"github.com/hcie123/guard9s/internal/testfixture"
)

func BenchmarkPeerDense(b *testing.B) {
	for _, size := range testfixture.Sizes[1:] {
		b.Run(size.Name, func(b *testing.B) {
			s := testfixture.PeerDense(size.Nodes, size.Pods).Indexed()
			p := s.Pods[size.Nodes+1]
			target := p.Spec.NodeName
			ctx := context.Background()
			for _, operation := range []string{"Candidates", "ExistingAntiAffinity", "PodAffinity", "TopologySpread", "NamespaceSelector", "AnalyzeNode", "AnalyzeAllNodes"} {
				b.Run(operation, func(b *testing.B) {
					x := newSchedulingEvidence(s)
					q := p.DeepCopy()
					namespaceTerm := x.compile(s, p.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0], p.Namespace)
					if operation == "ExistingAntiAffinity" {
						q.Spec.Affinity = nil
					} else if operation == "PodAffinity" || operation == "NamespaceSelector" {
						q.Spec.Affinity.PodAntiAffinity = nil
						x.existing = nil
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						switch operation {
						case "Candidates":
							candidates(ctx, s, p, target, x)
						case "ExistingAntiAffinity", "PodAffinity":
							prepareAffinity(ctx, s, q, target, x)
						case "NamespaceSelector":
							match, unknown := namespaceTerm.matches(s.Pods[i%len(s.Pods)])
							if !match || unknown {
								b.Fatal("namespace selector evidence is not complete")
							}
						case "TopologySpread":
							prepareSpread(ctx, s, p, target, x)
						case "AnalyzeNode":
							Diagnose(ctx, Input{Snapshot: s, Node: target})
						case "AnalyzeAllNodes":
							AnalyzeAll(ctx, s)
						}
					}
				})
			}
		})
	}
}
