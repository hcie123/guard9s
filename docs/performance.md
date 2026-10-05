# v0.4 final performance verification

Measured on 2026-10-04 with Go 1.26.8, Linux amd64, AMD EPYC 9V74, `GOMAXPROCS=2`. Baseline: exact pre-selector-index commit `b0b2308192a3338e4a2d8f2fb7ecf15f02a9ed97`. Final implementation: `e4c63ce8f55b5e7c1e811698f3499beee2d9cd72`. Both runs used the committed fixtures and benchmark code, ran sequentially on the same machine, and passed. The final run started from a clean checkout. Later closeout documentation and the fixture comment do not change runtime code.

Each result below is **one iteration** (`-benchtime=1x`), not a median or a production latency bound. Small timing differences are not statistically established regressions. Allocated bytes are cumulative allocation volume, not peak RSS. No cluster or credentials were used.

## Results

Pathological all-node analysis improved from **9.545 s to 0.511 s** (about 18.7 times faster in these samples). Allocated bytes fell from **475,900,784 to 257,022,768** (45.99%). Ordinary Large measured **0.460 s**, versus 0.513 s before. Dense Large measured **1.071 s**, versus 1.061 s before; this single-run difference is about 1%, with no catastrophic regression observed.

| Benchmark | ns/op, before → final | B/op, before → final | allocs/op, before → final |
| --- | ---: | ---: | ---: |
| PeerDense/Medium/Candidates | 938,225 → 926,037 | 117,352 → 117,696 | 1,586 → 1,589 |
| PeerDense/Medium/ExistingAntiAffinity | 13,260 → 9,614 | 5,432 → 5,456 | 25 → 26 |
| PeerDense/Medium/PodAffinity | 569,933 → 596,343 | 24,528 → 24,528 | 235 → 235 |
| PeerDense/Medium/TopologySpread | 138,828 → 139,659 | 12,592 → 12,592 | 90 → 90 |
| PeerDense/Medium/NamespaceSelector | 421 → 521 | 0 → 0 | 0 → 0 |
| PeerDense/Medium/AnalyzeNode | 9,589,149 → 8,202,252 | 4,111,328 → 4,121,648 | 54,542 → 54,656 |
| PeerDense/Medium/AnalyzeAllNodes | 243,007,428 → 243,970,185 | 103,817,768 → 103,873,160 | 1,787,731 → 1,789,748 |
| PeerDense/Large/Candidates | 2,444,134 → 2,605,121 | 202,680 → 164,848 | 2,127 → 2,118 |
| PeerDense/Large/ExistingAntiAffinity | 20,331 → 20,991 | 5,432 → 5,456 | 25 → 26 |
| PeerDense/Large/PodAffinity | 2,478,160 → 2,025,062 | 46,336 → 46,336 | 437 → 437 |
| PeerDense/Large/TopologySpread | 538,947 → 586,037 | 22,760 → 22,760 | 139 → 139 |
| PeerDense/Large/NamespaceSelector | 721 → 1,292 | 0 → 0 | 0 → 0 |
| PeerDense/Large/AnalyzeNode | 21,851,759 → 19,846,787 | 8,186,624 → 8,196,968 | 109,018 → 109,139 |
| PeerDense/Large/AnalyzeAllNodes | 1,060,560,638 → 1,070,761,845 | 426,083,088 → 426,172,496 | 7,440,604 → 7,445,577 |
| PathologicalUniqueSelectors/AnalyzeNode | 170,141,554 → 63,360,592 | 26,296,400 → 25,387,760 | 244,815 → 234,699 |
| PathologicalUniqueSelectors/AnalyzeAllNodes | 9,544,708,837 → 510,570,595 | 475,900,784 → 257,022,768 | 6,041,299 → 3,994,844 |
| CSIHeavy/AnalyzeNode | 8,753,678 → 7,217,827 | 4,253,064 → 4,253,416 | 39,470 → 39,473 |
| CSIHeavy/AnalyzeAllNodes | 193,802,887 → 170,199,151 | 90,754,344 → 90,789,496 | 1,296,961 → 1,296,961 |
| AnalyzeAllNodes/Small | 5,387,543 → 6,331,956 | 2,877,064 → 2,881,136 | 25,917 → 26,169 |
| AnalyzeAllNodes/Medium | 113,971,094 → 107,375,740 | 50,425,224 → 50,486,944 | 844,727 → 847,109 |
| AnalyzeAllNodes/Large | 513,267,535 → 459,864,700 | 220,124,720 → 220,159,928 | 4,201,216 → 4,208,097 |

## Profile and bounded optimization

The first candidate-prefilter implementation still took **8,598,965,676 ns/op** for PathologicalUniqueSelectors/AnalyzeAllNodes. A CPU profile of that slow path identified `schedulingEvidence.domains` at **88.17% cumulative** and `domainAggregate.add` at **69.62% cumulative**. These overlapping cumulative percentages must not be summed. Repeated peer aggregation for distinct but equivalent negative selectors was the observed hotspot.

The final implementation records label-key presence among all assigned Pods in the current snapshot. A valid selector containing only `DoesNotExist` terms may share the empty-selector peer aggregate **only when none of those keys occurs on any assigned Pod**. Namespace and topology dimensions remain part of the aggregate identity. Terminal, terminating and missing-node assigned Pods are included in this proof. Invalid, nil, feature-gated, positive, mixed and observed-key selectors do not use this equivalence. The original selector remains available to full matching and required-affinity self-bootstrap logic.

The existing anti-affinity positive-label index is only a candidate prefilter. Retained groups still pass the full selector matcher; uncertain terms are not discarded. Regression tests compare rejected, unknown and checked reasons to an independent scan oracle for both PeerDense and PathologicalUniqueSelectors. The bounded prefilter fuzz test checks that matching or unknown groups cannot be excluded.

All caches belong to one snapshot/analysis lifecycle. There is no cross-snapshot or global cache. General unique negative selectors may still require repeated scans; this optimization does not claim general linear complexity. Candidate evidence/output still grows with Pods × Nodes.

## Measurement boundaries

- Ordinary fixtures: Small 12 Nodes/240 Pods, Medium 50/2,000, Large 100/5,000. AnalyzeAllNodes includes fresh snapshot indexing and shared scheduling evidence.
- PeerDense: Medium 50/2,000 and Large 100/5,000, with required affinity/anti-affinity, namespace selectors and hard topology spread. Component benchmarks reuse an indexed snapshot; they do not have the same timing boundary as a complete diagnosis.
- PathologicalUniqueSelectors: 100 Nodes/5,000 Pods, distinct absent-key negative affinity selectors and distinct unmatched positive existing anti-affinity groups. AnalyzeNode and AnalyzeAllNodes start fresh scheduling evidence from the fixture snapshot.
- CSIHeavy: 50 Nodes/2,000 Pods, API-evidence synthetic fixture; it does not exercise a real CSI backend.
- Fixture creation is outside the benchmark timer. API latency, informer memory, terminal rendering and real backend behavior are outside these measurements.

## Reproduce

Run baseline and final separately from the repository root, with the same toolchain and CPU settings:

```sh
GOMAXPROCS=2 go test ./internal/analyzer \
  -run '^$' \
  -bench='BenchmarkPathologicalUniqueSelectors|BenchmarkPeerDense|BenchmarkAnalyzeAllNodes|BenchmarkCSIHeavy' \
  -benchmem -benchtime=1x

GOMAXPROCS=2 go test ./internal/analyzer \
  -run '^$' -bench='^BenchmarkPathologicalUniqueSelectors/AnalyzeAllNodes$' \
  -benchmem -benchtime=1x \
  -cpuprofile=/tmp/guard9s-pathological.cpu \
  -memprofile=/tmp/guard9s-pathological.mem
go tool pprof -top /tmp/guard9s-pathological.cpu
```

CI retains synthetic benchmark smoke checks without any absolute timing gate. Historical performance records are available in Git history; these are the measured v0.4 closeout results.
