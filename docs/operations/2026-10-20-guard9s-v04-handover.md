# guard9s v0.4 reliability audit handover — 2026-10-20

## Scope and starting state

- Repository: `hcie123/guard9s` (standalone checkout `/root/guard9s-test/guard9s`). The historical `/root/infra-lab/guard9s` directory was not used or changed.
- Local checkout started at `main` `d3d18ebc47e2e006f70b2a677564e0a5cdd9210b`, worktree clean; tag `v0.4.0` points to `e0c2f54208085a8ef72d11c59873923ee2404b74`. Initial shell DNS prevented `git fetch`; subsequent GitHub API inspection established remote `main` is `a06dee8dc1bc6f6b9983f2286151a1dd6b04ca73`. The local starting commit is an ancestor of that remote main; PR #6 targets the actual remote `main` and GitHub reports it mergeable with only this handover file changed.
- The repository has already published v0.4.0. This differs from the requested `0.4.0-dev`; no version, release tag, runtime behavior, Kubernetes resource, or CI configuration was changed in this audit.
- Existing open-PR search returned no open pull requests. Historical v0.4.0 implementation and release work is already on `main`.

## Evidence found in the current tree

- Scheduling indexes and parity coverage already exist: snapshot-local selector/domain caches, existing required PodAntiAffinity label candidate prefilter, topology spread aggregates, and scan-oracle parity/fuzz tests.
- CSI evidence already has optional CSINode and VolumeAttachment collection and conservative UNKNOWN tests for denied/unavailable/incomplete evidence. Docs explicitly disclaim backend attach success.
- Redaction v2 tests cover typed identity collisions, stable mappings, orphaned VolumeAttachment references, and preservation of the JSON v1 contract.
- Informer/watch code observes and sanitizes watch errors; existing tests cover lifecycle, cancellation and capability degradation. CI includes race testing.
- TUI reliability/benchmark tests exist, as do demo/empty-state, resize, interaction and rendering tests.
- The analyzer, model, collector, report and UI each have benchmarks. `docs/performance.md` records same-host before/after measurements from 2026-10-04 and clearly labels single-iteration and synthetic limits.
- Existing CI (`.github/workflows/ci.yml`) runs module integrity, formatting, vet, unit/race tests, 90% core coverage, build, fuzz smoke, synthetic benchmark smoke, offline smoke, and disposable kind E2E for Kubernetes 1.34/1.35.
- No Kubernetes API or cluster was accessed. No production or home-Lab resource was changed.

## Confirmed gap / follow-up

The committed scale fixtures stop at Small 12 Nodes/240 Pods, Medium 50/2,000, and Large 100/5,000. Peer-Dense uses only Medium and Large (50/2,000 and 100/5,000). Thus the requested 500 Nodes/10,000 Pods and same-size Peer-Dense case have **not** been benchmarked. Existing documentation accurately describes this boundary. Do not report the historical 100/5,000 numbers as evidence for 500/10,000.

The 500/10,000 full-analysis benchmark was not added or run: this environment has no Go toolchain (`go: command not found`), and the required large result matrix may consume substantial memory/time. A later change should first add a bounded, opt-in benchmark (not part of default CI timing smoke), run it on a measured host with resource limits, and record actual time/allocations. Component-level large benchmarks may be added separately if they preserve equivalent data and timing boundaries.

## Validation and remaining work

- Read-only inspection and source/document audit completed.
- Local `go test`, `go test -race`, `go vet`, coverage and benchmarks: **not run**, because Go is absent. No test result is inferred from prior release records.
- GitHub checks for the actual remote-main commit `a06dee8dc1bc6f6b9983f2286151a1dd6b04ca73` were successful. The latest observed prior completed workflow was guard9s CI on 2026-10-06; PR #6 run `39374050619` was queued at handover time. These are hosted checks, not local Go validation.
- No code fix was justified by the source audit; the concrete unmeasured 500/10,000 scale is recorded as follow-up rather than claiming unverified optimization.
- Next: use a Go 1.26.x host or hosted CI for the opt-in 500/10,000 benchmark and rerun full unit/race/vet/coverage gates before any v0.4 follow-up release decision. Keep all runtime behavior READ ONLY and do not access Kubernetes for this benchmark.
