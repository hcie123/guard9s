package kubernetes

import (
	"context"
	"errors"
	"github.com/hcie123/guard9s/internal/model"
	storage "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOptionalVolumeAttachmentCollection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		state model.CapabilityState
	}{
		{"available", nil, model.Available},
		{"forbidden", apierrors.NewForbidden(schema.GroupResource{Resource: "volumeattachments"}, "", errors.New("Bearer fixture-token BEGIN PRIVATE KEY")), model.Forbidden},
		{"unsupported", apierrors.NewNotFound(schema.GroupResource{Resource: "volumeattachments"}, ""), model.Unsupported},
		{"unavailable", errors.New("Authorization: fixture-token"), model.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pv := "fixture-pv"
			object := &storage.VolumeAttachment{ObjectMeta: meta.ObjectMeta{Name: "fixture-attachment"}, Spec: storage.VolumeAttachmentSpec{NodeName: "worker-a", Attacher: "example.csi.io", Source: storage.VolumeAttachmentSource{PersistentVolumeName: &pv}}}
			client := fake.NewSimpleClientset(append(demoObjects(), object)...)
			if tc.err != nil {
				client.PrependReactor("list", "volumeattachments", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, tc.err })
			}
			s := NewSource(client, "synthetic", "all")
			defer s.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := s.Start(ctx, 2*time.Second); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for {
				snap, err := s.Snapshot(ctx)
				if err != nil {
					t.Fatal(err)
				}
				cap := snap.Capability(model.VolumeAttachmentsResource)
				if cap.State == tc.state {
					if len(snap.Issues) != 0 || strings.Contains(cap.Detail, "fixture-token") {
						t.Fatal("optional failure blocked core or leaked credentials")
					}
					if tc.err == nil {
						if !cap.Synced || cap.Failed || len(snap.VolumeAttachments) != 1 {
							t.Fatal("inventory not synced")
						}
						snap.VolumeAttachments[0].Spec.NodeName = "mutated"
						again, _ := s.Snapshot(ctx)
						if again.VolumeAttachments[0].Spec.NodeName != "worker-a" {
							t.Fatal("snapshot mutated informer")
						}
					}
					if tc.err != nil && !cap.Failed {
						t.Fatal("failed capability not recorded")
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("capability did not settle")
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}
func TestVolumeAttachmentWatchRevocationAndShutdown(t *testing.T) {
	client := fake.NewSimpleClientset(demoObjects()...)
	var mu sync.Mutex
	var stream *watch.RaceFreeFakeWatcher
	client.PrependWatchReactor("volumeattachments", func(ktesting.Action) (bool, watch.Interface, error) {
		w := watch.NewRaceFreeFake()
		mu.Lock()
		stream = w
		mu.Unlock()
		return true, w, nil
	})
	s := NewSource(client, "synthetic", "all")
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, time.Second); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	var w *watch.RaceFreeFakeWatcher
	for time.Now().Before(deadline) {
		mu.Lock()
		w = stream
		mu.Unlock()
		if w != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if w == nil {
		t.Fatal("optional watch missing")
	}
	before, _ := s.Snapshot(ctx)
	w.Error(&meta.Status{Status: meta.StatusFailure, Reason: meta.StatusReasonForbidden, Code: 403, Message: "Bearer fixture-token"})
	for {
		after, _ := s.Snapshot(ctx)
		if after.Capability(model.VolumeAttachmentsResource).State == model.Forbidden {
			if !after.Capability(model.VolumeAttachmentsResource).Synced || !after.Capability(model.VolumeAttachmentsResource).Failed || len(after.Issues) != 0 || before.Capability(model.VolumeAttachmentsResource).State != model.Available {
				t.Fatal("stale/inconsistent capability")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("revocation hidden")
		}
		time.Sleep(time.Millisecond)
	}
	s.Close()
	mu.Lock()
	defer mu.Unlock()
	if !stream.IsStopped() {
		t.Fatal("optional worker leaked")
	}
}
