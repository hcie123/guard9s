# Compatibility and validation scope

guard9s v0.4 has the following distinct evidence. No company/production validation
is claimed, and compatibility outside the tested versions is not guaranteed.
This is not a blanket support policy for every Kubernetes 1.34–1.36 release.

| Validation class | Kubernetes server | Environment and scope |
| --- | --- | --- |
| CI integration | v1.34.11 | Disposable three-node kind; separate get/list/watch identity |
| CI integration | v1.35.8 | Disposable three-node kind; separate get/list/watch identity |
| Manual test environment | v1.36.4 | 2026-10-05; Linux amd64 CI archive, no Go installed; TUI and JSON/basic-redacted JSON |

The manual record was supplied by the project maintainer. It observed 23 Nodes
and 183 Pods; the selected worker's maintenance result was NOT RECOMMENDED.
JSON export exited 0 with schema `guard9s/v1`, `readOnly=true`, 57 findings,
four scheduling candidates and 16 capabilities. The TUI rendered and refreshed
without a reported panic or crash. Full anonymized counts and comparison results
are in [verification.md](verification.md). Original cluster identifiers, report
files and credentials are not included in the repository.

| Item | Boundary |
| --- | --- |
| Prebuilt binary | No Go installation required to run |
| Source build | Go 1.26.0 minimum in go.mod; local validation uses Go 1.26.8 |
| CI toolchain | Go 1.26.x, pinned actions and node images, ubuntu-24.04 |
| Kubernetes dependencies | client-go, api, apimachinery and component-helpers v0.35.6 |
| Credential modes | Trusted read-only kubeconfig with verified TLS; exec/auth-provider plugins and insecure TLS unsupported |
| Required inventory | Core Namespace/Node/Pod/Event/PVC/PV, built-in apps/batch controllers, PDB and StorageClass APIs in deploy/rbac.yaml |
| Optional inventory | storage.k8s.io/v1 CSINode and VolumeAttachment; failure affects relevant evidence without blocking core startup |

The CI fixture installer uses a writable admin client only inside its disposable
test harness. guard9s uses a separate read-only identity. CI covers initial
list/watch synchronization, update/delete/watch restart and resourceVersion
behavior, PDB refresh, NamespaceSelector, affinity/anti-affinity, topology spread,
synthetic CSINode/VolumeAttachment evidence, headless JSON, optional Forbidden
degradation and blocked POST/Secret GET. CI's zero non-GET reader count is evidence
for those runs; the manual test record does not supply API audit-log counts.

## Workload and storage limits

Ownership analysis primarily follows Kubernetes built-in workload controllers.
Third-party CRD controllers such as **StrimziPodSet** may leave the owner chain
unresolved. The manual test observed UNKNOWN / incomplete evidence for that
case. This is a conservative boundary, not silent success or a safe-maintenance
inference; no Strimzi-specific support is claimed.

The manual inventory included four PDBs, 23 PVCs, 23 PVs, one StorageClass and
23 CSINodes. It observed zero VolumeAttachment objects, so positive attachment
state/transition coverage was not established by that environment. Zero objects
must not be confused with proof of backend safety. Available-empty, Forbidden,
Unsupported and Unavailable remain distinct capability states.

Snapshots are not atomic across resources. Scheduler profiles, unsupported
feature gates, interacting replacement placement, backend health and future
attach/detach success remain outside the validated model. ALLOWED means the
implemented checks pass observed evidence, not a scheduler or drain guarantee.

## Optional source-checkout live-test harness

`make live-test` remains default-off. CI checks its syntax/help/refusal separately
from the disposable kind matrix. The 2026-10-05 manual record used the prebuilt
binary directly; it does not claim execution of this optional harness.

The [read-only live-test guide](live-testing.md) covers explicit test-context
selection, direct binary use, local report handling and the opt-in harness.
Record software versions, artifact identity when available, anonymized counts
and sanitized results. Do not publish a real kubeconfig, cluster identifiers,
internal IPs or raw reports. Missing profile/gate/backend evidence stays UNKNOWN.
