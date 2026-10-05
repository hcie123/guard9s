package kubernetes

import (
	"context"
	"errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	apps "k8s.io/client-go/kubernetes/typed/apps/v1"
	batch "k8s.io/client-go/kubernetes/typed/batch/v1"
	core "k8s.io/client-go/kubernetes/typed/core/v1"
	policy "k8s.io/client-go/kubernetes/typed/policy/v1"
	storage "k8s.io/client-go/kubernetes/typed/storage/v1"
	"k8s.io/client-go/util/watchlist"
)

// Observe watch error events before the reflector handles/retries them or logs
// them. Expired resource versions and revoked access must not leave a stale PASS.
// All resource methods except Watch delegate to the original client; the HTTP
// transport still rejects every write. Namespace is core; CSI node and attachment APIs are optional.
type observedClient struct {
	kubernetes.Interface
	observe func(string, error)
}
type watchResource interface {
	Watch(context.Context, meta.ListOptions) (watch.Interface, error)
}

func (c observedClient) IsWatchListSemanticsUnSupported() bool {
	return watchlist.DoesClientNotSupportWatchListSemantics(c.Interface)
}

func sanitizedStatus(err error) *apierrors.StatusError {
	status := meta.Status{Status: meta.StatusFailure, Reason: meta.StatusReasonUnknown, Code: 500, Message: classify(err)}
	if value, ok := err.(apierrors.APIStatus); ok {
		original := value.Status()
		status.Reason = original.Reason
		status.Code = original.Code
	}
	return &apierrors.StatusError{ErrStatus: status}
}
func observedWatch(ctx context.Context, options meta.ListOptions, resource string, original watchResource, observe func(string, error)) (watch.Interface, error) {
	stream, err := original.Watch(ctx, options)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			observe(resource, err)
			return nil, context.DeadlineExceeded
		}
		if !errors.Is(err, context.Canceled) {
			observe(resource, err)
		}
		return nil, sanitizedStatus(err)
	}
	return watch.Filter(stream, func(event watch.Event) (watch.Event, bool) {
		if event.Type == watch.Error {
			err := apierrors.FromObject(event.Object)
			observe(resource, err)
			event.Object = &sanitizedStatus(err).ErrStatus
		}
		return event, true
	}), nil
}

type observedCoreV1 struct {
	core.CoreV1Interface
	observe func(string, error)
}

func (c observedClient) CoreV1() core.CoreV1Interface {
	return observedCoreV1{c.Interface.CoreV1(), c.observe}
}
func (c observedCoreV1) Nodes() core.NodeInterface {
	return observedNode{c.CoreV1Interface.Nodes(), c.observe}
}

func (c observedCoreV1) Namespaces() core.NamespaceInterface {
	return observedNamespace{c.CoreV1Interface.Namespaces(), c.observe}
}

type observedNamespace struct {
	core.NamespaceInterface
	observe func(string, error)
}

func (c observedNamespace) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "namespaces", c.NamespaceInterface, c.observe)
}

type observedNode struct {
	core.NodeInterface
	observe func(string, error)
}

func (c observedNode) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "nodes", c.NodeInterface, c.observe)
}
func (c observedCoreV1) Pods(ns string) core.PodInterface {
	return observedPod{c.CoreV1Interface.Pods(ns), c.observe}
}

type observedPod struct {
	core.PodInterface
	observe func(string, error)
}

func (c observedPod) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "pods", c.PodInterface, c.observe)
}
func (c observedCoreV1) PersistentVolumeClaims(ns string) core.PersistentVolumeClaimInterface {
	return observedPersistentVolumeClaim{c.CoreV1Interface.PersistentVolumeClaims(ns), c.observe}
}

type observedPersistentVolumeClaim struct {
	core.PersistentVolumeClaimInterface
	observe func(string, error)
}

func (c observedPersistentVolumeClaim) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "persistentvolumeclaims", c.PersistentVolumeClaimInterface, c.observe)
}
func (c observedCoreV1) PersistentVolumes() core.PersistentVolumeInterface {
	return observedPersistentVolume{c.CoreV1Interface.PersistentVolumes(), c.observe}
}

type observedPersistentVolume struct {
	core.PersistentVolumeInterface
	observe func(string, error)
}

func (c observedPersistentVolume) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "persistentvolumes", c.PersistentVolumeInterface, c.observe)
}
func (c observedCoreV1) Events(ns string) core.EventInterface {
	return observedEvent{c.CoreV1Interface.Events(ns), c.observe}
}

type observedEvent struct {
	core.EventInterface
	observe func(string, error)
}

func (c observedEvent) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "events", c.EventInterface, c.observe)
}

type observedAppsV1 struct {
	apps.AppsV1Interface
	observe func(string, error)
}

func (c observedClient) AppsV1() apps.AppsV1Interface {
	return observedAppsV1{c.Interface.AppsV1(), c.observe}
}
func (c observedAppsV1) Deployments(ns string) apps.DeploymentInterface {
	return observedDeployment{c.AppsV1Interface.Deployments(ns), c.observe}
}

type observedDeployment struct {
	apps.DeploymentInterface
	observe func(string, error)
}

func (c observedDeployment) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "deployments", c.DeploymentInterface, c.observe)
}
func (c observedAppsV1) ReplicaSets(ns string) apps.ReplicaSetInterface {
	return observedReplicaSet{c.AppsV1Interface.ReplicaSets(ns), c.observe}
}

type observedReplicaSet struct {
	apps.ReplicaSetInterface
	observe func(string, error)
}

func (c observedReplicaSet) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "replicasets", c.ReplicaSetInterface, c.observe)
}
func (c observedAppsV1) StatefulSets(ns string) apps.StatefulSetInterface {
	return observedStatefulSet{c.AppsV1Interface.StatefulSets(ns), c.observe}
}

type observedStatefulSet struct {
	apps.StatefulSetInterface
	observe func(string, error)
}

func (c observedStatefulSet) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "statefulsets", c.StatefulSetInterface, c.observe)
}
func (c observedAppsV1) DaemonSets(ns string) apps.DaemonSetInterface {
	return observedDaemonSet{c.AppsV1Interface.DaemonSets(ns), c.observe}
}

type observedDaemonSet struct {
	apps.DaemonSetInterface
	observe func(string, error)
}

func (c observedDaemonSet) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "daemonsets", c.DaemonSetInterface, c.observe)
}

type observedBatchV1 struct {
	batch.BatchV1Interface
	observe func(string, error)
}

func (c observedClient) BatchV1() batch.BatchV1Interface {
	return observedBatchV1{c.Interface.BatchV1(), c.observe}
}
func (c observedBatchV1) Jobs(ns string) batch.JobInterface {
	return observedJob{c.BatchV1Interface.Jobs(ns), c.observe}
}

type observedJob struct {
	batch.JobInterface
	observe func(string, error)
}

func (c observedJob) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "jobs", c.JobInterface, c.observe)
}
func (c observedBatchV1) CronJobs(ns string) batch.CronJobInterface {
	return observedCronJob{c.BatchV1Interface.CronJobs(ns), c.observe}
}

type observedCronJob struct {
	batch.CronJobInterface
	observe func(string, error)
}

func (c observedCronJob) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "cronjobs", c.CronJobInterface, c.observe)
}

type observedPolicyV1 struct {
	policy.PolicyV1Interface
	observe func(string, error)
}

func (c observedClient) PolicyV1() policy.PolicyV1Interface {
	return observedPolicyV1{c.Interface.PolicyV1(), c.observe}
}
func (c observedPolicyV1) PodDisruptionBudgets(ns string) policy.PodDisruptionBudgetInterface {
	return observedPodDisruptionBudget{c.PolicyV1Interface.PodDisruptionBudgets(ns), c.observe}
}

type observedPodDisruptionBudget struct {
	policy.PodDisruptionBudgetInterface
	observe func(string, error)
}

func (c observedPodDisruptionBudget) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "poddisruptionbudgets", c.PodDisruptionBudgetInterface, c.observe)
}

type observedStorageV1 struct {
	storage.StorageV1Interface
	observe func(string, error)
}

func (c observedClient) StorageV1() storage.StorageV1Interface {
	return observedStorageV1{c.Interface.StorageV1(), c.observe}
}
func (c observedStorageV1) StorageClasses() storage.StorageClassInterface {
	return observedStorageClass{c.StorageV1Interface.StorageClasses(), c.observe}
}

func (c observedStorageV1) CSINodes() storage.CSINodeInterface {
	return observedCSINode{c.StorageV1Interface.CSINodes(), c.observe}
}

type observedCSINode struct {
	storage.CSINodeInterface
	observe func(string, error)
}

func (c observedCSINode) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "csinodes", c.CSINodeInterface, c.observe)
}

type observedStorageClass struct {
	storage.StorageClassInterface
	observe func(string, error)
}

func (c observedStorageClass) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "storageclasses", c.StorageClassInterface, c.observe)
}

func (c observedStorageV1) VolumeAttachments() storage.VolumeAttachmentInterface {
	return observedVolumeAttachment{c.StorageV1Interface.VolumeAttachments(), c.observe}
}

type observedVolumeAttachment struct {
	storage.VolumeAttachmentInterface
	observe func(string, error)
}

func (c observedVolumeAttachment) Watch(ctx context.Context, options meta.ListOptions) (watch.Interface, error) {
	return observedWatch(ctx, options, "volumeattachments", c.VolumeAttachmentInterface, c.observe)
}
