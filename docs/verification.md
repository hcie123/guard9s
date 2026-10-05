# guard9s v0.4.0 validation record

## Manual test-environment validation — 2026-10-05

The project maintainer supplied this manual test record. It used a **prebuilt
Linux amd64 CI archive on a test machine without Go**, exercising the ordinary
binary-installation path. This is validation of one Kubernetes **v1.36.4 test
environment**, not official support for every Kubernetes 1.36 release or any
production environment. No cluster was contacted to prepare these documents.

Only aggregate counts, software versions and anonymized results are retained
here. The selected worker is called `test-worker-1`; the context is described as
the test kubeconfig context. Real identifiers, addresses, raw reports and
credentials are intentionally not included. The supplied record did not include
the CI archive filename, artifact checksum or embedded binary commit, so none
is inferred for this manual run.

| Item | Reported observation |
| --- | --- |
| Date / platform | 2026-10-05 / Linux amd64 |
| Kubernetes server | v1.36.4 |
| Binary / Go on test machine | Prebuilt Linux amd64 CI archive / Go not installed |
| Cluster snapshot | 23 Nodes, 183 Pods |
| Selected worker | test-worker-1 (anonymized) |
| Maintenance readiness | NOT RECOMMENDED |
| JSON exit code / parsing | 0 / PASS |
| JSON schema / readOnly | guard9s/v1 / true |
| Findings / scheduling candidates / capabilities | 57 / 4 / 16 |

| Finding severity | Count |
| --- | ---: |
| HIGH | 31 |
| WARN | 12 |
| INFO | 6 |
| PASS | 8 |
| Total | 57 |

| Finding category | Count |
| --- | ---: |
| Capacity | 1 |
| Node | 5 |
| PDB | 5 |
| PodHealth | 4 |
| Replica | 9 |
| Scheduling | 4 |
| Storage | 29 |
| Total | 57 |

| Observed resource | Count |
| --- | ---: |
| PDB | 4 |
| PVC | 23 |
| PV | 23 |
| StorageClass | 1 |
| CSINode | 23 |
| VolumeAttachment | 0 |

Zero observed VolumeAttachment objects does not establish attachment transition
or backend coverage. It is not a reason to create fixtures in this test cluster.

### Manual redaction comparison

| Field | Original JSON | Basic-redacted JSON |
| --- | --- | --- |
| Parsing | valid | valid |
| schemaVersion | guard9s/v1 | guard9s/v1 |
| readOnly | true | true |
| readiness | NOT RECOMMENDED | NOT RECOMMENDED |
| findings | 57 | 57 |
| schedulingCandidates | 4 | 4 |
| capabilities | 16 | 16 |

Basic redaction preserved the reported analysis result and these fields in this
sample. Matching counts alone does not prove every field is identical; same-
snapshot severity/UNKNOWN/quantity/candidate-status preservation has separate
automated regression coverage. Basic redaction is not complete anonymization,
and arbitrary Event/business prose still requires local review before sharing.

### Manual TUI result and limitations

**PASS**, as reported by the maintainer: READ ONLY was visible, the context was
correct, 23 Nodes and 183 Pods were shown, the cache refreshed and Kubernetes
v1.36.4 was visible. Node inspection, findings, capacity, scheduling and storage
evidence rendered. The selected worker remained NOT RECOMMENDED. No panic or
crash was reported.

The test encountered a StrimziPodSet owner chain that could not be fully resolved.
guard9s displayed `Owner chain could not be resolved (StrimziPodSet).` and
`UNKNOWN: incomplete evidence`. This is a conservative limitation for third-party
CRD controllers, not silent success. No controller support, finding code field,
UI feature or storage behavior is changed for this release preparation.

This manual record contains no API audit-log request counts, scheduler-profile
or feature-gate inventory, or backend attach/detach success evidence. The CI
transport audit below has a separate scope. No production validation is claimed.

## Release preparation validation — historical 2026-10-05

Before standalone migration, the release-preparation branch started from main commit
`4f30d6aada445c4eea841ce92c12c2a360356538`. Product code, tests, dependencies,
RBAC and the CI workflow are unchanged. The changes consolidate the bilingual
README, record validation and release notes, and update archive file selection.

Local Linux amd64 validation used **Go 1.26.8** and **GoReleaser OSS 2.18.2**.
The toolchain archives were checked against their official SHA256 manifests.

| Gate | Measured result |
| --- | --- |
| Module integrity / tidy | PASS; go.mod and go.sum unchanged |
| Format / vet | PASS |
| Unit tests | PASS; 102 top-level Test functions, 385 child/seed results and 4 Fuzz parents: 491 passing results |
| Race detector | PASS |
| Core coverage | 95.9% aggregate; analyzer 94.8%, model 99.3%, kubernetes 96.8%, report 96.2%; 90% gate passes |
| Existing fuzz smoke | All four targets PASS, each configured for 2 seconds with 2 workers |
| Existing benchmarks | All 38 cases PASS; GOMAXPROCS=2, benchmem, benchtime=1x |
| Build / default-off live-test guard | PASS |
| CLI / offline exports | Help, version, JSON, basic-redacted JSON and Markdown PASS for all three demo scenarios |
| Offline TUI | mixed, healthy and scheduling start and quit with invalid KUBECONFIG; READ ONLY visible |
| Release snapshot | Six archives, checksum manifest and packaged documentation verified; publication disabled |
| Prebuilt execution | Extracted Linux amd64 binary passes version/help/offline JSON with Go absent from PATH |

The benchmark run is a single-iteration smoke check, not a comparative performance
study. Existing baselines and measurement limits remain in [performance.md](performance.md).
Offline reports retained BLOCKED / READY / NOT RECOMMENDED for mixed / healthy /
scheduling respectively. Basic redaction preserved schema, readOnly, readiness,
counts, severity, UNKNOWN, candidate status and structured quantities.

The runtime audit confirmed GET-only transport, inventory allowlisting, blocked
Secret/subresource requests and credential-plugin protections. The scoped file
and diff scan found no real sensitive information; synthetic credential canaries
and test addresses were reviewed and retained. CI actions remain commit-pinned,
permissions remain read-only, and no publishing credentials were added.

Each archive contains one bilingual README, licensing/security/contribution
files, the RBAC example and docs. Commit-specific snapshot filenames, SHA256
values and fresh PR CI results are recorded in the release-preparation PR.
An untagged repository produces a `0.0.0-SNAPSHOT-*` version; this is not a formal
v0.4.0 release. No tag, release, visibility change or standalone extraction is
performed by this preparation.

---

# v0.4 implementation closeout — historical 2026-10-04

Validation date: **2026-10-04**. Branch: `feat/guard9s-v04-live-storage`. Final implementation commit: **`e4c63ce8f55b5e7c1e811698f3499beee2d9cd72`**. The measurements below belong to this exact implementation. The closeout documentation commit changes these records and a fixture comment only. Historical source-repository evidence (not a current project link): the final PR head, latest CI and squash-merge commit are available in [PR #11](https://github.com/hcie123/infra-lab/pull/11); a document cannot embed its own future commit hash.

## Local validation

The complete sequence began and ended with a clean checkout, using **Go 1.26.8, Linux amd64, AMD EPYC 9V74, GOMAXPROCS=2**. The core coverage gate remains **90%**.

| Check | Observed result |
| --- | --- |
| `go mod verify`, `go mod tidy`, module diff | PASS; no go.mod/go.sum changes, including relative to main |
| gofmt on all changed Go files; `make fmt-check` | PASS |
| `go vet ./...` | PASS |
| `go test ./... -count=1 -json` | PASS; 491 successful Go test results, 0 failures |
| `go test -race ./... -count=1` | PASS; no race detected |
| `make coverage` aggregate | **95.9%** statement coverage |
| analyzer / model / kubernetes / report | **94.8% / 99.3% / 96.8% / 96.2%** |
| `make build` | PASS; CGO_ENABLED=0 |
| `make live-test-guard` | PASS; syntax, help and default-off refusal |
| `--help`, `--version` | PASS |
| Offline mixed JSON | PASS; `guard9s/v1`, readOnly, BLOCKED, no actionable drain plan |
| Offline healthy Markdown and JSON | PASS; READY; commands are operator documentation only |
| Offline scheduling basic-redacted JSON | PASS; parseable, conservative readiness, typed alias, no original worker identifier |

The 491 results comprise 102 Test functions, 385 child/seed results and four fuzz entry results; they are not 491 independent test functions. Fuzz iterations, benchmark operations and oracle comparisons are separate evidence. All offline demos used a nonexistent KUBECONFIG.

| Fuzz target | Executions | Result |
| --- | ---: | --- |
| FuzzSelectorsAndTolerations | 7,544 | PASS |
| FuzzExistingAntiAffinityPrefilter | 4,697 | PASS |
| FuzzFilterParser | 25,656 | PASS |
| FuzzRedactionAndSerialization | 565 | PASS |

Each fuzz smoke requested two seconds and two workers. These bounded runs are regression evidence, not exhaustive proof.

## Real Kubernetes API integration

Historical source-repository implementation CI: [guard9s CI #115, run 37213698051](https://github.com/hcie123/infra-lab/actions/runs/37213698051), head `e4c63ce8f55b5e7c1e811698f3499beee2d9cd72`. CI uses pinned actions, kind and node-image digests, Go 1.26.x and ubuntu-24.04. The dedicated workflow triggers feature-branch work through pull_request; it does not duplicate that work with a feature-branch push trigger.

| CI job / evidence | Kubernetes v1.34.11 | Kubernetes v1.35.8 |
| --- | --- | --- |
| kind E2E | PASS | PASS |
| Actual server version | v1.34.11 | v1.35.8 |
| Protected reader GET requests | 53 | 53 |
| Protected reader non-GET requests | 0 | 0 |
| POST and Secret GET blocked | PASS | PASS |
| Watch update/delete/restart | PASS | PASS |
| Observed CSINode/VolumeAttachment; detachError becomes UNKNOWN | PASS | PASS |
| Optional VolumeAttachment Forbidden degradation | PASS | PASS |
| Clean kind teardown, cluster absence verified | PASS | PASS |

The same run's **verify** and **release-snapshot** jobs also passed. The latter built all six platforms, verified all six archive checksums and executed the Linux snapshot binary; publication was disabled. Actual CI toolchain: Go 1.26.8. As a pull_request run, Actions checked out test-merge commit `267ae989929c3acab292b9e2f89bcd4e358ffaaa`; its Git tree exactly matches the implementation head above.

The counted requests are the protected reader client's source/list/watch traffic, including the optional-denial source. They exclude fixture-admin operations, authorization/discovery harness calls and the separate headless-command client. GET includes LIST and WATCH HTTP requests. Counts vary with watch and optional-API retries; zero non-GET is the invariant.

Both versions exercise an actual three-node API server, a separate read-only ServiceAccount, initial list/watch synchronization, informer update/delete, resourceVersion changes, forced watch restart, PDB refresh, NamespaceSelector, affinity/anti-affinity, topology spread, and headless JSON. Admin credentials passed to the production transport still cannot send POST or read Secrets.

The CSI subtest creates synthetic CSINode, bound PV/PVC and VolumeAttachment API objects with the fixture-only admin client. It observes the advertised allocatable limit and attachment state, then updates detachError and waits for watch evidence to change the candidate to UNKNOWN. There is **no working CSI backend** and no claim of successful provisioning, mount, detach or attach. Removing VolumeAttachment RBAC independently verifies optional Forbidden degradation without blocking core startup.

Local Docker/kind was unavailable; this live matrix was executed in GitHub Actions, not on the local workstation. **At the 2026-10-04 closeout, personal home/test cluster: NOT RUN; company/production cluster: NOT RUN.** The later manual test-environment record is documented above.

## Correctness and lifecycle closeout

The selector index remains a candidate prefilter followed by complete matching. Table/property regressions cover positive labels and In, combined selectors, negative-only selectors, nil/empty/invalid selectors, MatchLabelKeys/MismatchLabelKeys, absent or partial candidate labels, multiple and duplicate groups, and incomplete Namespace inventory. Independent scan parity covers PeerDense and PathologicalUniqueSelectors, including missing peer nodes/domains, terminal/deleting peers and unavailable/missing/failed Namespace evidence. Rejected, unknown and checked reasons are compared; no mismatches remain in these tests.

The closeout reproduced and corrected four concrete correctness/lifecycle gaps:

- Duplicate existing anti-affinity terms left a phantom self-peer and a false rejection; self exclusion now subtracts the actual multiplicity.
- Available-but-failed Namespace capability could be treated as usable; affected checks now remain UNKNOWN.
- Missing attachment nodes or an attachment source conflicting with the assigned Pod could produce positive CSI evidence; these cases now remain UNKNOWN.
- Close overlapping final initial-sync publication could leave Source ready; closed/canceled state is now checked under the lifecycle lock.

Source is one-shot. First Start succeeds; duplicate/concurrent Start cannot create two informer groups; Close cancels and joins core and optional workers; closed snapshots are not ready; Start after Close requires a new Source. The deterministic close-during-sync and concurrent-start regressions pass under the race detector.

CSI regressions retain UNKNOWN for unavailable/unsupported/forbidden inventory, watch failure, duplicate/ambiguous attachments, absent or conflicting nodes, wrong attacher, attach/detach errors, deleting attachments, missing volumes and incomplete usage. CSINode limits are observed API capacity evidence. **Backend health / future attach-detach success remains UNKNOWN.**

## Read-only, report and redaction boundary

RBAC has five resource rules, each exactly **get/list/watch**, with no wildcard, Secret or ConfigMap permissions. Production transport permits only GET to the inventory resource families listed in deploy/rbac.yaml. POST/PUT/PATCH/DELETE/CONNECT are rejected before the underlying transport. Secret GET and unsupported resources/subresources are rejected, including logs, status, exec, attach, port-forward, scale and eviction. Informer list/watch and restart remain functional.

There is no production mutation, process/shell execution, automatic cordon/drain, Secret access, telemetry, phone-home or hidden network path. Maintenance commands are text suggestions only. Exec/auth-provider credential plugins and insecure TLS are rejected; error paths sanitize credential-bearing details.

Streaming JSON tests match standard json.Encoder output, including nil/empty slices, stable finding/candidate and map order, and propagated writer errors. Legacy consumers still parse schemaVersion **guard9s/v1**. Markdown and basic-redacted JSON smoke checks pass.

Redaction tests cover context, Kubernetes workload/storage identities, CSI driver/volume identifiers, IPv4/IPv6, hostname/domain/URL and local/kubeconfig paths. Orphan VolumeAttachment node/PV references are included. Aliases are deterministic and typed; severity, UNKNOWN, readiness, quantities and candidate status are preserved.

## Performance

The committed [performance record](performance.md) contains all 21 final benchmark rows with ns/op, B/op and allocs/op, exact baseline/source commits, profile evidence and timing boundaries. Single-iteration sequential samples on the same machine:

| AnalyzeAllNodes fixture | Before | Final |
| --- | ---: | ---: |
| PathologicalUniqueSelectors, 100 Nodes/5,000 Pods | 9,544,708,837 ns; 475,900,784 B; 6,041,299 allocs | 510,570,595 ns; 257,022,768 B; 3,994,844 allocs |
| Ordinary Large | 513,267,535 ns; 220,124,720 B; 4,201,216 allocs | 459,864,700 ns; 220,159,928 B; 4,208,097 allocs |
| PeerDense Large | 1,060,560,638 ns; 426,083,088 B; 7,440,604 allocs | 1,070,761,845 ns; 426,172,496 B; 7,445,577 allocs |

The pathological hotspot was profiled before the bounded snapshot-local optimization. No global/cross-snapshot cache or timing CI gate was added. Arbitrary unique selectors may still approach quadratic peer work; the Pod × Node evidence matrix remains a real memory cost. These samples do not establish production latency.

## Release snapshot, secrets and provenance

Local **GoReleaser 2.18.2** was verified against its upstream checksum and ran `release --snapshot --clean --parallelism 2` successfully. All six CGO-disabled targets passed: linux/darwin/windows × amd64/arm64. SHA256 checks passed for every archive. Archive inspection matched each required document's bytes to the checkout: both READMEs, LICENSE, NOTICE, CHANGELOG, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, deploy/rbac.yaml, every docs/*.md file and docs/demo.svg. The Linux amd64 snapshot binary passed version and offline JSON smoke. No release/tag/publication command was used.

The scan covered the full PR diff, tracked guard9s files and the dedicated workflow, using credential/private-key signatures, token patterns, Authorization/Bearer/password/secret and certificate/key markers, domain/IP checks and manual review of suspicious contexts. The strong-signature hit was an explicitly synthetic non-key canary in credential_test.go used to verify error sanitization; it contains no actual private-key body. Other keyword hits were synthetic tests, documentation and guarded ephemeral kind credentials. No real credential, company marker or production fixture was identified by these checks. This is a report of the scan performed, not an absolute guarantee that arbitrary future input is secret-free.

go.mod/go.sum are unchanged. The 48 modules linked by `go list -deps ./cmd/guard9s` use reviewed Apache-2.0, MIT, BSD or ISC texts; their license/notice texts and the Go runtime license are included in NOTICE and snapshot archives. No known copyleft conflict or copied application code from a blog, StackOverflow or another repository was identified. No such code was copied during this closeout.

That historical closeout was confined to the project and its dedicated workflow in the private source repository. It did not perform standalone extraction, change repository visibility, create a public fork, tag or GitHub Release, or publish binaries/containers. Current project paths are relative to the standalone repository root, with CI in `.github/workflows/ci.yml`.

## Remaining limitations

ALLOWED means that the implemented checks pass the observed evidence, not a scheduler placement or maintenance-safety guarantee. Snapshots are not cross-resource transactions. Scheduler profiles, hidden affinities, unsupported feature gates, interacting replacement scheduling, backend health and future storage operations are outside the model. The pinned kind versions are validated integration evidence only. Production behavior remains untested; the later manual v1.36.4 sample above has its own limited scope.

Earlier validation records remain in Git history. The implementation closeout and the later manual test-environment record are separately dated above.
