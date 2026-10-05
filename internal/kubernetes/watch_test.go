package kubernetes

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hcie123/guard9s/internal/analyzer"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestWatchErrorsAreSanitizedAndInvalidateDiagnosis(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"expired", apierrors.NewResourceExpired("https://service.example.invalid token=synthetic /private/path"), "expired"},
		{"revoked_rbac", apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "synthetic-private-pod", errors.New("https://service.example.invalid token=synthetic /private/path")), "RBAC Forbidden"},
		{"authentication", apierrors.NewUnauthorized("https://service.example.invalid token=synthetic /private/path"), "authentication"},
		{"server_timeout", apierrors.NewServerTimeout(schema.GroupResource{Resource: "pods"}, "list", 1), "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(demoObjects()...)
			stream := watch.NewRaceFreeFake()
			client.PrependWatchReactor("pods", func(ktesting.Action) (bool, watch.Interface, error) { return true, stream, nil })
			s := NewSource(client, "synthetic", "all")
			defer s.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := s.Start(ctx, time.Second); err != nil {
				t.Fatal(err)
			}
			status := tc.err.(apierrors.APIStatus).Status()
			stream.Error(&status)
			deadline := time.After(time.Second)
			for {
				snapshot, err := s.Snapshot(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if len(snapshot.Issues) > 0 {
					issue := strings.Join(snapshot.Issues, " ")
					if !strings.Contains(issue, tc.want) || strings.Contains(issue, "service.example.invalid") || strings.Contains(issue, "token=") || strings.Contains(issue, "/private/") {
						t.Fatal(issue)
					}
					d := analyzer.Diagnose(ctx, analyzer.Input{Snapshot: snapshot, Node: "worker-01"})
					if d.Capacity.Assessment != "UNKNOWN" {
						t.Fatal("watch failure retained a PASS")
					}
					break
				}
				select {
				case <-deadline:
					t.Fatal("watch error event was lost inside reflector")
				case <-time.After(time.Millisecond):
				}
			}
		})
	}
}
func TestWatchObserverNormalEventsAndOpenErrors(t *testing.T) {
	client := fake.NewSimpleClientset()
	stream := watch.NewRaceFreeFake()
	client.PrependWatchReactor("pods", func(ktesting.Action) (bool, watch.Interface, error) { return true, stream, nil })
	count := 0
	wrapped := observedClient{Interface: client, observe: func(string, error) { count++ }}
	if !wrapped.IsWatchListSemanticsUnSupported() {
		t.Fatal("fake client capability was hidden")
	}
	watcher, err := wrapped.CoreV1().Pods("").Watch(context.Background(), meta.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()
	object := demoObjects()[0]
	stream.Add(object)
	select {
	case event := <-watcher.ResultChan():
		if event.Type != watch.Added || event.Object != object || count != 0 {
			t.Fatal("normal event altered")
		}
	case <-time.After(time.Second):
		t.Fatal("normal event lost")
	}
	for _, err := range []error{context.Canceled, errors.New("https://service.example.invalid token=synthetic")} {
		client := fake.NewSimpleClientset()
		client.PrependWatchReactor("pods", func(ktesting.Action) (bool, watch.Interface, error) { return true, nil, err })
		calls := 0
		_, got := observedClient{Interface: client, observe: func(string, error) { calls++ }}.CoreV1().Pods("").Watch(context.Background(), meta.ListOptions{})
		if got == nil || strings.Contains(got.Error(), "service.example.invalid") {
			t.Fatal(got)
		}
		if (err == context.Canceled && calls != 0) || (err != context.Canceled && calls != 1) {
			t.Fatal("cancellation or failure observation")
		}
	}
	if !strings.Contains(classify(apierrors.NewGone("synthetic")), "expired") || !strings.Contains(classify(context.Canceled), "cancelled") {
		t.Fatal("error classification")
	}
}
func TestRelistFailureKeepsCacheUnknown(t *testing.T) {
	client := fake.NewSimpleClientset(demoObjects()...)
	var failList atomic.Bool
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("synthetic-private-marker"))
	client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		if failList.Load() {
			return true, nil, forbidden
		}
		return false, nil, nil
	})
	stream := watch.NewRaceFreeFake()
	client.PrependWatchReactor("pods", func(ktesting.Action) (bool, watch.Interface, error) { return true, stream, nil })
	s := NewSource(client, "synthetic", "all")
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, time.Second); err != nil {
		t.Fatal(err)
	}
	failList.Store(true)
	expired := apierrors.NewResourceExpired("synthetic-expired-marker").ErrStatus
	stream.Error(&expired)
	deadline := time.After(3 * time.Second)
	for {
		s.mu.RLock()
		issue := s.issues["pods"]
		s.mu.RUnlock()
		if strings.Contains(issue, "Forbidden") {
			break
		}
		select {
		case <-deadline:
			t.Fatal("reflector relist failure was not observed")
		case <-time.After(time.Millisecond):
		}
	}
	snapshot, _ := s.Snapshot(ctx)
	if len(snapshot.Pods) == 0 || len(snapshot.Issues) != 1 || strings.Contains(snapshot.Issues[0], "synthetic-private-marker") {
		t.Fatal("cache recovery error was hidden or leaked")
	}
	if analyzer.Diagnose(ctx, analyzer.Input{Snapshot: snapshot, Node: "worker-01"}).Capacity.Assessment != "UNKNOWN" {
		t.Fatal("relist error appeared safe")
	}
}
