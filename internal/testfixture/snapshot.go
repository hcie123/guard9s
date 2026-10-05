// Package testfixture provides deterministic, credential-free scaling fixtures.
package testfixture

import (
	"fmt"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	storage "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var Sizes = []struct {
	Name        string
	Nodes, Pods int
}{{"Small", 12, 240}, {"Medium", 50, 2000}, {"Large", 100, 5000}}

func Snapshot(nodes, pods int) model.Snapshot {
	seed := demo.Snapshot("healthy")
	s := model.Snapshot{Context: "synthetic-benchmark", Namespace: "all", At: seed.At}
	s.Capabilities = map[string]model.Capability{model.NamespacesResource: {State: model.Available}, model.CSINodesResource: {State: model.Available}}
	for _, name := range []string{"namespace-00", "namespace-01", "namespace-02", "monitoring"} {
		s.Namespaces = append(s.Namespaces, &core.Namespace{ObjectMeta: meta.ObjectMeta{Name: name, Labels: map[string]string{"tenant": "shared"}}})
	}
	controller := true
	for group := 0; group < 10; group++ {
		ns, name := fmt.Sprintf("namespace-%02d", group%3), fmt.Sprintf("workload-%02d", group)
		m := meta.ObjectMeta{Namespace: ns, Name: name, UID: types.UID("synthetic-" + name), Generation: 1}
		replicas := int32(pods / 10)
		selector := &meta.LabelSelector{MatchLabels: map[string]string{"app": name}}
		d := &apps.Deployment{ObjectMeta: m, Spec: apps.DeploymentSpec{Replicas: &replicas}, Status: apps.DeploymentStatus{ReadyReplicas: replicas}}
		rs := &apps.ReplicaSet{ObjectMeta: m, Spec: apps.ReplicaSetSpec{Replicas: &replicas}}
		rs.Name += "-rs"
		rs.UID += "-rs"
		rs.OwnerReferences = []meta.OwnerReference{{Kind: "Deployment", Name: d.Name, UID: d.UID, Controller: &controller}}
		s.Deployments = append(s.Deployments, d)
		s.ReplicaSets = append(s.ReplicaSets, rs)
		s.StatefulSets = append(s.StatefulSets, &apps.StatefulSet{ObjectMeta: m, Spec: apps.StatefulSetSpec{Replicas: &replicas}, Status: apps.StatefulSetStatus{ReadyReplicas: replicas}})
		s.PDBs = append(s.PDBs, &policy.PodDisruptionBudget{ObjectMeta: m, Spec: policy.PodDisruptionBudgetSpec{Selector: selector}, Status: policy.PodDisruptionBudgetStatus{ObservedGeneration: 1, DisruptionsAllowed: replicas, CurrentHealthy: replicas, ExpectedPods: replicas}})
	}
	ds := seed.DaemonSets[0].DeepCopy()
	ds.Status.DesiredNumberScheduled, ds.Status.NumberReady = int32(nodes), int32(nodes)
	s.DaemonSets = append(s.DaemonSets, ds)
	s.StorageClasses = []*storage.StorageClass{{ObjectMeta: meta.ObjectMeta{Name: "synthetic-shared"}, Provisioner: "example.invalid/synthetic-csi"}}
	for i := 0; i < nodes; i++ {
		n := seed.Nodes[0].DeepCopy()
		n.Name = fmt.Sprintf("worker-%02d", i+1)
		n.UID = types.UID("synthetic-" + n.Name)
		n.Labels[core.LabelHostname] = n.Name
		n.Labels[core.LabelTopologyZone] = fmt.Sprintf("zone-%d", i%3)
		n.Status.Allocatable[core.ResourceCPU] = resource.MustParse("32")
		n.Status.Allocatable[core.ResourceMemory] = resource.MustParse("64Gi")
		if i%13 == 12 {
			n.Spec.Taints = []core.Taint{{Key: "dedicated", Value: "batch", Effect: core.TaintEffectNoSchedule}}
		}
		s.Nodes = append(s.Nodes, n)
		limit := int32(10000)
		s.CSINodes = append(s.CSINodes, &storage.CSINode{ObjectMeta: meta.ObjectMeta{Name: n.Name}, Spec: storage.CSINodeSpec{Drivers: []storage.CSINodeDriver{{Name: "example.invalid/synthetic-csi", NodeID: "synthetic-" + n.Name, TopologyKeys: []string{core.LabelTopologyZone}, Allocatable: &storage.VolumeNodeResources{Count: &limit}}}}})
	}
	for i := 0; i < pods; i++ {
		group, nodeID := i%10, i%nodes
		d, rs := s.Deployments[group], s.ReplicaSets[group]
		p := seed.Pods[0].DeepCopy()
		p.Namespace, p.Name = d.Namespace, fmt.Sprintf("pod-%05d", i)
		p.UID = types.UID("synthetic-" + p.Name)
		p.Labels = map[string]string{"app": d.Name}
		p.Spec.NodeName = s.Nodes[nodeID].Name
		p.Spec.Containers[0].Resources.Requests[core.ResourceCPU] = resource.MustParse("125m")
		p.Spec.Containers[0].Resources.Requests[core.ResourceMemory] = resource.MustParse("128Mi")
		p.OwnerReferences = []meta.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &controller}}
		if i%17 == 0 {
			p.OwnerReferences = []meta.OwnerReference{{Kind: "StatefulSet", Name: d.Name, UID: d.UID, Controller: &controller}}
		}
		if i < nodes {
			p.Namespace = ds.Namespace
			p.OwnerReferences = []meta.OwnerReference{{Kind: "DaemonSet", Name: ds.Name, UID: ds.UID, Controller: &controller}}
		}
		term := core.PodAffinityTerm{TopologyKey: core.LabelTopologyZone, LabelSelector: &meta.LabelSelector{MatchLabels: map[string]string{"app": d.Name}}}
		if i%23 == 0 {
			p.Spec.Affinity = &core.Affinity{PodAffinity: &core.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{term}}}
		}
		if i%29 == 0 {
			p.Spec.Affinity = &core.Affinity{PodAntiAffinity: &core.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{term}}}
		}
		if i%31 == 0 {
			p.Spec.TopologySpreadConstraints = []core.TopologySpreadConstraint{{TopologyKey: core.LabelTopologyZone, MaxSkew: 1, WhenUnsatisfiable: core.DoNotSchedule, LabelSelector: term.LabelSelector}}
		}
		if i%19 == 0 {
			name := fmt.Sprintf("claim-%05d", i)
			sc := "synthetic-shared"
			p.Spec.Volumes = []core.Volume{{Name: "data", VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: name}}}}
			s.PVCs = append(s.PVCs, &core.PersistentVolumeClaim{ObjectMeta: meta.ObjectMeta{Namespace: p.Namespace, Name: name}, Spec: core.PersistentVolumeClaimSpec{VolumeName: "volume-" + name, StorageClassName: &sc}, Status: core.PersistentVolumeClaimStatus{Phase: core.ClaimBound}})
			s.PVs = append(s.PVs, &core.PersistentVolume{ObjectMeta: meta.ObjectMeta{Name: "volume-" + name}, Spec: core.PersistentVolumeSpec{StorageClassName: sc, PersistentVolumeSource: core.PersistentVolumeSource{CSI: &core.CSIPersistentVolumeSource{Driver: "example.invalid/synthetic-csi", VolumeHandle: name}}}, Status: core.PersistentVolumeStatus{Phase: core.VolumeBound}})
		}
		if i == pods-1 {
			p.Spec.NodeName = ""
			p.Status.Phase = core.PodPending
			p.Status.Conditions = nil
		}
		s.Pods = append(s.Pods, p)
	}
	return s
}
