# CSI evidence in v0.4

CSINode and VolumeAttachment are optional read-only enhancements. Namespace collection is core evidence. The collector records Available, Forbidden, Unsupported or Unavailable independently of the number of objects returned. v0.4 validates these collection paths against disposable three-node kind clusters on Kubernetes v1.34.11 and v1.35.8; no company/production cluster, CSI backend RPC or driver mutation was used.

## Supported

For incoming Pods with bound PVCs pointing to direct `PV.spec.csi` volumes:

- A present, available candidate CSINode must register the PV's driver. A proven absent driver rejects that candidate. Missing/terminating CSINode, duplicated driver entries or an empty NodeID produces UNKNOWN.
- Registered topology keys are compared with candidate Node labels. Missing keys produce UNKNOWN; observed values are evidence. Existing PV node affinity still rejects known topology conflicts.
- A nonnegative `allocatable.count` is compared with observed unique CSI volume handles from VolumeAttachment inventory on the candidate plus incoming handles. Repeated references to one handle count once per driver. Missing, duplicate, unattached, terminating, errored or driver-mismatched attachment evidence makes the usage estimate incomplete; an observed count exceeding the advertised limit rejects placement, while incomplete evidence at or below the limit remains UNKNOWN.
- A nil allocatable object or nil count means an unspecified/unbounded limit in the pinned API. It is not treated as zero. Registration/topology/identity checks still apply, and this does not establish unlimited backend capacity.

Driver names, node IDs and volume handles are not exported as raw Kubernetes objects. Basic redaction includes known CSI driver/volume identifiers and sanitizes capability error details.

## Unknown

Unavailable capability, an absent CSINode in an otherwise available inventory, incomplete registration, absent topology labels, negative limits, missing handles and missing/deleting resident claims or volumes do not establish placement. Resident inline/generic ephemeral and potentially migrated in-tree volumes make the usage estimate incomplete. Incoming inline/generic ephemeral or migration-dependent storage remains UNKNOWN.

Pending/unbound claims, storage capacity/provisioning, future attach/detach completion, inaccessible backends, access-mode transitions, concurrent external changes and feature-gated mutable limits are not simulated. Positive checks concern observed registration, attachment inventory and advertised limits only. **Backend health / future attach-detach success remains UNKNOWN.** ALLOWED never asserts that a mount or migration will succeed. A missing attachment Node or a source Node that conflicts with the assigned Pod also leaves attachment evidence UNKNOWN.

## Optional API behavior

`storage.k8s.io/v1` CSINode and VolumeAttachment use only get/list/watch. Forbidden, missing API, timeout or connection failure does not block core cache startup. Relevant direct CSI/attachment candidate checks become UNKNOWN. Optional watch/relist errors mark only the affected capability unavailable and remain sticky until restart; ordinary retry recovery cannot silently restore positive CSI evidence. Core Namespace failures stop initial synchronization or invalidate collection evidence after startup.

Source shutdown cancels and joins all informers, including optional informers still retrying unavailable APIs. Start is one-shot; a closed Source cannot publish ready or restart, including when Close overlaps initial synchronization. Construct a new Source to collect again. Available with zero CSINode/VolumeAttachment objects differs from a failed collection: absence and failed collection remain distinct capability evidence.

## Limitations

VolumeAttachment inventory is observed but cannot prove that no attachment exists outside the snapshot or that a detach/attach transition will complete safely. Duplicate objects, deletion, `attached=false`, attach/detach errors, orphaned references and multi-node access modes are handled conservatively. No CSIStorageCapacity or CSIDriver collection, driver health probing, attachment mutation or provisioning is implemented. Snapshots are not atomic across resources.

Tests use synthetic objects for present/absent drivers, topology keys, nil/zero/negative/changing limits, duplicate handles, incomplete resident usage, capability failures and refresh changes. The disposable kind matrix additionally validates optional VolumeAttachment denial and GET-only collection on Kubernetes v1.34.11 and v1.35.8. Backend-specific CSI behavior remains unvalidated.

Primary references: pinned [CSINode and VolumeNodeResources API comments](https://github.com/kubernetes/api/blob/v0.35.6/storage/v1/types.go), [CSINode API](https://kubernetes.io/docs/reference/kubernetes-api/storage/csi-node-v1/), and [VolumeAttachment API](https://kubernetes.io/docs/reference/kubernetes-api/storage/volume-attachment-v1/). Current web documentation does not establish a cluster's feature gates or inventory freshness.
