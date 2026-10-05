//go:build e2e

// Package e2e is a writable test harness for a cluster created by kind-e2e.sh.
// It is excluded from ordinary tests and the production binary.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hcie123/guard9s/internal/analyzer"
	"github.com/hcie123/guard9s/internal/app"
	collector "github.com/hcie123/guard9s/internal/kubernetes"
	"github.com/hcie123/guard9s/internal/model"
	"github.com/hcie123/guard9s/internal/report"
	authentication "k8s.io/api/authentication/v1"
	authorization "k8s.io/api/authorization/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	storage "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	yaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientapi "k8s.io/client-go/tools/clientcmd/api"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if status, ok := err.(apierrors.APIStatus); ok {
		s := status.Status()
		t.Fatalf("fixture/API operation failed (%T): reason=%s code=%d; raw message suppressed", err, s.Reason, s.Code)
	}
	t.Fatalf("fixture/API operation failed (%T); raw credential-bearing errors suppressed", err)
}
func eventually(t *testing.T, ctx context.Context, description string, check func() bool) {
	t.Helper()
	limit, stop := context.WithTimeout(ctx, 90*time.Second)
	defer stop()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		if check() {
			return
		}
		select {
		case <-limit.Done():
			t.Fatal("timed out: " + description)
		case <-tick.C:
		}
	}
}
func p[T any](v T) *T { return &v }

type methodAudit struct {
	base          http.RoundTripper
	reads, writes *atomic.Int32
}

func (a methodAudit) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet {
		a.reads.Add(1)
	} else {
		a.writes.Add(1)
	}
	return a.base.RoundTrip(r)
}

func (a methodAudit) WrappedRoundTripper() http.RoundTripper { return a.base }

func installYAML(t *testing.T, ctx context.Context, c *rest.Config, path, a, b string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	must(t, err)
	text := strings.ReplaceAll(strings.ReplaceAll(string(raw), "WORKER_A", a), "WORKER_B", b)
	client, err := dynamic.NewForConfig(c)
	must(t, err)
	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(text), 4096)
	resources := map[string]string{"Namespace": "namespaces", "Pod": "pods", "Event": "events", "PersistentVolume": "persistentvolumes", "PersistentVolumeClaim": "persistentvolumeclaims", "Deployment": "deployments", "DaemonSet": "daemonsets", "StatefulSet": "statefulsets", "PodDisruptionBudget": "poddisruptionbudgets", "ClusterRole": "clusterroles", "ClusterRoleBinding": "clusterrolebindings"}
	for {
		var obj unstructured.Unstructured
		err = dec.Decode(&obj)
		if err == io.EOF {
			return
		}
		must(t, err)
		if obj.Object == nil {
			continue
		}
		gv, err := schema.ParseGroupVersion(obj.GetAPIVersion())
		must(t, err)
		name, ok := resources[obj.GetKind()]
		if !ok {
			t.Fatal("unreviewed fixture kind")
		}
		r := client.Resource(gv.WithResource(name))
		if obj.GetNamespace() == "" {
			_, err = r.Create(ctx, &obj, meta.CreateOptions{})
		} else {
			_, err = r.Namespace(obj.GetNamespace()).Create(ctx, &obj, meta.CreateOptions{})
		}
		must(t, err)
	}
}

func TestKindIntegration(t *testing.T) {
	if os.Getenv("GUARD9S_KIND_E2E") != "1" {
		t.Skip("explicit disposable kind opt-in required")
	}
	cluster := os.Getenv("GUARD9S_E2E_CLUSTER")
	path := os.Getenv("GUARD9S_E2E_ADMIN_CONFIG")
	if !strings.HasPrefix(cluster, "guard9s-e2e-") || path == "" {
		t.Fatal("only harness-owned kind configurations are accepted")
	}
	raw, err := clientcmd.LoadFromFile(path)
	must(t, err)
	if raw.CurrentContext != "kind-"+cluster {
		t.Fatal("context is not the disposable kind cluster")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	must(t, err)
	endpoint, err := url.Parse(cfg.Host)
	must(t, err)
	if endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost" {
		t.Fatal("kind API must be loopback")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	admin, err := kubernetes.NewForConfig(cfg)
	must(t, err)
	nodes, err := admin.CoreV1().Nodes().List(ctx, meta.ListOptions{})
	must(t, err)
	if len(nodes.Items) != 3 {
		t.Fatal("expected three-node topology")
	}
	workers := map[string]string{}
	for _, n := range nodes.Items {
		if w := n.Labels["guard9s.io/worker"]; w != "" {
			workers[w] = n.Name
		}
	}
	a, b := workers["worker-a"], workers["worker-b"]
	if a == "" || b == "" {
		t.Fatal("worker fixture labels missing")
	}
	installYAML(t, ctx, cfg, "../../testdata/e2e/fixtures.yaml", a, b)
	installYAML(t, ctx, cfg, "../../deploy/rbac.yaml", a, b)
	_, err = admin.CoreV1().ServiceAccounts("demo-a").Create(ctx, &core.ServiceAccount{ObjectMeta: meta.ObjectMeta{Name: "guard9s-reader", Namespace: "demo-a"}, AutomountServiceAccountToken: p(false)}, meta.CreateOptions{})
	must(t, err)
	binding, err := admin.RbacV1().ClusterRoleBindings().Get(ctx, "guard9s-readers", meta.GetOptions{})
	must(t, err)
	binding.Subjects = []rbac.Subject{{Kind: "ServiceAccount", Name: "guard9s-reader", Namespace: "demo-a"}}
	_, err = admin.RbacV1().ClusterRoleBindings().Update(ctx, binding, meta.UpdateOptions{})
	must(t, err)
	token, err := admin.CoreV1().ServiceAccounts("demo-a").CreateToken(ctx, "guard9s-reader", &authentication.TokenRequest{Spec: authentication.TokenRequestSpec{ExpirationSeconds: p(int64(600))}}, meta.CreateOptions{})
	must(t, err)
	readerCfg := &rest.Config{Host: cfg.Host, TLSClientConfig: rest.TLSClientConfig{CAData: cfg.CAData, CAFile: cfg.CAFile}, BearerToken: token.Status.Token, Timeout: 15 * time.Second}
	// Authorization review is a harness operation. The production transport still
	// forbids its POST, even though Kubernetes permits self-review by basic users.
	reviewer, err := kubernetes.NewForConfig(readerCfg)
	must(t, err)
	var reads, writes atomic.Int32
	protected := collector.Protect(readerCfg)
	protected.Wrap(func(rt http.RoundTripper) http.RoundTripper { return methodAudit{rt, &reads, &writes} })
	reader, err := kubernetes.NewForConfig(protected)
	must(t, err)
	source := collector.NewSource(reader, "guard9s-kind", "all")
	defer source.Close()
	must(t, source.Start(ctx, 30*time.Second))
	snapshot := func() model.Snapshot { s, err := source.Snapshot(ctx); must(t, err); return s }
	var initial model.Snapshot
	eventually(t, ctx, "controllers, PDB, PVC and optional caches", func() bool {
		initial = snapshot()
		d, err := admin.AppsV1().Deployments("demo-a").Get(ctx, "web", meta.GetOptions{})
		if err != nil || d.Status.ReadyReplicas != 2 {
			return false
		}
		for _, budget := range initial.PDBs {
			if budget.Name == "web" && budget.Status.CurrentHealthy == 2 && budget.Status.DisruptionsAllowed == 0 && budget.Status.ObservedGeneration == budget.Generation {
				return len(initial.Deployments) >= 2 && len(initial.StatefulSets) >= 1 && len(initial.DaemonSets) >= 1 && len(initial.PVCs) == 1 && initial.PVCs[0].Status.Phase == core.ClaimBound && initial.Capability(model.CSINodesResource).Usable() && initial.Capability(model.VolumeAttachmentsResource).Usable()
			}
		}
		return false
	})
	version, err := reviewer.Discovery().ServerVersion()
	must(t, err)
	t.Log("Kubernetes server:", version.GitVersion)
	t.Run("DiscoveryListWatchInitialSync", func(t *testing.T) {
		// Discovery is harness metadata, not part of guard9s inventory collection.
		// Use the same read-only ServiceAccount without the production inventory
		// transport allowlist; the protected reader below must stay resource-scoped.
		_, err = reviewer.Discovery().ServerResourcesForGroupVersion("v1")
		must(t, err)
		list, err := reader.CoreV1().Pods("demo-a").List(ctx, meta.ListOptions{LabelSelector: "app=web"})
		must(t, err)
		if len(list.Items) != 2 || list.ResourceVersion == "" {
			t.Fatal("real list/selector/resourceVersion not observed")
		}
		for _, resource := range []string{model.NamespacesResource, model.CSINodesResource, model.VolumeAttachmentsResource} {
			if !initial.Capability(resource).Synced {
				t.Fatal("cache sync flag absent", resource)
			}
		}
	})
	t.Run("ReadOnlyRBAC", func(t *testing.T) {
		cases := []struct {
			verb, group, resource, sub string
			allowed                    bool
		}{{"get", "", "pods", "", true}, {"list", "", "pods", "", true}, {"watch", "", "pods", "", true}, {"create", "", "pods", "", false}, {"update", "", "pods", "", false}, {"patch", "", "nodes", "", false}, {"delete", "", "pods", "", false}, {"create", "", "pods", "eviction", false}}
		for _, tc := range cases {
			t.Run(tc.verb+"/"+tc.resource+"/"+tc.sub, func(t *testing.T) {
				review, err := reviewer.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorization.SelfSubjectAccessReview{Spec: authorization.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorization.ResourceAttributes{Namespace: "demo-a", Verb: tc.verb, Group: tc.group, Resource: tc.resource, Subresource: tc.sub}}}, meta.CreateOptions{})
				must(t, err)
				if review.Status.Allowed != tc.allowed {
					t.Fatal("authorization mismatch")
				}
			})
		}
	})
	t.Run("TransportGuardWithAdminCredentials", func(t *testing.T) {
		protectedAdmin, err := kubernetes.NewForConfig(collector.Protect(cfg))
		must(t, err)
		_, err = protectedAdmin.CoreV1().Pods("demo-a").Create(ctx, &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "must-not-exist"}}, meta.CreateOptions{})
		if err == nil || !strings.Contains(err.Error(), "read-only transport") {
			t.Fatal("POST was not stopped before API")
		}
		_, err = protectedAdmin.CoreV1().Secrets("demo-a").List(ctx, meta.ListOptions{})
		if err == nil || !strings.Contains(err.Error(), "out-of-scope resource") {
			t.Fatal("Secret GET was not stopped before API")
		}
		if writes.Load() != 0 || reads.Load() == 0 {
			t.Fatal("collector used non-GET transport")
		}
	})
	findPod := func(s model.Snapshot, name string) *core.Pod {
		for _, pod := range s.Pods {
			if pod.Namespace == "demo-a" && pod.Name == name {
				return pod
			}
		}
		t.Fatal("fixture pod missing", name)
		return nil
	}
	status := func(f analyzer.Feasibility, node string) string {
		for _, e := range f.Evaluations {
			if e.Node == node {
				return e.Status
			}
		}
		t.Fatal("candidate missing")
		return ""
	}
	t.Run("AffinityAntiAffinityNamespaceSelectorTopology", func(t *testing.T) {
		s := snapshot()
		affinity := analyzer.Candidates(s, findPod(s, "affinity"), a)
		if status(affinity, b) != "ALLOWED" {
			t.Fatal("NamespaceSelector affinity evidence incorrect", affinity)
		}
		if status(analyzer.Candidates(s, findPod(s, "anti-affinity"), a), b) != "REJECTED" {
			t.Fatal("cross-namespace anti-affinity conflict missed")
		}
		if status(analyzer.Candidates(s, findPod(s, "spread"), a), b) != "ALLOWED" {
			t.Fatal("hard spread not checked")
		}
		ns, err := admin.CoreV1().Namespaces().Get(ctx, "demo-b", meta.GetOptions{})
		must(t, err)
		oldRV := ns.ResourceVersion
		ns.Labels["guard9s.io/team"] = "c"
		_, err = admin.CoreV1().Namespaces().Update(ctx, ns, meta.UpdateOptions{})
		must(t, err)
		eventually(t, ctx, "NamespaceSelector watch update", func() bool {
			latest := snapshot()
			n := latest.NamespaceObject("demo-b")
			return n != nil && n.ResourceVersion != oldRV && status(analyzer.Candidates(latest, findPod(latest, "affinity"), a), b) == "REJECTED"
		})
		if status(analyzer.Candidates(s, findPod(s, "affinity"), a), b) != "ALLOWED" {
			t.Fatal("old snapshot mutated")
		}
	})
	t.Run("PDBBlockedAllowedInformerRefresh", func(t *testing.T) {
		before := snapshot()
		oldRV := ""
		for _, budget := range before.PDBs {
			if budget.Name == "web" {
				oldRV = budget.ResourceVersion
				if level, _ := analyzer.PDBAssessment(budget); level != model.Critical {
					t.Fatal("PDB not blocked")
				}
			}
		}
		budget, err := admin.PolicyV1().PodDisruptionBudgets("demo-a").Get(ctx, "web", meta.GetOptions{})
		must(t, err)
		budget.Spec.MinAvailable = p(intstr.FromInt32(1))
		_, err = admin.PolicyV1().PodDisruptionBudgets("demo-a").Update(ctx, budget, meta.UpdateOptions{})
		must(t, err)
		eventually(t, ctx, "PDB real controller + informer refresh", func() bool {
			for _, v := range snapshot().PDBs {
				if v.Name == "web" && v.ResourceVersion != oldRV {
					severity, stale := analyzer.PDBAssessment(v)
					return severity == model.Pass && !stale && v.Status.DisruptionsAllowed == 1
				}
			}
			return false
		})
		for _, v := range before.PDBs {
			if v.Name == "web" && v.Status.DisruptionsAllowed != 0 {
				t.Fatal("old snapshot mutated")
			}
		}
	})
	t.Run("WatchResourceVersionUpdateDeleteRestart", func(t *testing.T) {
		list, err := reader.CoreV1().Pods("demo-a").List(ctx, meta.ListOptions{})
		must(t, err)
		w, err := reader.CoreV1().Pods("demo-a").Watch(ctx, meta.ListOptions{ResourceVersion: list.ResourceVersion, TimeoutSeconds: p(int64(1))})
		must(t, err)
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		closed := false
		for !closed {
			select {
			case _, ok := <-w.ResultChan():
				closed = !ok
			case <-deadline.C:
				t.Fatal("normal watch timeout did not close")
			}
		}
		w.Stop()
		restarted, err := reader.CoreV1().Pods("demo-a").Watch(ctx, meta.ListOptions{ResourceVersion: list.ResourceVersion, TimeoutSeconds: p(int64(10))})
		must(t, err)
		defer restarted.Stop()
		// Build a fresh probe Pod instead of cloning a live informer object. A live
		// Pod may contain admission-injected projected volumes/volumeMounts; copying
		// only part of that object can create an invalid fixture and test the API
		// defaulting details instead of watch restart behavior.
		pod := &core.Pod{
			ObjectMeta: meta.ObjectMeta{Name: "watch-probe", Namespace: "demo-a"},
			Spec: core.PodSpec{
				NodeName: a,
				Containers: []core.Container{{
					Name:  "probe",
					Image: "registry.k8s.io/pause:3.10.1",
				}},
			},
		}
		created, err := admin.CoreV1().Pods("demo-a").Create(ctx, pod, meta.CreateOptions{})
		must(t, err)
		eventually(t, ctx, "informer ADDED", func() bool {
			for _, p := range snapshot().Pods {
				if p.Name == created.Name && p.Namespace == "demo-a" {
					return p.ResourceVersion != ""
				}
			}
			return false
		})
		observed := false
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for !observed {
			select {
			case ev, ok := <-restarted.ResultChan():
				if !ok {
					t.Fatal("restarted watch closed before update")
				}
				if p, ok := ev.Object.(*core.Pod); ok && p.Name == created.Name {
					observed = true
				}
			case <-timer.C:
				t.Fatal("restarted watch missed ADDED")
			}
		}
		fresh, err := admin.CoreV1().Pods("demo-a").Get(ctx, created.Name, meta.GetOptions{})
		must(t, err)
		beforePatchRV := fresh.ResourceVersion
		// Patch metadata instead of updating a freshly scheduled Pod object. The
		// kubelet may update Pod status between GET and Update, making a full-object
		// Update race on resourceVersion and turn this watch test into a conflict
		// test. MergePatch gives us a deterministic API-side MODIFIED event.
		patched, err := admin.CoreV1().Pods("demo-a").Patch(
			ctx,
			created.Name,
			types.MergePatchType,
			[]byte(`{"metadata":{"labels":{"fixture":"updated"}}}`),
			meta.PatchOptions{},
		)
		must(t, err)
		if patched.ResourceVersion == beforePatchRV {
			t.Fatal("metadata patch did not advance resourceVersion")
		}
		eventually(t, ctx, "informer MODIFIED resourceVersion", func() bool {
			for _, p := range snapshot().Pods {
				if p.Name == created.Name {
					return p.ResourceVersion != beforePatchRV && p.Labels["fixture"] == "updated"
				}
			}
			return false
		})
		must(t, admin.CoreV1().Pods("demo-a").Delete(ctx, created.Name, meta.DeleteOptions{GracePeriodSeconds: p(int64(0))}))
		eventually(t, ctx, "informer DELETED", func() bool {
			for _, p := range snapshot().Pods {
				if p.Name == created.Name {
					return false
				}
			}
			return true
		})
	})
	t.Run("ObservedCSINodeAndVolumeAttachmentEvidence", func(t *testing.T) {
		// Synthetic API objects validate collection and evidence only. There is
		// deliberately no working CSI driver/backend or successful mount claim.
		const driver, pvName, claimName = "csi.example.invalid", "synthetic-csi-pv", "synthetic-csi-claim"
		csiNode, err := admin.StorageV1().CSINodes().Get(ctx, b, meta.GetOptions{})
		if apierrors.IsNotFound(err) {
			csiNode, err = admin.StorageV1().CSINodes().Create(ctx, &storage.CSINode{ObjectMeta: meta.ObjectMeta{Name: b}}, meta.CreateOptions{})
		}
		must(t, err)
		csiNode.Spec.Drivers = append(csiNode.Spec.Drivers, storage.CSINodeDriver{Name: driver, NodeID: "synthetic-node-id", Allocatable: &storage.VolumeNodeResources{Count: p(int32(10))}})
		_, err = admin.StorageV1().CSINodes().Update(ctx, csiNode, meta.UpdateOptions{})
		must(t, err)
		_, err = admin.CoreV1().PersistentVolumes().Create(ctx, &core.PersistentVolume{
			ObjectMeta: meta.ObjectMeta{Name: pvName},
			Spec: core.PersistentVolumeSpec{
				Capacity:                      core.ResourceList{core.ResourceStorage: resource.MustParse("1Gi")},
				AccessModes:                   []core.PersistentVolumeAccessMode{core.ReadWriteOnce},
				PersistentVolumeReclaimPolicy: core.PersistentVolumeReclaimRetain,
				PersistentVolumeSource:        core.PersistentVolumeSource{CSI: &core.CSIPersistentVolumeSource{Driver: driver, VolumeHandle: "synthetic-volume-handle"}},
			},
		}, meta.CreateOptions{})
		must(t, err)
		_, err = admin.CoreV1().PersistentVolumeClaims("demo-a").Create(ctx, &core.PersistentVolumeClaim{
			ObjectMeta: meta.ObjectMeta{Name: claimName, Namespace: "demo-a"},
			Spec: core.PersistentVolumeClaimSpec{
				VolumeName: pvName, StorageClassName: p(""), AccessModes: []core.PersistentVolumeAccessMode{core.ReadWriteOnce},
				Resources: core.VolumeResourceRequirements{Requests: core.ResourceList{core.ResourceStorage: resource.MustParse("1Gi")}},
			},
		}, meta.CreateOptions{})
		must(t, err)
		attachment, err := admin.StorageV1().VolumeAttachments().Create(ctx, &storage.VolumeAttachment{
			ObjectMeta: meta.ObjectMeta{Name: "synthetic-csi-attachment"},
			Spec:       storage.VolumeAttachmentSpec{Attacher: driver, NodeName: a, Source: storage.VolumeAttachmentSource{PersistentVolumeName: p(pvName)}},
		}, meta.CreateOptions{})
		must(t, err)
		attachment.Status.Attached = true
		attachment, err = admin.StorageV1().VolumeAttachments().UpdateStatus(ctx, attachment, meta.UpdateOptions{})
		must(t, err)
		probe := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "synthetic-csi-candidate", Namespace: "demo-a"}, Spec: core.PodSpec{NodeName: a,
			Volumes: []core.Volume{{Name: "data", VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: claimName}}}},
		}}
		eventually(t, ctx, "observed CSI registration, attachment and bound volume", func() bool {
			s := snapshot().Indexed()
			claim := s.PVC("demo-a", claimName)
			if claim == nil || claim.Status.Phase != core.ClaimBound || len(s.VolumeAttachmentByPV(pvName)) != 1 {
				return false
			}
			f := analyzer.Candidates(s, probe, a)
			if status(f, b) != "ALLOWED" {
				return false
			}
			for _, e := range f.Evaluations {
				if e.Node == b && strings.Contains(strings.Join(e.Reasons, " "), "observed allocatable limit=10") {
					return true
				}
			}
			return false
		})
		oldRV := attachment.ResourceVersion
		attachment.Status.DetachError = &storage.VolumeError{Time: meta.Now(), Message: "synthetic backend outcome unavailable"}
		_, err = admin.StorageV1().VolumeAttachments().UpdateStatus(ctx, attachment, meta.UpdateOptions{})
		must(t, err)
		eventually(t, ctx, "VolumeAttachment watch invalidates positive CSI evidence", func() bool {
			s := snapshot().Indexed()
			attachments := s.VolumeAttachmentByPV(pvName)
			return len(attachments) == 1 && attachments[0].ResourceVersion != oldRV && attachments[0].Status.DetachError != nil && status(analyzer.Candidates(s, probe, a), b) == "UNKNOWN"
		})
	})
	t.Run("HeadlessJSONStorageCapacityUnknownEvents", func(t *testing.T) {
		ca := readerCfg.CAData
		if len(ca) == 0 {
			ca, err = os.ReadFile(readerCfg.CAFile)
			must(t, err)
		}
		ro := clientapi.NewConfig()
		ro.CurrentContext = "guard9s-kind"
		ro.Clusters["kind"] = &clientapi.Cluster{Server: cfg.Host, CertificateAuthorityData: ca}
		ro.AuthInfos["reader"] = &clientapi.AuthInfo{Token: token.Status.Token}
		ro.Contexts["guard9s-kind"] = &clientapi.Context{Cluster: "kind", AuthInfo: "reader"}
		roPath := filepath.Join(t.TempDir(), "reader.config")
		must(t, clientcmd.WriteToFile(*ro, roPath))
		must(t, os.Chmod(roPath, 0600))
		// The verify job already builds and executes the production binary. Exercise
		// the same Cobra/live-source/report path in-process here so kind E2E focuses
		// on Kubernetes integration instead of spending most of its runtime doing a
		// second cold Go build in each matrix job.
		cmd := app.NewCommand("e2e")
		cmd.SetArgs([]string{"--kubeconfig", roPath, "--context", "guard9s-kind", "--node", a, "--output", "json", "--timeout", "30s"})
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		if err = cmd.ExecuteContext(ctx); err != nil {
			t.Fatal("headless command failed; raw output suppressed")
		}
		var r report.Report
		must(t, json.Unmarshal(stdout.Bytes(), &r))
		if r.SchemaVersion != "guard9s/v1" || !r.ReadOnly || r.Readiness == "READY" {
			t.Fatal("schema/readOnly/false READY")
		}
		categories := map[string]bool{}
		unknown := false
		single := false
		for _, f := range r.Findings {
			categories[f.Category] = true
			unknown = unknown || f.Unknown
			if f.Category == "Replica" && strings.Contains(f.Reason, "replica") {
				single = true
			}
		}
		if !categories["Storage"] || !categories["PDB"] || !categories["Capacity"] || !unknown || !single {
			t.Fatal("semantic finding missing", categories, single, unknown)
		}
		for _, secret := range []string{token.Status.Token, string(cfg.CertData), string(cfg.KeyData), roPath} {
			if secret != "" && (bytes.Contains(stdout.Bytes(), []byte(secret)) || bytes.Contains(stderr.Bytes(), []byte(secret))) {
				t.Fatal("credential escaped output")
			}
		}
		safe, err := report.Build(ctx, snapshot(), a, "e2e", report.Options{Redact: "basic"})
		must(t, err)
		artifacts := os.Getenv("GUARD9S_E2E_ARTIFACTS")
		if artifacts != "" {
			must(t, os.MkdirAll(artifacts, 0700))
			f, err := os.Create(filepath.Join(artifacts, "guard9s-sanitized.json"))
			must(t, err)
			must(t, report.WriteJSON(f, safe))
			must(t, f.Close())
		}
		events := snapshot().Events
		found := false
		for _, e := range events {
			if e.Name == "synthetic-warning" && e.Type == core.EventTypeWarning {
				found = true
			}
		}
		if !found {
			t.Fatal("Warning Event collection failed")
		}
	})
	t.Run("OptionalVolumeAttachmentForbidden", func(t *testing.T) {
		role, err := admin.RbacV1().ClusterRoles().Get(ctx, "guard9s-reader", meta.GetOptions{})
		must(t, err)
		for i := range role.Rules {
			var keep []string
			for _, r := range role.Rules[i].Resources {
				if r != "volumeattachments" {
					keep = append(keep, r)
				}
			}
			role.Rules[i].Resources = keep
		}
		_, err = admin.RbacV1().ClusterRoles().Update(ctx, role, meta.UpdateOptions{})
		must(t, err)
		degraded := collector.NewSource(reader, "guard9s-kind", "all")
		defer degraded.Close()
		must(t, degraded.Start(ctx, 30*time.Second))
		eventually(t, ctx, "real optional RBAC degradation", func() bool {
			s, err := degraded.Snapshot(ctx)
			must(t, err)
			return s.Capability(model.VolumeAttachmentsResource).State == model.Forbidden && s.Capability(model.VolumeAttachmentsResource).Failed && len(s.Issues) == 0
		})
	})
	source.Close()
	if writes.Load() != 0 {
		t.Fatal("production collector issued non-GET request")
	}
	t.Logf("GET-only collector requests observed: %d; non-GET: %d", reads.Load(), writes.Load())
}
