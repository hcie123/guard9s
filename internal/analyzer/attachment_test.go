package analyzer

import (
	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"testing"
)

func TestVolumeAttachmentCandidateEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*model.Snapshot)
		want   string
	}{
		{"available", func(*model.Snapshot) {}, "ALLOWED"},
		{"forbidden", func(s *model.Snapshot) {
			s.Capabilities[model.VolumeAttachmentsResource] = model.Capability{State: model.Forbidden}
		}, "UNKNOWN"},
		{"unsupported", func(s *model.Snapshot) {
			s.Capabilities[model.VolumeAttachmentsResource] = model.Capability{State: model.Unsupported}
		}, "UNKNOWN"},
		{"unavailable", func(s *model.Snapshot) { delete(s.Capabilities, model.VolumeAttachmentsResource) }, "UNKNOWN"},
		{"stale_failed", func(s *model.Snapshot) {
			s.Capabilities[model.VolumeAttachmentsResource] = model.Capability{State: model.Available, Synced: true, Failed: true}
		}, "UNKNOWN"},
		{"empty_complete_inventory", func(s *model.Snapshot) { s.VolumeAttachments = nil }, "UNKNOWN"},
		{"attached_false", func(s *model.Snapshot) { s.VolumeAttachments[0].Status.Attached = false }, "UNKNOWN"},
		{"duplicate_objects", func(s *model.Snapshot) {
			a := s.VolumeAttachments[0].DeepCopy()
			a.Name = "duplicate"
			s.VolumeAttachments = append(s.VolumeAttachments, a)
		}, "UNKNOWN"},
		{"conflicting_nodes", func(s *model.Snapshot) {
			a := s.VolumeAttachments[0].DeepCopy()
			a.Name = "conflict"
			a.Spec.NodeName = "worker-03"
			s.VolumeAttachments = append(s.VolumeAttachments, a)
		}, "UNKNOWN"},
		{"wrong_attacher", func(s *model.Snapshot) { s.VolumeAttachments[0].Spec.Attacher = "other.csi.io" }, "UNKNOWN"},
		{"terminating", func(s *model.Snapshot) { s.VolumeAttachments[0].DeletionTimestamp = ptr(meta.NewTime(s.At)) }, "UNKNOWN"},
		{"attach_error", func(s *model.Snapshot) {
			s.VolumeAttachments[0].Status.AttachError = &storage.VolumeError{Message: "untrusted fixture error"}
		}, "UNKNOWN"},
		{"detach_error", func(s *model.Snapshot) {
			s.VolumeAttachments[0].Status.DetachError = &storage.VolumeError{Message: "synthetic detach error"}
		}, "UNKNOWN"},
		{"missing_attachment_node", func(s *model.Snapshot) { s.VolumeAttachments[0].Spec.NodeName = "synthetic-missing-node" }, "UNKNOWN"},
		{"empty_attachment_node", func(s *model.Snapshot) { s.VolumeAttachments[0].Spec.NodeName = "" }, "UNKNOWN"},
		{"conflicting_source_node", func(s *model.Snapshot) { s.VolumeAttachments[0].Spec.NodeName = "worker-03" }, "UNKNOWN"},
		{"multi_attach", func(s *model.Snapshot) {
			s.PVs[0].Spec.AccessModes = []core.PersistentVolumeAccessMode{core.ReadWriteMany}
		}, "UNKNOWN"},
		{"limit_reached_with_orphan_attachment", func(s *model.Snapshot) {
			pv := s.PVs[0].DeepCopy()
			pv.Name = "orphan-volume"
			pv.Spec.CSI.VolumeHandle = "orphan-handle"
			s.PVs = append(s.PVs, pv)
			a := s.VolumeAttachments[0].DeepCopy()
			a.Name = "orphan-attachment"
			a.Spec.NodeName = "worker-02"
			a.Spec.Source.PersistentVolumeName = ptr(pv.Name)
			s.VolumeAttachments = append(s.VolumeAttachments, a)
			s.CSINodes[1].Spec.Drivers[0].Allocatable.Count = ptr(int32(1))
		}, "REJECTED"},
		{"unknown_inventory_source", func(s *model.Snapshot) {
			a := s.VolumeAttachments[0].DeepCopy()
			a.Name = "missing-volume"
			a.Spec.NodeName = "worker-02"
			a.Spec.Source.PersistentVolumeName = ptr("missing")
			s.VolumeAttachments = append(s.VolumeAttachments, a)
		}, "UNKNOWN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := csiFixture()
			tc.change(&s)
			f := Candidates(s, s.Pods[0], "worker-01")
			if status(f, "worker-02") != tc.want {
				t.Fatal(f)
			}
		})
	}
}

func TestCombinedRelocationAttachmentBudget(t *testing.T) {
	s := csiFixture()
	s.Pods = s.Pods[:1]
	s.Nodes[2].Spec.Unschedulable = true
	second := s.Pods[0].DeepCopy()
	second.Name = "incoming-second"
	second.Spec.Volumes[0].PersistentVolumeClaim.ClaimName = "second-claim"
	s.Pods = append(s.Pods, second)
	claim := s.PVCs[0].DeepCopy()
	claim.Name = "second-claim"
	claim.Spec.VolumeName = "second-volume"
	s.PVCs = append(s.PVCs, claim)
	pv := s.PVs[0].DeepCopy()
	pv.Name = "second-volume"
	pv.Spec.CSI.VolumeHandle = "second-handle"
	s.PVs = append(s.PVs, pv)
	a := s.VolumeAttachments[0].DeepCopy()
	a.Name = "second-attachment"
	a.Spec.Source.PersistentVolumeName = ptr(pv.Name)
	s.VolumeAttachments = append(s.VolumeAttachments, a)
	s.CSINodes[1].Spec.Drivers[0].Allocatable.Count = ptr(int32(1))
	for _, pod := range s.Pods {
		if status(Candidates(s, pod, "worker-01"), "worker-02") != "ALLOWED" {
			t.Fatal("individual candidate unavailable")
		}
	}
	c := CalculateCapacity(Input{Snapshot: s, Node: "worker-01"})
	if c.Assessment != "UNKNOWN" || !strings.Contains(c.Detail, "combined CSI") {
		t.Fatal("combined volumes received false PASS", c)
	}
	s.CSINodes[1].Spec.Drivers[0].Allocatable.Count = ptr(int32(2))
	c = CalculateCapacity(Input{Snapshot: s, Node: "worker-01"})
	if c.Assessment != "PASS" {
		t.Fatal("known fitting CSI allocation rejected", c)
	}
}

func TestDefaultAdmissionTolerationOnlyMattersOnMatchingTaint(t *testing.T) {
	s := affinityFixture()
	s.Pods[0].Spec.Tolerations = []core.Toleration{{Key: "node.kubernetes.io/not-ready", Operator: core.TolerationOpExists, Effect: core.TaintEffectNoExecute, TolerationSeconds: ptr(int64(300))}}
	if status(Candidates(s, s.Pods[0], "worker-01"), "worker-02") != "ALLOWED" {
		t.Fatal("default admission toleration invalidated healthy node")
	}
	s.Nodes[1].Spec.Taints = []core.Taint{{Key: "node.kubernetes.io/not-ready", Effect: core.TaintEffectNoExecute}}
	if status(Candidates(s, s.Pods[0], "worker-01"), "worker-02") != "UNKNOWN" {
		t.Fatal("time-limited taint treated as permanent permission")
	}
}
