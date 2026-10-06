// Package demo builds synthetic Kubernetes API objects without loading credentials.
package demo

import (
	"fmt"
	"time"

	"github.com/hcie123/guard9s/internal/model"
	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	storage "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func ptr[T any](v T) *T { return &v }
func metadata(ns, name string) meta.ObjectMeta {
	return meta.ObjectMeta{Name: name, Namespace: ns, UID: types.UID("demo-" + ns + "-" + name), Generation: 1, CreationTimestamp: meta.NewTime(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))}
}
func owner(kind string, m meta.ObjectMeta) []meta.OwnerReference {
	return []meta.OwnerReference{{APIVersion: "apps/v1", Kind: kind, Name: m.Name, UID: m.UID, Controller: ptr(true)}}
}
func quantities(cpu, mem string) core.ResourceList {
	return core.ResourceList{core.ResourceCPU: resource.MustParse(cpu), core.ResourceMemory: resource.MustParse(mem)}
}

func Snapshot(scenario string) model.Snapshot {
	now := time.Date(2026, 1, 16, 12, 0, 0, 0, time.UTC)
	s := model.Snapshot{Context: "demo-cluster", Namespace: "all", At: now}
	s.Capabilities = map[string]model.Capability{
		model.NamespacesResource:        {State: model.Available},
		model.CSINodesResource:          {State: model.Available},
		model.VolumeAttachmentsResource: {State: model.Unsupported, Detail: "not collected in this synthetic snapshot"},
	}
	for _, name := range []string{"demo", "monitoring", "production"} {
		s.Namespaces = append(s.Namespaces, &core.Namespace{ObjectMeta: metadata("", name)})
	}
	deploy := &apps.Deployment{ObjectMeta: metadata("demo", "nginx"), Spec: apps.DeploymentSpec{Replicas: ptr(int32(24))}, Status: apps.DeploymentStatus{ReadyReplicas: 24, AvailableReplicas: 24}}
	rs := &apps.ReplicaSet{ObjectMeta: metadata("demo", "nginx-rs"), Spec: apps.ReplicaSetSpec{Replicas: ptr(int32(24))}, Status: apps.ReplicaSetStatus{ReadyReplicas: 24}}
	rs.OwnerReferences = owner("Deployment", deploy.ObjectMeta)
	ds := &apps.DaemonSet{ObjectMeta: metadata("monitoring", "node-agent"), Status: apps.DaemonSetStatus{DesiredNumberScheduled: 3, NumberReady: 3}}
	s.Deployments = append(s.Deployments, deploy)
	s.ReplicaSets = append(s.ReplicaSets, rs)
	s.DaemonSets = append(s.DaemonSets, ds)
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("worker-%02d", i)
		n := &core.Node{ObjectMeta: metadata("", name)}
		n.Labels = map[string]string{"kubernetes.io/hostname": name, "node.kubernetes.io/instance-type": "demo", "pool": "general"}
		n.Status.Allocatable = quantities("8", "16Gi")
		n.Status.Allocatable[core.ResourcePods] = resource.MustParse("110")
		n.Status.Conditions = []core.NodeCondition{{Type: core.NodeReady, Status: core.ConditionTrue}, {Type: core.NodeMemoryPressure, Status: core.ConditionFalse}, {Type: core.NodeDiskPressure, Status: core.ConditionFalse}, {Type: core.NodePIDPressure, Status: core.ConditionFalse}, {Type: core.NodeNetworkUnavailable, Status: core.ConditionFalse}}
		n.Status.NodeInfo = core.NodeSystemInfo{KubeletVersion: "v1.35.6", KernelVersion: "6.8.0-demo", OSImage: "Demo Linux", OperatingSystem: "linux", Architecture: "amd64", ContainerRuntimeVersion: "containerd://2.0-demo"}
		s.Nodes = append(s.Nodes, n)
		s.CSINodes = append(s.CSINodes, &storage.CSINode{ObjectMeta: metadata("", name), Spec: storage.CSINodeSpec{Drivers: []storage.CSINodeDriver{{Name: "example.invalid/demo-csi", NodeID: "synthetic-" + name, TopologyKeys: []string{core.LabelHostname}, Allocatable: &storage.VolumeNodeResources{Count: ptr(int32(128))}}}}})
		for j := 1; j <= 8; j++ {
			p := pod("demo", fmt.Sprintf("nginx-%d-%02d", i, j), name, "250m", "256Mi", now)
			p.OwnerReferences = owner("ReplicaSet", rs.ObjectMeta)
			p.Labels = map[string]string{"app": "nginx"}
			s.Pods = append(s.Pods, p)
		}
		p := pod("monitoring", fmt.Sprintf("node-agent-%d", i), name, "100m", "128Mi", now)
		p.OwnerReferences = owner("DaemonSet", ds.ObjectMeta)
		s.Pods = append(s.Pods, p)
	}
	s.PDBs = append(s.PDBs, &policy.PodDisruptionBudget{ObjectMeta: metadata("demo", "nginx-budget"), Spec: policy.PodDisruptionBudgetSpec{MaxUnavailable: ptr(intstr.FromInt32(2)), Selector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "nginx"}}}, Status: policy.PodDisruptionBudgetStatus{ObservedGeneration: 1, CurrentHealthy: 24, DesiredHealthy: 22, ExpectedPods: 24, DisruptionsAllowed: 2}})
	if scenario == "healthy" || scenario == "scheduling" {
		s.PDBs[0].Spec.MaxUnavailable = ptr(intstr.FromInt32(9))
		s.PDBs[0].Status.DesiredHealthy = 15
		s.PDBs[0].Status.DisruptionsAllowed = 9
		if scenario == "scheduling" {
			return scheduling(s)
		}
		return s
	}
	s.Nodes[2].Status.Conditions[1].Status = core.ConditionTrue
	s.Nodes[1].Status.Allocatable = quantities("4", "8Gi")
	s.Nodes[1].Status.Allocatable[core.ResourcePods] = resource.MustParse("110")
	payment := &apps.Deployment{ObjectMeta: metadata("production", "payment-api"), Spec: apps.DeploymentSpec{Replicas: ptr(int32(1))}, Status: apps.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1}}
	paymentRS := &apps.ReplicaSet{ObjectMeta: metadata("production", "payment-api-rs")}
	paymentRS.OwnerReferences = owner("Deployment", payment.ObjectMeta)
	s.Deployments = append(s.Deployments, payment)
	s.ReplicaSets = append(s.ReplicaSets, paymentRS)
	p := pod("production", "payment-api-01", "worker-01", "500m", "512Mi", now)
	p.OwnerReferences = owner("ReplicaSet", paymentRS.ObjectMeta)
	p.Labels = map[string]string{"app": "payment-api"}
	s.Pods = append(s.Pods, p)
	s.PDBs = append(s.PDBs, &policy.PodDisruptionBudget{ObjectMeta: metadata("production", "payment-api-pdb"), Spec: policy.PodDisruptionBudgetSpec{MinAvailable: ptr(intstr.FromInt32(1)), Selector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "payment-api"}}}, Status: policy.PodDisruptionBudgetStatus{ObservedGeneration: 1, CurrentHealthy: 1, DesiredHealthy: 1, ExpectedPods: 1, DisruptionsAllowed: 0}})
	sts := &apps.StatefulSet{ObjectMeta: metadata("production", "redis"), Spec: apps.StatefulSetSpec{Replicas: ptr(int32(1))}, Status: apps.StatefulSetStatus{ReadyReplicas: 1}}
	s.StatefulSets = append(s.StatefulSets, sts)
	p = pod("production", "redis-0", "worker-01", "250m", "512Mi", now)
	p.OwnerReferences = owner("StatefulSet", sts.ObjectMeta)
	p.Spec.Volumes = []core.Volume{{Name: "data", VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: "redis-data"}}}}
	s.Pods = append(s.Pods, p)
	s.StorageClasses = []*storage.StorageClass{{ObjectMeta: metadata("", "demo-local"), Provisioner: "kubernetes.io/no-provisioner", VolumeBindingMode: ptr(storage.VolumeBindingWaitForFirstConsumer)}, {ObjectMeta: metadata("", "demo-shared"), Provisioner: "example.invalid/demo-csi"}}
	s.PVCs = []*core.PersistentVolumeClaim{{ObjectMeta: metadata("production", "redis-data"), Spec: core.PersistentVolumeClaimSpec{VolumeName: "redis-local-pv", StorageClassName: ptr("demo-local"), AccessModes: []core.PersistentVolumeAccessMode{core.ReadWriteOnce}, VolumeMode: ptr(core.PersistentVolumeFilesystem)}, Status: core.PersistentVolumeClaimStatus{Phase: core.ClaimBound}}, {ObjectMeta: metadata("demo", "shared-data"), Spec: core.PersistentVolumeClaimSpec{VolumeName: "shared-pv", StorageClassName: ptr("demo-shared"), AccessModes: []core.PersistentVolumeAccessMode{core.ReadWriteMany}}, Status: core.PersistentVolumeClaimStatus{Phase: core.ClaimBound}}}
	s.PVs = []*core.PersistentVolume{{ObjectMeta: metadata("", "redis-local-pv"), Spec: core.PersistentVolumeSpec{PersistentVolumeSource: core.PersistentVolumeSource{Local: &core.LocalVolumeSource{Path: "/var/lib/demo-data"}}, StorageClassName: "demo-local", AccessModes: []core.PersistentVolumeAccessMode{core.ReadWriteOnce}, NodeAffinity: &core.VolumeNodeAffinity{Required: &core.NodeSelector{NodeSelectorTerms: []core.NodeSelectorTerm{{MatchExpressions: []core.NodeSelectorRequirement{{Key: "kubernetes.io/hostname", Operator: core.NodeSelectorOpIn, Values: []string{"worker-01"}}}}}}}}}, {ObjectMeta: metadata("", "shared-pv"), Spec: core.PersistentVolumeSpec{PersistentVolumeSource: core.PersistentVolumeSource{CSI: &core.CSIPersistentVolumeSource{Driver: "example.invalid/demo-csi", VolumeHandle: "demo-volume"}}, StorageClassName: "demo-shared", AccessModes: []core.PersistentVolumeAccessMode{core.ReadWriteMany}}}}
	s.Pods[0].Spec.Volumes = []core.Volume{{Name: "scratch", VolumeSource: core.VolumeSource{EmptyDir: &core.EmptyDirVolumeSource{}}}}
	s.Pods[9].Spec.Volumes = []core.Volume{{Name: "shared", VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: "shared-data"}}}}
	s.Pods[18].Spec.Volumes = []core.Volume{{Name: "host-data", VolumeSource: core.VolumeSource{HostPath: &core.HostPathVolumeSource{Path: "/var/lib/demo-data"}}}}
	s.Pods[19].Status.ContainerStatuses[0].LastTerminationState = core.ContainerState{Terminated: &core.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, FinishedAt: meta.NewTime(now.Add(-2 * time.Minute))}}
	s.Pods[19].Status.ContainerStatuses[0].RestartCount = 12
	s.Pods[20].Status.ContainerStatuses[0].State = core.ContainerState{Waiting: &core.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
	s.Pods[20].Status.ContainerStatuses[0].Ready = false
	s.Pods[20].Status.Conditions[0].Status = core.ConditionFalse
	p = pod("demo", "capacity-check", "worker-03", "5500m", "8Gi", now)
	s.Pods = append(s.Pods, p)
	p = pod("demo", "pending-example", "", "500m", "256Mi", now)
	p.Status.Phase = core.PodPending
	p.Status.Conditions[0].Status = core.ConditionFalse
	p.Status.ContainerStatuses = nil
	s.Pods = append(s.Pods, p)
	p = pod("monitoring", "static-example", "worker-01", "100m", "128Mi", now)
	p.Annotations = map[string]string{core.MirrorPodAnnotationKey: "demo-only"}
	s.Pods = append(s.Pods, p)
	cj := &batch.CronJob{ObjectMeta: metadata("demo", "nginx-check")}
	job := &batch.Job{ObjectMeta: metadata("demo", "nginx-check-job")}
	job.OwnerReferences = owner("CronJob", cj.ObjectMeta)
	s.CronJobs = append(s.CronJobs, cj)
	s.Jobs = append(s.Jobs, job)
	p = pod("demo", "nginx-check-job-01", "worker-02", "100m", "64Mi", now)
	p.OwnerReferences = owner("Job", job.ObjectMeta)
	s.Pods = append(s.Pods, p)
	s.Events = []*core.Event{{ObjectMeta: metadata("demo", "pending-event"), Type: "Warning", Reason: "FailedScheduling", Message: "No eligible node has sufficient requested CPU and memory.", InvolvedObject: core.ObjectReference{Kind: "Pod", Namespace: "demo", Name: "pending-example"}, Count: 4, LastTimestamp: meta.NewTime(now.Add(-time.Minute))}, {ObjectMeta: metadata("demo", "backoff-event"), Type: "Warning", Reason: "BackOff", Message: "Back-off restarting failed nginx container.", InvolvedObject: core.ObjectReference{Kind: "Pod", Namespace: "demo", Name: s.Pods[20].Name}, Count: 12, LastTimestamp: meta.NewTime(now.Add(-2 * time.Minute))}, {ObjectMeta: metadata("demo", "normal-event"), Type: "Normal", Reason: "Pulled", Message: "Container image already present.", InvolvedObject: core.ObjectReference{Kind: "Pod", Namespace: "demo", Name: "nginx-1-01"}, LastTimestamp: meta.NewTime(now)}}
	return s
}

func pod(ns, name, node, cpu, memory string, now time.Time) *core.Pod {
	p := &core.Pod{ObjectMeta: metadata(ns, name), Spec: core.PodSpec{NodeName: node, Containers: []core.Container{{Name: "app", Image: "nginx:stable", Resources: core.ResourceRequirements{Requests: quantities(cpu, memory)}}}}, Status: core.PodStatus{Phase: core.PodRunning, StartTime: ptr(meta.NewTime(now.Add(-time.Hour))), Conditions: []core.PodCondition{{Type: core.PodReady, Status: core.ConditionTrue}}, ContainerStatuses: []core.ContainerStatus{{Name: "app", Ready: true, State: core.ContainerState{Running: &core.ContainerStateRunning{StartedAt: meta.NewTime(now.Add(-time.Hour))}}}}}}
	return p
}
