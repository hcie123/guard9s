# Changelog

## v0.4.0 — Initial Open Source Release

v0.4.0 was published on 2026-10-06 as guard9s' initial open source release.
The standalone repository is public, and the formal GitHub Release is available
at https://github.com/hcie123/guard9s/releases/tag/v0.4.0. The release tag points
to commit `e0c2f54208085a8ef72d11c59873923ee2404b74`.

### What guard9s does

guard9s gathers read-only Kubernetes evidence to explain node maintenance risk
before an operator considers cordon or drain. It does not execute maintenance.

### Highlights

- Read-only Kubernetes maintenance diagnosis in a keyboard-driven TUI: Node,
  Pod, PDB, health, replica, event, capacity, storage, diagnosis and plan evidence.
- Request-based CPU/memory/Pod capacity and conservative scheduling evidence:
  required affinity/anti-affinity, NamespaceSelector and hard topology spread.
- PVC/PV/StorageClass analysis, CSINode driver/topology/advertised limits and
  optional observed VolumeAttachment evidence.
- ALLOWED / REJECTED / UNKNOWN explanations, deterministic typed basic
  redaction, and JSON `guard9s/v1` / Markdown exports.
- Six prebuilt targets: Linux, macOS and Windows on amd64 and arm64. End users
  running the prebuilt binary do not need Go; building source requires Go 1.26+.

### Compatibility

- CI integration on disposable three-node kind Kubernetes v1.34.11 and v1.35.8:
  list/watch sync, update/delete/restart, scheduling/PDB/storage evidence,
  optional API degradation and transport guards.
- Manual Kubernetes v1.36.4 test-environment validation on 2026-10-05, using a
  Linux amd64 CI artifact on a machine without Go: TUI, JSON and basic-redacted
  JSON passed the reported checks. The selected worker remained NOT RECOMMENDED.
  Only anonymized counts and software versions are retained in
  [verification.md](docs/verification.md).
- Unit, race, coverage, fuzz, synthetic benchmark, offline demo and nonpublishing
  release-snapshot checks are retained. Version-specific test evidence is not a
  guarantee for other Kubernetes versions or production environments.

### Installation

Use a prebuilt archive for Linux, macOS or Windows on amd64/arm64; Go is not
required to run it. Verify the matching checksums.txt before extraction. Formal
v0.4.0 downloads are available from the GitHub Release linked above. Source
builds require Go 1.26+, as declared in go.mod. The single bilingual
[README](README.md#installation) includes installation and CLI examples.

### Safety model

- Production Kubernetes requests pass a GET-only transport and an inventory
  resource allowlist. RBAC grants get/list/watch only. Secrets and subresources
  such as exec, logs, port-forward, scale and eviction are excluded.
- No automatic mutation, shell/credential-plugin execution, telemetry,
  phone-home, external service integration or report upload. Plan commands are
  documentation and operator suggestions only.

### Known limitations

- Missing, failed or ambiguous evidence remains UNKNOWN. Snapshots are not
  cross-resource transactions and ALLOWED is not a guarantee of placement or
  maintenance safety. CSI backend health and future attach/detach remain UNKNOWN.
- Built-in workload controllers are the primary ownership model. Third-party
  CRD controllers, including StrimziPodSet, can produce unresolved owner chains
  and UNKNOWN / incomplete evidence. No controller-specific workaround is added.
- Finding `code` identifiers are not part of the current JSON contract. Long
  lines can wrap in narrow terminals. Zero observed VolumeAttachment objects
  does not validate attachment transitions. Basic redaction is not complete
  anonymization; inspect business prose before sharing.

The entries below are development history, not additional published releases.

## Development history — v0.4 live API and CSI attachment evidence

- Fix Close/startup ready-publication races, duplicate existing anti-affinity self exclusion and orphan/conflicting CSI attachment-node evidence.
- Preserve UNKNOWN for failed Namespace evidence even if a caller retains its earlier Available state.
- Verify positive-selector prefilters against an independent scan oracle; share negative-selector domain counts only when snapshot label inventory proves identical peer matches.
- Extend kind E2E with synthetic CSINode/VolumeAttachment evidence and watch invalidation; make teardown verification a required result.
- Include dependency license/notice texts in snapshot archives and retain the 90% aggregate core coverage gate.

- Add disposable three-node kind integration against Kubernetes v1.34.11 and v1.35.8 with a separate get/list/watch guard9s identity and writable fixture-only admin client.
- Validate list/watch startup, PDB refresh, watch update/delete/restart behavior, headless JSON, optional API degradation and transport-level write rejection against real API servers.
- Collect optional storage.k8s.io/v1 VolumeAttachment objects and combine them with CSINode allocatable limits for conservative observed CSI attachment headroom; incomplete/ambiguous evidence remains UNKNOWN.
- Preserve the read-only boundary: production transport rejects non-GET requests and the validated kind runs observed zero collector non-GET requests.
- Stream JSON reports without buffering the entire document and retain guard9s/v1 additive compatibility.
- Stabilize watch E2E fixtures by using a clean synthetic Pod and metadata patching instead of racing full-Pod updates with kubelet status writes.
- Remove a redundant cold binary build from each kind E2E matrix job; the headless integration step dropped from roughly 70–88 seconds to about 0.12 seconds in the validated run.
- Keep the personal-cluster live-test harness default-off; no company or production cluster was contacted.

## Development history — v0.3 scale, evidence and real-world readiness

- Collect core Namespaces and optional CSINodes using get/list/watch only, with explicit capability states and joined informer shutdown.
- Resolve NamespaceSelector expressions/unions and reuse aggregate peer, existing anti-affinity and topology counts per snapshot/target.
- Fix empty topology selectors falsely allowing skew violations and missing affinity peer topology falsely proving no peer.
- Add dense 50/2,000 and 100/5,000 peer fixtures, seven benchmarks, scan parity and capability refresh regressions.
- Add direct bound CSI driver/topology/observed volume-limit evidence; VolumeAttachment remains uncollected.
- Keep JSON v1 with additive metadata, deterministic type-aware redaction and documentation-only command placeholders.
- Group candidate details by status without expanding main tables.
- Add default-off read-only personal live-test harness and honest compatibility/performance/verification records.

## Development history — v0.2 reliability and scheduling

- Explain ALLOWED/REJECTED/UNKNOWN destinations for supported required inter-pod affinity, anti-affinity (including existing pods), and hard topology spread.
- Preserve UNKNOWN for unavailable namespace labels/feature gates and interacting multi-pod migrations; add destination health and storage lifecycle evidence.
- Index snapshot inventory/requests/owners/selectors, share all-node analysis and build full candidate text on demand.
- Cancel/coalesce refresh requests, discard stale generations, and join workers on quit.
- Add basic report pseudonyms, address/image/path masking, explicit limits, and bare `--redact` support.
- Add field filters, persistent sorting, breadcrumbs and four-size terminal regressions.
- Sanitize watch error events before reflector retries/logging, including expired resource versions and revoked RBAC.
- Add scheduling demo, four goldens, fuzz smoke, realistic 12/240, 50/2000 and 100/5000 benchmarks, and deferred CSI research.
- Retain GET-only transport, resource/RBAC scope and the aggregate 70% coverage gate.

## Development history — V1 preview

- Add guard9s as an independent Go module.
- Add offline mixed/healthy demo scenarios and keyboard-first tview/tcell UI.
- Add Nodes, Pods, Risks, Events, Diagnosis, Maintenance Plan, Capacity, PDB and Storage views.
- Add eight evidence-based analyzers with conservative UNKNOWN results.
- Add protected client-go informer collection and least-privilege RBAC example.
- Add fake-client, transport, analyzer, report, screen and keyboard tests.
- Add CI, static single-binary build and nonpublishing GoReleaser snapshot configuration.
- Add headless JSON/Markdown reports with explicit node selection, schema version, timestamps and capacity units.
- Keep open plans current after refresh; retain resource selection and scrolling, invalidate deleted details and deduplicate refresh errors.
- Classify missing targets, incomplete empty snapshots and pending cluster demand as UNKNOWN capacity.
- Unify stale PDB status across diagnosis and budget views; prevent empty plan summaries from showing READY.
- Add focused regressions and offline binary export smoke checks.
- Reuse scheduling/capacity evidence within one diagnosis; cache capacity for redraw and compute each destination's free resources once.
- Analyze refresh failures in the background without mutating prior snapshot issues.
- Include initContainer/native sidecar hostPort constraints and deduplicate UNKNOWN explanations.
- Add synthetic capacity-redraw/report benchmarks and regressions for cache replacement, cancellation and result parity.

v0.4.0 is the first public guard9s release. No Kubernetes mutation functionality is included.
