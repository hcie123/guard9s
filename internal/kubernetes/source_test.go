package kubernetes

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hcie123/guard9s/internal/demo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func demoObjects() []runtime.Object {
	s := demo.Snapshot("mixed")
	var objects []runtime.Object
	for _, x := range s.Namespaces {
		objects = append(objects, x)
	}
	for _, x := range s.CSINodes {
		objects = append(objects, x)
	}
	for _, x := range s.Nodes {
		objects = append(objects, x)
	}
	for _, x := range s.Pods {
		objects = append(objects, x)
	}
	for _, x := range s.Deployments {
		objects = append(objects, x)
	}
	for _, x := range s.ReplicaSets {
		objects = append(objects, x)
	}
	for _, x := range s.StatefulSets {
		objects = append(objects, x)
	}
	for _, x := range s.DaemonSets {
		objects = append(objects, x)
	}
	for _, x := range s.Jobs {
		objects = append(objects, x)
	}
	for _, x := range s.CronJobs {
		objects = append(objects, x)
	}
	for _, x := range s.PDBs {
		objects = append(objects, x)
	}
	for _, x := range s.PVCs {
		objects = append(objects, x)
	}
	for _, x := range s.PVs {
		objects = append(objects, x)
	}
	for _, x := range s.StorageClasses {
		objects = append(objects, x)
	}
	for _, x := range s.Events {
		objects = append(objects, x)
	}
	return objects
}
func TestInformerSnapshotReadOnly(t *testing.T) {
	client := fake.NewSimpleClientset(demoObjects()...)
	s := NewSource(client, "demo-cluster", "production")
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := s.Snapshot(ctx); err == nil {
		t.Fatal("unsynced cache accepted")
	}
	if err := s.Start(ctx, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Nodes) != 3 || len(snap.Pods) != 33 || len(snap.Deployments) != 2 || len(snap.ReplicaSets) != 2 || len(snap.StatefulSets) != 1 || len(snap.DaemonSets) != 1 || len(snap.PDBs) != 2 || len(snap.PVCs) != 2 || len(snap.PVs) != 2 || len(snap.Events) != 3 || len(snap.Jobs) != 1 || len(snap.CronJobs) != 1 || len(snap.StorageClasses) != 2 {
		t.Fatalf("snapshot resource counts differ")
	}
	if snap.Namespace != "production" {
		t.Fatal("display filter")
	}
	foundDemo := false
	for _, p := range snap.Pods {
		if p.Namespace == "demo" {
			foundDemo = true
		}
	}
	if !foundDemo {
		t.Fatal("namespace filter removed capacity/availability evidence")
	}
	snap.Nodes[0].Name = "changed-in-snapshot"
	again, _ := s.Snapshot(ctx)
	if again.Nodes[0].Name == "changed-in-snapshot" {
		t.Fatal("cache object mutated")
	}
	count := len(client.Actions())
	for range 3 {
		_, _ = s.Snapshot(ctx)
	}
	if len(client.Actions()) != count {
		t.Fatal("snapshot made API calls")
	}
	for _, a := range client.Actions() {
		if a.GetVerb() != "list" && a.GetVerb() != "watch" {
			t.Fatalf("unexpected Kubernetes action %s", a.GetVerb())
		}
		if a.GetResource().Resource == "secrets" {
			t.Fatal("read secret")
		}
	}
	cancel()
	if _, err := s.Snapshot(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSourceStartIsOneShot(t *testing.T) {
	client := fake.NewSimpleClientset(demoObjects()...)
	s := NewSource(client, "synthetic", "all")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx, 2*time.Second); err == nil || !strings.Contains(err.Error(), "only be called once") {
		t.Fatalf("duplicate Start should fail clearly: %v", err)
	}
	s.Close()
	if err := s.Start(ctx, 2*time.Second); err == nil || !strings.Contains(err.Error(), "only be called once") {
		t.Fatalf("closed Source should require reconstruction: %v", err)
	}
}

// Pause after real informer synchronization, before Start can publish ready.
type pausedSyncInformer struct {
	cache.SharedIndexInformer
	once           sync.Once
	synced, resume chan struct{}
}

func (p *pausedSyncInformer) HasSynced() bool {
	if !p.SharedIndexInformer.HasSynced() {
		return false
	}
	p.once.Do(func() { close(p.synced); <-p.resume })
	return true
}

func TestSourceCloseDuringReadyPublication(t *testing.T) {
	s := NewSource(fake.NewSimpleClientset(demoObjects()...), "synthetic", "all")
	p := &pausedSyncInformer{SharedIndexInformer: s.caches["pods"], synced: make(chan struct{}), resume: make(chan struct{})}
	s.caches["pods"] = p
	done := make(chan error, 1)
	go func() { done <- s.Start(context.Background(), 3*time.Second) }()
	defer s.Close()
	defer close(p.resume)
	select {
	case <-p.synced:
	case <-time.After(3 * time.Second):
		t.Fatal("Pod informer did not synchronize")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		all := true
		for name, inf := range s.caches {
			if name != "pods" && !inf.HasSynced() {
				all = false
			}
		}
		if all {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("remaining informers did not synchronize")
		}
		time.Sleep(time.Millisecond)
	}
	s.Close()
	p.resume <- struct{}{}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed Source republished ready during Start")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not exit after Close")
	}
	if _, err := s.Snapshot(context.Background()); err == nil {
		t.Fatal("closed Source returned a ready snapshot")
	}
}

func TestSourceConcurrentStartAndCloseBeforeStart(t *testing.T) {
	t.Run("concurrent_start", func(t *testing.T) {
		client := fake.NewSimpleClientset(demoObjects()...)
		s := NewSource(client, "synthetic", "all")
		defer s.Close()
		gate := make(chan struct{})
		results := make(chan error, 8)
		for range 8 {
			go func() { <-gate; results <- s.Start(context.Background(), 3*time.Second) }()
		}
		close(gate)
		successes := 0
		for range 8 {
			if err := <-results; err == nil {
				successes++
			}
		}
		if successes != 1 {
			t.Fatalf("started %d worker groups", successes)
		}
		s.Close()
		lists := map[string]int{}
		for _, a := range client.Actions() {
			if a.GetVerb() == "list" {
				lists[a.GetResource().Resource]++
			}
		}
		for name, count := range lists {
			if count != 1 {
				t.Fatalf("%s had %d initial lists", name, count)
			}
		}
		if _, err := s.Snapshot(context.Background()); err == nil {
			t.Fatal("closed Source remained ready")
		}
	})
	t.Run("close_before_start", func(t *testing.T) {
		s := NewSource(fake.NewSimpleClientset(), "synthetic", "all")
		s.Close()
		if err := s.Start(context.Background(), time.Second); err == nil || !strings.Contains(err.Error(), "new Source") {
			t.Fatalf("closed Source must require reconstruction: %v", err)
		}
	})
}

func TestAPIErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"forbidden", apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("sensitive marker")), "RBAC Forbidden"},
		{"unsupported", apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, ""), "unsupported"},
		{"timeout", apierrors.NewTimeoutError("sensitive marker", 1), "timeout"},
		{"connection_refused", errors.New("connection refused sensitive marker"), "connection error"},
		{"unauthorized", apierrors.NewUnauthorized("sensitive marker"), "authentication"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, tc.err })
			s := NewSource(client, "demo", "all")
			defer s.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := s.Start(ctx, time.Second)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "sensitive marker") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
func TestWatchFailureAndTimeout(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependWatchReactor("pods", func(ktesting.Action) (bool, watch.Interface, error) { return true, nil, errors.New("watch refused") })
	s := NewSource(client, "demo", "all")
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = s.Start(ctx, time.Second)
	deadline := time.After(time.Second)
	for {
		s.mu.RLock()
		issue := s.issues["pods"]
		s.mu.RUnlock()
		if issue != "" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("watch failure not recorded")
		case <-time.After(time.Millisecond):
		}
	}
	s2 := NewSource(fake.NewSimpleClientset(), "demo", "all")
	s2.caches = map[string]cache.SharedIndexInformer{"never": cache.NewSharedIndexInformer(&cache.ListWatch{}, nil, 0, cache.Indexers{})}
	if err := s2.Start(ctx, 5*time.Millisecond); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatal(err)
	}
	if err := s2.Start(ctx, 0); err == nil {
		t.Fatal("zero timeout")
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	s3 := NewSource(fake.NewSimpleClientset(), "demo", "all")
	if err := s3.Start(cancelled, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTransportRejectsAllWrites(t *testing.T) {
	var calls atomic.Int32
	rt := readOnlyTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200}, nil
	})}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "CONNECT"} {
		t.Run(method, func(t *testing.T) {
			r, _ := http.NewRequest(method, "https://example.invalid/api", nil)
			if _, err := rt.RoundTrip(r); err == nil {
				t.Fatal("write escaped")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("write reached upstream")
	}
	r, _ := http.NewRequest("GET", "https://example.invalid/api/v1/pods", nil)
	if _, err := rt.RoundTrip(r); err != nil || calls.Load() != 1 {
		t.Fatal(err)
	}
	cfg := Protect(&rest.Config{Host: "https://example.invalid"})
	if cfg.QPS != 10 || cfg.WrapTransport == nil {
		t.Fatal("protection not installed")
	}
}
func TestTransportReadScopeAllowlist(t *testing.T) {
	for _, path := range []string{
		"/api/v1/nodes",
		"/api/v1/namespaces",
		"/api/v1/namespaces/demo",
		"/api/v1/pods",
		"/api/v1/namespaces/demo/pods",
		"/api/v1/namespaces/demo/pods/app",
		"/apis/apps/v1/deployments",
		"/apis/apps/v1/namespaces/demo/replicasets",
		"/apis/batch/v1/namespaces/demo/jobs",
		"/apis/policy/v1/namespaces/demo/poddisruptionbudgets",
		"/apis/storage.k8s.io/v1/volumeattachments",
	} {
		if !allowedReadPath(path) {
			t.Errorf("expected allowed inventory path: %s", path)
		}
	}
	for _, path := range []string{
		"/api",
		"/version",
		"/api/v1/secrets",
		"/api/v1/namespaces/demo/secrets",
		"/api/v1/namespaces/demo/pods/app/log",
		"/api/v1/namespaces/demo/pods/app/status",
		"/api/v1/namespaces/demo/pods/app/exec",
		"/api/v1/namespaces/demo/pods/app/attach",
		"/api/v1/namespaces/demo/pods/app/portforward",
		"/api/v1/namespaces/demo/pods/app/eviction",
		"/apis/apps/v1/namespaces/demo/deployments/app/scale",
		"/apis/apps/v1/namespaces/demo/statefulsets/app/scale",
		"/api/v1/configmaps",
		"/apis/example.invalid/v1/pods",
		"/apis/storage.k8s.io/v1/csidrivers",
	} {
		if allowedReadPath(path) {
			t.Errorf("unexpected out-of-scope read path allowed: %s", path)
		}
	}
	for group, resources := range allowedReadResources {
		prefix := "/apis/" + group + "/v1/"
		if group == "" {
			prefix = "/api/v1/"
		}
		for resource := range resources {
			if !allowedReadPath(prefix + resource) {
				t.Errorf("inventory list/watch rejected: %s%s", prefix, resource)
			}
		}
	}

	calls := 0
	rt := readOnlyTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200}, nil
	})}
	for _, path := range []string{"/api/v1/secrets", "/api/v1/namespaces/demo/pods/app/log"} {
		req, _ := http.NewRequest(http.MethodGet, "https://example.invalid"+path, nil)
		if _, err := rt.RoundTrip(req); err == nil || strings.Contains(err.Error(), path) {
			t.Fatalf("out-of-scope GET was not safely rejected: %s err=%v", path, err)
		}
	}
	if calls != 0 {
		t.Fatal("out-of-scope GET reached upstream transport")
	}
}

func writeConfig(t *testing.T, change func(*clientcmdapi.Config)) string {
	t.Helper()
	c := clientcmdapi.NewConfig()
	c.CurrentContext = "demo"
	c.Contexts["demo"] = &clientcmdapi.Context{Cluster: "demo", AuthInfo: "reader"}
	c.Clusters["demo"] = &clientcmdapi.Cluster{Server: "https://example.invalid"}
	c.AuthInfos["reader"] = &clientcmdapi.AuthInfo{}
	change(c)
	path := filepath.Join(t.TempDir(), "config")
	if err := clientcmd.WriteToFile(*c, path); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestConfigOfflineValidation(t *testing.T) {
	path := writeConfig(t, func(c *clientcmdapi.Config) {})
	cfg, name, err := Config(path, "")
	if err != nil || name != "demo" || cfg.WrapTransport == nil {
		t.Fatal(name, err)
	}
	if _, _, err := Client(path, ""); err != nil {
		t.Fatal(err)
	} // construction only, no API request
	if _, _, err := Config(path, "missing"); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatal(err)
	}
	if _, _, err := Config(filepath.Join(t.TempDir(), "missing"), ""); err == nil {
		t.Fatal("missing file")
	}
	for _, tc := range []struct {
		name   string
		change func(*clientcmdapi.Config)
		want   string
	}{
		{"exec_rejected_before_execution", func(c *clientcmdapi.Config) {
			c.AuthInfos["reader"].Exec = &clientcmdapi.ExecConfig{Command: "must-never-execute"}
		}, "plugins"},
		{"auth_provider", func(c *clientcmdapi.Config) {
			c.AuthInfos["reader"].AuthProvider = &clientcmdapi.AuthProviderConfig{Name: "unsupported"}
		}, "plugins"},
		{"insecure_tls", func(c *clientcmdapi.Config) { c.Clusters["demo"].InsecureSkipTLSVerify = true }, "CA"},
		{"invalid_cluster", func(c *clientcmdapi.Config) { delete(c.Clusters, "demo") }, "invalid kubeconfig"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.change)
			if _, _, err := Config(path, ""); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v", err)
			}
		})
	}
	malformed := filepath.Join(t.TempDir(), "invalid")
	_ = os.WriteFile(malformed, []byte("not: [valid"), 0600)
	if _, _, err := Config(malformed, ""); err == nil {
		t.Fatal("malformed config")
	}
}
