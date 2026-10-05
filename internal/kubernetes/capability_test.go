package kubernetes

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hcie123/guard9s/internal/model"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestOptionalCSINodeFailureDoesNotBlockCoreStartup(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		state model.CapabilityState
	}{
		{"forbidden", apierrors.NewForbidden(schema.GroupResource{Resource: "csinodes"}, "", errors.New("sensitive-marker")), model.Forbidden},
		{"unsupported", apierrors.NewNotFound(schema.GroupResource{Resource: "csinodes"}, ""), model.Unsupported},
		{"unavailable", errors.New("sensitive-marker connection refused"), model.Unavailable},
		{"timeout", apierrors.NewTimeoutError("sensitive-marker", 1), model.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(demoObjects()...)
			client.PrependReactor("list", "csinodes", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, tc.err })
			s := NewSource(client, "synthetic", "all")
			defer s.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := s.Start(ctx, 2*time.Second); err != nil {
				t.Fatalf("optional API blocked startup: %v", err)
			}
			snap, err := s.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if snap.Capability(model.CSINodesResource).State != tc.state || snap.Capability(model.NamespacesResource).State != model.Available || len(snap.Namespaces) != 3 || len(snap.Issues) != 0 {
				t.Fatalf("%+v issues=%v", snap.Capabilities, snap.Issues)
			}
			if strings.Contains(snap.Capability(model.CSINodesResource).Detail, "sensitive-marker") {
				t.Fatal("raw error escaped")
			}
			if snap.Capability(model.VolumeAttachmentsResource).State != model.Available {
				t.Fatal("attachment inventory did not sync")
			}
			for _, a := range client.Actions() {
				if a.GetVerb() != "list" && a.GetVerb() != "watch" {
					t.Fatal(a.GetVerb())
				}
			}
		})
	}
}

func TestNamespaceCollectionFailureIsCore(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "namespaces", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "", errors.New("synthetic-marker"))
	})
	s := NewSource(client, "synthetic", "all")
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, time.Second); err == nil || !strings.Contains(err.Error(), "namespaces") || !strings.Contains(err.Error(), "Forbidden") {
		t.Fatalf("%v", err)
	}
	if _, err := s.Snapshot(ctx); err == nil {
		t.Fatal("unsynchronized core source accepted")
	}
}

func TestCapabilityWatchFailureAndInformerShutdown(t *testing.T) {
	client := fake.NewSimpleClientset(demoObjects()...)
	var mu sync.Mutex
	streams := map[string]*watch.RaceFreeFakeWatcher{}
	client.PrependWatchReactor("*", func(a ktesting.Action) (bool, watch.Interface, error) {
		w := watch.NewRaceFreeFake()
		mu.Lock()
		streams[a.GetResource().Resource] = w
		mu.Unlock()
		return true, w, nil
	})
	s := NewSource(client, "synthetic", "all")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer s.Close()
	if err := s.Start(ctx, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var namespaceWatch, csiWatch *watch.RaceFreeFakeWatcher
	for time.Now().Before(deadline) {
		mu.Lock()
		namespaceWatch, csiWatch = streams["namespaces"], streams["csinodes"]
		mu.Unlock()
		if namespaceWatch != nil && csiWatch != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if namespaceWatch == nil || csiWatch == nil {
		t.Fatal("new resource watches did not start")
	}
	before, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	csiWatch.Error(&meta.Status{Status: meta.StatusFailure, Reason: meta.StatusReasonForbidden, Code: 403, Message: "sensitive-marker"})
	namespaceWatch.Error(&meta.Status{Status: meta.StatusFailure, Reason: meta.StatusReasonForbidden, Code: 403, Message: "sensitive-marker"})
	for time.Now().Before(deadline) {
		after, err := s.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if after.Capability(model.NamespacesResource).State == model.Forbidden && after.Capability(model.CSINodesResource).State == model.Forbidden {
			if len(after.Issues) == 0 || before.Capability(model.NamespacesResource).State != model.Available || before.Capability(model.CSINodesResource).State != model.Available {
				t.Fatal("old capability map mutated or failure lost")
			}
			break
		}
		time.Sleep(time.Millisecond)
	}
	after, _ := s.Snapshot(ctx)
	if after.Capability(model.NamespacesResource).State != model.Forbidden || after.Capability(model.CSINodesResource).State != model.Forbidden {
		t.Fatal("stale capability remained Available")
	}
	s.Close()
	mu.Lock()
	defer mu.Unlock()
	for resource, w := range streams {
		if !w.IsStopped() {
			t.Fatalf("%s informer watch leaked after Close", resource)
		}
	}
	if _, err := s.Snapshot(ctx); err == nil {
		t.Fatal("closed source remained ready")
	}
}
