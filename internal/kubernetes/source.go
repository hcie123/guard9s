package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/hcie123/guard9s/internal/model"
	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	storage "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type Source struct {
	KubeconfigPath         string // Set before Start; included only in command suggestions.
	factory                informers.SharedInformerFactory
	caches                 map[string]cache.SharedIndexInformer
	contextName, namespace string
	mu                     sync.RWMutex
	issues                 map[string]string
	capabilities           map[string]model.Capability
	ready                  bool
	startCalled            bool
	closed                 bool
	stop                   context.CancelFunc
}

func NewSource(client kubernetes.Interface, contextName, namespace string) *Source {
	s := &Source{contextName: contextName, namespace: namespace, issues: map[string]string{}, capabilities: map[string]model.Capability{}}
	client = observedClient{Interface: client, observe: s.recordIssue}
	f := informers.NewSharedInformerFactory(client, 0) // Watches; no periodic full resync.
	s.factory = f
	s.caches = map[string]cache.SharedIndexInformer{
		"nodes": f.Core().V1().Nodes().Informer(), "pods": f.Core().V1().Pods().Informer(),
		"namespaces":        f.Core().V1().Namespaces().Informer(),
		"csinodes":          f.Storage().V1().CSINodes().Informer(),
		"volumeattachments": f.Storage().V1().VolumeAttachments().Informer(),
		"deployments":       f.Apps().V1().Deployments().Informer(), "replicasets": f.Apps().V1().ReplicaSets().Informer(),
		"statefulsets": f.Apps().V1().StatefulSets().Informer(), "daemonsets": f.Apps().V1().DaemonSets().Informer(),
		"jobs": f.Batch().V1().Jobs().Informer(), "cronjobs": f.Batch().V1().CronJobs().Informer(),
		"poddisruptionbudgets":   f.Policy().V1().PodDisruptionBudgets().Informer(),
		"persistentvolumeclaims": f.Core().V1().PersistentVolumeClaims().Informer(), "persistentvolumes": f.Core().V1().PersistentVolumes().Informer(),
		"storageclasses": f.Storage().V1().StorageClasses().Informer(), "events": f.Core().V1().Events().Informer(),
	}
	return s
}
func (s *Source) recordIssue(resource string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := model.Unavailable
	if apierrors.IsForbidden(err) {
		state = model.Forbidden
	}
	if apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err) {
		state = model.Unsupported
	}
	s.capabilities[resource] = model.Capability{State: state, Detail: classify(err), Failed: true, Synced: s.caches[resource] != nil && s.caches[resource].HasSynced()}
	if !model.OptionalResource(resource) {
		s.issues[resource] = resource + ": " + classify(err)
	}
}
func classify(err error) string {
	switch {
	case apierrors.IsResourceExpired(err) || apierrors.IsGone(err):
		return "watch resource version expired; relist/restart and revalidate evidence"
	case apierrors.IsForbidden(err):
		return "RBAC Forbidden; grant get/list/watch for this resource"
	case apierrors.IsUnauthorized(err):
		return "authentication failed; refresh read-only credentials"
	case apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err):
		return "resource API is unsupported or unavailable"
	case apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || errors.Is(err, context.DeadlineExceeded):
		return "API timeout"
	case errors.Is(err, context.Canceled):
		return "context cancelled"
	default:
		return "API or connection error; verify API reachability and credentials"
	}
}
func (s *Source) Start(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("cache timeout must be positive")
	}
	// SharedInformerFactory is a one-shot lifecycle. Reject duplicate/concurrent
	// Start calls rather than losing the original cancel function and leaking
	// informer workers. Publish the cancel function atomically with the one-shot
	// state so a concurrent Close cannot miss an in-progress Start.
	runCtx, stop := context.WithCancel(ctx)
	s.mu.Lock()
	if s.startCalled || s.closed {
		s.mu.Unlock()
		stop()
		return errors.New("Kubernetes source Start may only be called once; create a new Source after Close")
	}
	s.startCalled = true
	s.stop = stop
	s.mu.Unlock()
	started := false
	defer func() {
		if !started {
			s.Close()
		}
	}()
	watchErrors := make(chan error, 1)
	var synced []cache.InformerSynced
	for name, inf := range s.caches {
		if !model.OptionalResource(name) {
			synced = append(synced, inf.HasSynced)
		}
		err := inf.SetWatchErrorHandlerWithContext(func(_ context.Context, _ *cache.Reflector, err error) {
			// Do not log raw API errors, URLs or credential details. Watch failures
			// remain visible until a fresh startup verifies the entire cache again.
			message := name + ": " + classify(err)
			s.recordIssue(name, err)
			if model.OptionalResource(name) {
				return
			}
			select {
			case watchErrors <- errors.New(message):
			default:
			}
		})
		if err != nil {
			return fmt.Errorf("could not register %s watch handler", name)
		}
	}
	s.factory.Start(runCtx.Done())
	syncCtx, cancel := context.WithTimeout(runCtx, timeout)
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- cache.WaitForCacheSync(syncCtx.Done(), synced...) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-watchErrors:
		return err
	case ok := <-done:
		if !ok {
			return errors.New("cache synchronization timed out; check API connectivity and get/list/watch permissions")
		}
		s.mu.Lock()
		if s.closed || runCtx.Err() != nil {
			s.mu.Unlock()
			return errors.New("Kubernetes source closed or cancelled during startup; create a new Source")
		}
		s.ready = true
		s.mu.Unlock()
		started = true
		return nil
	}
}

// Close cancels and joins all core/optional informer workers, including startup
// failures. Cancellation alone must not leave an optional API retry in flight.
func (s *Source) Close() {
	s.mu.Lock()
	stop := s.stop
	s.stop = nil
	s.closed = true
	s.ready = false
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	s.factory.Shutdown()
}
func (s *Source) Snapshot(ctx context.Context) (model.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	s.mu.RLock()
	ready := s.ready
	var issues []string
	capabilities := map[string]model.Capability{}
	for name, capability := range s.capabilities {
		capabilities[name] = capability
	}
	for _, message := range s.issues {
		issues = append(issues, message)
	}
	s.mu.RUnlock()
	if !ready {
		return model.Snapshot{}, errors.New("Kubernetes cache is not synchronized")
	}
	sort.Strings(issues)
	for name, inf := range s.caches {
		if _, failed := capabilities[name]; !failed {
			state := model.Unavailable
			if inf.HasSynced() {
				state = model.Available
			}
			capabilities[name] = model.Capability{State: state, Synced: inf.HasSynced()}
		}
	}
	out := model.Snapshot{KubeconfigPath: s.KubeconfigPath, Context: s.contextName, Namespace: s.namespace, At: time.Now().UTC(), Issues: issues, Capabilities: capabilities}
	// Informer objects are read-only. Deep copies keep each analysis snapshot
	// isolated from callers and test fixtures. This is not a cross-resource transaction.
	for _, inf := range s.caches {
		for _, obj := range inf.GetStore().List() {
			if err := ctx.Err(); err != nil {
				return model.Snapshot{}, err
			}
			switch x := obj.(type) {
			case *core.Namespace:
				out.Namespaces = append(out.Namespaces, x.DeepCopy())
			case *storage.CSINode:
				out.CSINodes = append(out.CSINodes, x.DeepCopy())
			case *storage.VolumeAttachment:
				out.VolumeAttachments = append(out.VolumeAttachments, x.DeepCopy())
			case *core.Node:
				out.Nodes = append(out.Nodes, x.DeepCopy())
			case *core.Pod:
				out.Pods = append(out.Pods, x.DeepCopy())
			case *apps.Deployment:
				out.Deployments = append(out.Deployments, x.DeepCopy())
			case *apps.ReplicaSet:
				out.ReplicaSets = append(out.ReplicaSets, x.DeepCopy())
			case *apps.StatefulSet:
				out.StatefulSets = append(out.StatefulSets, x.DeepCopy())
			case *apps.DaemonSet:
				out.DaemonSets = append(out.DaemonSets, x.DeepCopy())
			case *batch.Job:
				out.Jobs = append(out.Jobs, x.DeepCopy())
			case *batch.CronJob:
				out.CronJobs = append(out.CronJobs, x.DeepCopy())
			case *policy.PodDisruptionBudget:
				out.PDBs = append(out.PDBs, x.DeepCopy())
			case *core.PersistentVolumeClaim:
				out.PVCs = append(out.PVCs, x.DeepCopy())
			case *core.PersistentVolume:
				out.PVs = append(out.PVs, x.DeepCopy())
			case *storage.StorageClass:
				out.StorageClasses = append(out.StorageClasses, x.DeepCopy())
			case *core.Event:
				out.Events = append(out.Events, x.DeepCopy())
			}
		}
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].Name < out.Nodes[j].Name })
	sort.Slice(out.Namespaces, func(i, j int) bool { return out.Namespaces[i].Name < out.Namespaces[j].Name })
	sort.Slice(out.CSINodes, func(i, j int) bool { return out.CSINodes[i].Name < out.CSINodes[j].Name })
	sort.Slice(out.VolumeAttachments, func(i, j int) bool { return out.VolumeAttachments[i].Name < out.VolumeAttachments[j].Name })
	sort.Slice(out.Pods, func(i, j int) bool {
		return out.Pods[i].Namespace+"/"+out.Pods[i].Name < out.Pods[j].Namespace+"/"+out.Pods[j].Name
	})
	return out, nil
}
