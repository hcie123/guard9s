# Analysis contract and limitations

Each analyzer consumes a snapshot and emits severity, category, namespace/resource, reason, evidence and recommendation. An analyzer error, cancellation or collection issue emits UNKNOWN evidence. Snapshot objects are independent copies, but reading several informer stores is not a cross-resource transaction.

## PDB

The implementation uses the upstream policy/v1 label-selector conversion. A nil selector matches none; an empty selector matches all pods in that PDB's namespace. Overlapping budgets are CRITICAL. `status.observedGeneration < metadata.generation` is UNKNOWN. A zero/negative disruption allowance is conservatively BLOCKED even when an unhealthy eviction exception could apply; the policy and uncertainty appear in the evidence. No eviction request is sent.

If more target pods match a budget than its allowance, the plan warns that replacements must become healthy and the budget must recover between evictions. A positive allowance is a snapshot, not permission to evict every matching pod concurrently.

The PDB view and analyzer share the same stale-status assessment. The view includes budgets matched to active relocatable pods; DaemonSet and static pods are excluded consistently. A stale positive allowance is HIGH/UNKNOWN, never PASS.

## Replicas and storage

Owner chains follow controlling references and validate owner UIDs when present. Ready counts exclude terminal/deleting pods. Multiple desired replicas are insufficient if only one is healthy or all healthy replicas are on the target. StatefulSet replica counts do not establish application quorum.

Ownership analysis primarily supports Kubernetes built-in workload controllers.
A third-party CRD controller, such as **StrimziPodSet**, may leave its owner chain
unresolved. The manual v1.36.4 test observed `Owner chain could not be resolved
(StrimziPodSet).` with `UNKNOWN: incomplete evidence`. This is conservative
evidence handling, not silent success or inferred maintenance safety. No
controller-specific workaround or StrimziPodSet support is included in v0.4.0.

DaemonSet and static pods are excluded from relocation demand; static pods still produce a HIGH manual-runbook finding. Standalone and batch pods remain in demand estimates but have separate recreation/interruption findings.

Local PV/hostPath findings prevent a maintenance recommendation. RWX is informational, not proof of backend availability. PV node affinity is checked for candidate placement. Optional CSINode evidence checks direct CSI registration, topology keys and observed allocatable limits. Optional VolumeAttachment inventory adds observed node/driver/PV attachment identities and state; duplicate, terminating, unattached, errored, missing or otherwise ambiguous attachment evidence keeps the result UNKNOWN. Unobserved attachments, backend health, access-mode transitions, provisioning capacity and data consistency are not simulated. Inline/generic ephemeral storage is UNKNOWN.

## Scheduling and capacity

This is **best-effort scheduling feasibility analysis**, not a scheduler implementation.

Checked hard constraints: Ready and schedulable candidate nodes without reported pressure, nodeSelector, required nodeAffinity (including upstream expression/field matching), NoSchedule/NoExecute taints and tolerations, PV node affinity. Preferred constraints remain scheduler decisions.

Required podAffinity and podAntiAffinity support selectors, explicit namespace lists, default own namespace, an empty namespaceSelector (all namespaces), and observed topology domains. Affinity peers must match **all** incoming terms, as in the pinned upstream filter. The first self-matching pod may bootstrap when no matching peers exist. Existing pods' required anti-affinity also rejects proven conflicts. NamespaceSelector supports upstream label expressions using core Namespace inventory. Namespaces and NamespaceSelector form a union; a nil selector with no explicit list defaults to the incoming/owning Pod namespace, while an empty selector matches every namespace without requiring labels. Explicit namespace matches can prove union membership even without labels. Missing Namespace objects or a non-Available label capability produce UNKNOWN when a label lookup is needed. An explicitly failed core Namespace capability invalidates confirmed candidates even without a selector. Invalid selectors, feature-gated matchLabelKeys/mismatchLabelKeys, missing anti-affinity topology labels and unavailable peer nodes remain UNKNOWN.

Terminating peers cannot prove stable affinity anchors. Assigned active terminating pods still constrain anti-affinity until removal; terminal peers do not prove stable scheduling evidence. The post-removal projection excludes relocatable target pods from peer counts and retains DaemonSet/static residents. It does not establish a valid eviction/replacement order. Capacity stays UNKNOWN for multiple departing pods with interacting affinity/topology requirements, even if individual candidates exist.

Hard topology spread supports topologyKey, positive maxSkew, selectors, minDomains and default inclusion policies (nodeAffinityPolicy Honor, nodeTaintsPolicy Ignore). Counting domains follows node affinity rather than destination readiness: observed cordoned/NotReady nodes may still contribute domains. Counting nodes need every hard topology key. Same-namespace counts exclude terminating/departing peers and add the incoming pod only when it matches. An empty Pod label selector matches all peers; nil matches none. v0.3 fixes a v0.2 false PASS caused by skipping empty-selector peers. Below minDomains the minimum is zero. Already merged matchLabelKeys selectors are checked; unmerged keys and nondefault inclusion policies remain UNKNOWN because feature-gate state is unavailable. Soft constraints and scoring are not simulated.

Custom schedulers, scheduling gates, dynamic resource claims, extended resources, hostPort/hostNetwork, feature-gated tolerations and pinned templates remain UNKNOWN. Missing destination memory/disk/PID conditions also prevent confirmed candidates. Scheduler profiles, hidden added affinities, admission changes and replacement template changes are not collected.

Every observed destination has ALLOWED, REJECTED or UNKNOWN reasons. ALLOWED covers checked constraints in the projection, not actual scheduler placement. Pod detail and Scheduling diagnosis detail show full reasons; JSON/Markdown expose `schedulingCandidates`. Findings keep compact count summaries to avoid allocating the full explanation matrix during redraw.

hostPort inspection covers regular containers and init containers, including native sidecars. Multiple constraints with the same unsupported reason produce one UNKNOWN explanation rather than duplicates.

Resource requests use Kubernetes' component-helpers implementation, including init containers, native sidecars, overhead, pod-level requests and container/pod-level resize allocations. Current requests and allocatable values are not live utilization. Missing CPU/memory requests prevent a positive capacity recommendation. Resize status, when present, is accounted for conservatively; absent status does not add a reservation.

Eligible node capacity is a union of per-pod candidates, counted once per node. DaemonSet and other resident active pods consume destination capacity. The estimate first tests aggregate limits and individual-pod fit, then tries a deterministic constrained/larger-first allocation. Successful placement is only a snapshot estimate. Failure of the placement heuristic is UNKNOWN rather than proof that no alternative placement exists.

Active unscheduled pods are counted separately as pending demand. Their presence forces capacity to UNKNOWN: the estimate does not allocate the scheduler queue or assume every pending pod can use the same nodes. Terminal unscheduled pods are excluded. Missing target nodes and incomplete collection remain UNKNOWN even when no relocatable pods were observed.

Preemption, unobserved CSI attachments, ephemeral storage and concurrent scheduling changes are not modeled as a full scheduler queue. Inspect cluster risks and pending pods before acting. Multiple node maintenance is unsupported; assessing nodes individually does not make simultaneous maintenance safe.

## Refresh and exported evidence

Open plans use the latest successful snapshot or the prior snapshot marked UNKNOWN after refresh failure. Their contents are regenerated, so an old READY command sequence is removed when blockers or collection issues appear. Resource selection follows identity across row insertion/reordering; a removed resource invalidates its open detail. This does not make the snapshot atomic or guarantee freshness between refreshes.

One diagnosis computes each pod's placement candidates once, calculates destination free capacity once per node and reuses request arithmetic during placement. UI and exported reports consume that same capacity result. The cache is private to one diagnosis/snapshot and never spans refreshes. Refresh failures copy prior collection issues before marking evidence UNKNOWN; successful and failed collection results are analyzed in the background, with widget updates kept on the UI loop. Capacity redraws consume prepared results; absent results remain UNKNOWN.

`--node NAME --output json|markdown` exports one diagnosis without a terminal. The explicit schema includes snapshot time, string severities, UNKNOWN markers and capacity units. Namespace display filters do not restrict export analysis. Kubeconfig paths, raw manifests and container environment fields are excluded, but arbitrary business text in names or event evidence is not automatically redacted. A successful export exits 0 regardless of readiness; consumers must inspect the report's readiness field.

Finding `code` identifiers are not part of the v0.4.0 JSON contract; consumers
should use the documented fields and must not assume a stable machine-readable
code exists. Detail text can wrap in narrow terminals; a wider terminal and the
full detail view improve readability without changing the analysis.

## v0.4 runtime and sharing

One worker runs collection/analysis; a refresh burst has at most one pending notification and cancels superseded work. A generation advances immediately on request. Older results are discarded, including queued results waiting for redraw. Quit cancels and joins UI workers and source informer goroutines, including optional API retries; sources must honor context. Immutable lookups/owners/requests/selectors belong to one snapshot. Scheduling-only calls avoid evaluating capacity requests. There is no global cache or sync.Pool.

Watch error events are observed and sanitized before client-go retries/logging. Expired resource versions, failed relists and revoked RBAC keep collection UNKNOWN until a new startup verifies caches. Ordinary relist recovery does not silently restore a maintenance recommendation. The wrapper preserves optional WatchList capability detection, including fake-client list/watch semantics.

`--redact` or `--redact=basic` masks known identifiers with per-report pseudonyms, detected network addresses, images and recorded paths. Default `--redact=none` preserves names. Severity/category, UNKNOWN/candidate status and structured quantities remain intact. Aliases are type-aware and deterministic for the same snapshot/report, independent of inventory order. Kind-specific fields and explicit Kind/name references keep distinct identities; ambiguous untyped names use deterministic identifier aliases. Node and CSINode references share the Node alias in untyped prose. Aliases may change when inventory changes. Ambiguous free text, arbitrary business prose and encoded secrets cannot be guaranteed removed; basic is best-effort, not anonymization. Redacted commands contain placeholders and the exact notice: Redacted commands are documentation-only and must not be executed verbatim. JSON v1 adds optional redaction flags, capabilities and finding resourceKind without removing or changing prior fields.

Filter clauses are ANDed. `namespace:demo`, `status:Pending`, `category:Storage`, `node:worker-01`, `name:nginx` are case-insensitive substrings; `=` selects exact values. `risk>=HIGH` supports severity comparisons. Bare text searches the row and evidence. `s` cycles persistent sorting in Nodes/Pods/Risks. Numeric sorts descend by requested quantities/restarts/pod counts, with identity tie breaks. Tests cover 80×24, 100×30, 120×40 and 160×44 and preserve selection/view/filter/detail/scroll on resize.

PVC Pending or missing binding produces UNKNOWN phase/binding evidence. PVC Lost/terminating, PV Released/Failed/terminating and observed PV affinity conflicts produce HIGH. Missing StorageClasses remain UNKNOWN. [CSI evidence](csi-analysis.md) documents optional CSINode and VolumeAttachment collection, observed driver/topology/limit/attachment checks and unavailable evidence. Missing or incomplete attachment inventory never proves spare attachment capacity; it degrades the affected result to UNKNOWN.

## Snapshot capability and scheduling index

Capabilities are explicit Available/Forbidden/Unsupported/Unavailable states. Object absence never creates Available implicitly. Namespace is required; optional CSINode and VolumeAttachment list/watch/sync failures do not enter the core startup gate. Relevant CSI/attachment checks become UNKNOWN while non-CSI analysis continues. Core watch errors remain sticky UNKNOWN; optional errors remain sticky in their capability. A failed UI refresh copies prior inventory and downgrades previous Available capabilities to Unavailable. Prior snapshot maps are not mutated.

SchedulingIndex v2 is private to one immutable snapshot and one sequential analysis worker. It compiles recurring terms and Namespace label matches, uses equality-label/namespace candidate lists, groups existing anti-affinity terms, and aggregates topology peer counts with per-source-node removals. Projected counts and spread proofs are reused for each maintenance target. All-required-term affinity matching and any-term anti-affinity matching retain their different semantics; nondeparting incoming self references are removed once. Scan-oracle tests compare 4,608 candidate outcomes without using these aggregate maps.

Shared selectors avoid repeated full peer scans in dense workloads. Unique broad or negative selectors can still approach quadratic Pod work; the exported candidate matrix requires Pods × Nodes evidence. There is no global cache or reuse across refreshes, and no claim of general linear complexity. Missing affinity peer node/topology prevents a definite no-peer rejection and instead produces UNKNOWN.

## Health and events

High restart counts are cumulative; OOM history can persist after recovery. Recent significant events (last hour) contribute WARN findings to node diagnosis. Event view also exposes older retained warnings. Normal success noise is suppressed. Unscheduled pods appear in cluster Risks and Events rather than a specific node diagnosis.

## Source notes

Primary references used for implementation:

- [Kubernetes PDB API](https://kubernetes.io/docs/reference/kubernetes-api/policy/pod-disruption-budget-v1/)
- [Disruption budgets](https://kubernetes.io/docs/tasks/run-application/configure-pdb/)
- [Resource requests](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)
- [Sidecar containers](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/)
- [Namespace selection](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#namespace-selector) and pinned [PodAffinityTerm API](https://github.com/kubernetes/api/blob/v0.35.6/core/v1/types.go)
- [client-go](https://github.com/kubernetes/client-go)
- [Pinned InterPodAffinity filter](https://github.com/kubernetes/kubernetes/blob/v1.35.6/pkg/scheduler/framework/plugins/interpodaffinity/filtering.go)
- [Pinned topology spread filter](https://github.com/kubernetes/kubernetes/blob/v1.35.6/pkg/scheduler/framework/plugins/podtopologyspread/filtering.go) and [count/selector helpers](https://github.com/kubernetes/kubernetes/blob/v1.35.6/pkg/scheduler/framework/plugins/podtopologyspread/common.go)
- [Topology spread](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/)
- [tview](https://github.com/rivo/tview)

Dependencies are pinned in go.mod/go.sum. Unit tests validate the supported model; they do not replace a real test-cluster compatibility exercise.
