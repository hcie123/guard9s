package testfixture

import (
	"fmt"
	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CSIHeavy uses direct bound CSI PVs and an attachment per assigned volume.
// It models API evidence, not a working CSI backend.
func CSIHeavy(nodes, pods int) model.Snapshot {
	s := Snapshot(nodes, pods)
	s.PVCs = nil
	s.PVs = nil
	s.VolumeAttachments = nil
	s.Capabilities[model.VolumeAttachmentsResource] = model.Capability{State: model.Available, Synced: true}
	for i, p := range s.Pods {
		p.Spec.Affinity = nil
		p.Spec.TopologySpreadConstraints = nil
		claim := fmt.Sprintf("csi-claim-%05d", i)
		volume := "volume-" + claim
		sc := "synthetic-shared"
		p.Spec.Volumes = []core.Volume{{Name: "data", VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: claim}}}}
		s.PVCs = append(s.PVCs, &core.PersistentVolumeClaim{ObjectMeta: meta.ObjectMeta{Name: claim, Namespace: p.Namespace}, Spec: core.PersistentVolumeClaimSpec{VolumeName: volume, StorageClassName: &sc, AccessModes: []core.PersistentVolumeAccessMode{core.ReadWriteOnce}}, Status: core.PersistentVolumeClaimStatus{Phase: core.ClaimBound}})
		s.PVs = append(s.PVs, &core.PersistentVolume{ObjectMeta: meta.ObjectMeta{Name: volume}, Spec: core.PersistentVolumeSpec{PersistentVolumeSource: core.PersistentVolumeSource{CSI: &core.CSIPersistentVolumeSource{Driver: "example.invalid/synthetic-csi", VolumeHandle: claim}}, AccessModes: []core.PersistentVolumeAccessMode{core.ReadWriteOnce}}, Status: core.PersistentVolumeStatus{Phase: core.VolumeBound}})
		if p.Spec.NodeName != "" {
			name := volume
			s.VolumeAttachments = append(s.VolumeAttachments, &storage.VolumeAttachment{ObjectMeta: meta.ObjectMeta{Name: "attachment-" + claim}, Spec: storage.VolumeAttachmentSpec{Attacher: "example.invalid/synthetic-csi", NodeName: p.Spec.NodeName, Source: storage.VolumeAttachmentSource{PersistentVolumeName: &name}}, Status: storage.VolumeAttachmentStatus{Attached: true}})
		}
	}
	return s
}

// Each broad negative selector is unique. Snapshot-local absent-key evidence
// can prove these peer sets equivalent; arbitrary negative selectors cannot
// share that shortcut. Unique unmatched positive anti-affinity groups also
// exercise candidate prefiltering without changing the full matcher.
func PathologicalUniqueSelectors(nodes, pods int) model.Snapshot {
	s := Snapshot(nodes, pods)
	for i, p := range s.Pods {
		p.Spec.Volumes = nil
		p.Spec.TopologySpreadConstraints = nil
		selector := &meta.LabelSelector{MatchExpressions: []meta.LabelSelectorRequirement{{Key: fmt.Sprintf("absent-%05d", i), Operator: meta.LabelSelectorOpDoesNotExist}}}
		term := core.PodAffinityTerm{LabelSelector: selector, NamespaceSelector: &meta.LabelSelector{}, TopologyKey: core.LabelTopologyZone}
		p.Spec.Affinity = &core.Affinity{PodAffinity: &core.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{term}}, PodAntiAffinity: &core.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []core.PodAffinityTerm{{LabelSelector: &meta.LabelSelector{MatchLabels: map[string]string{"unique-absent": fmt.Sprint(i)}}, NamespaceSelector: &meta.LabelSelector{}, TopologyKey: core.LabelTopologyZone}}}}
	}
	return s
}
