package analyzer

import (
	"testing"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func csiFixture() model.Snapshot {
	s := affinityFixture()
	s.Pods[0].Spec.Volumes = []core.Volume{{Name: "data", VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: "synthetic-claim"}}}}
	s.PVCs = []*core.PersistentVolumeClaim{{ObjectMeta: meta.ObjectMeta{Name: "synthetic-claim", Namespace: s.Pods[0].Namespace}, Spec: core.PersistentVolumeClaimSpec{VolumeName: "synthetic-volume"}, Status: core.PersistentVolumeClaimStatus{Phase: core.ClaimBound}}}
	s.PVs = []*core.PersistentVolume{{ObjectMeta: meta.ObjectMeta{Name: "synthetic-volume"}, Spec: core.PersistentVolumeSpec{PersistentVolumeSource: core.PersistentVolumeSource{CSI: &core.CSIPersistentVolumeSource{Driver: "example.invalid/test-csi", VolumeHandle: "synthetic-handle"}}}, Status: core.PersistentVolumeStatus{Phase: core.VolumeBound}}}
	for _, n := range s.CSINodes {
		n.Spec.Drivers = []storage.CSINodeDriver{{Name: "example.invalid/test-csi", NodeID: "synthetic-node-id", TopologyKeys: []string{core.LabelTopologyZone}, Allocatable: &storage.VolumeNodeResources{Count: ptr(int32(10))}}}
	}
	s.Capabilities[model.VolumeAttachmentsResource] = model.Capability{State: model.Available, Synced: true}
	s.VolumeAttachments = []*storage.VolumeAttachment{{ObjectMeta: meta.ObjectMeta{Name: "synthetic-attachment"}, Spec: storage.VolumeAttachmentSpec{Attacher: "example.invalid/test-csi", NodeName: "worker-01", Source: storage.VolumeAttachmentSource{PersistentVolumeName: ptr("synthetic-volume")}}, Status: storage.VolumeAttachmentStatus{Attached: true}}}
	return s
}

func TestCSIOptionalEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*model.Snapshot)
		want   string
	}{
		{"registered", func(*model.Snapshot) {}, "ALLOWED"},
		{"driver_absent", func(s *model.Snapshot) { s.CSINodes[1].Spec.Drivers = nil }, "REJECTED"},
		{"node_absent", func(s *model.Snapshot) { s.CSINodes = append(s.CSINodes[:1], s.CSINodes[2:]...) }, "UNKNOWN"},
		{"empty_available_inventory", func(s *model.Snapshot) { s.CSINodes = nil }, "UNKNOWN"},
		{"forbidden", func(s *model.Snapshot) {
			s.Capabilities[model.CSINodesResource] = model.Capability{State: model.Forbidden}
		}, "UNKNOWN"},
		{"unsupported", func(s *model.Snapshot) {
			s.Capabilities[model.CSINodesResource] = model.Capability{State: model.Unsupported}
		}, "UNKNOWN"},
		{"unavailable", func(s *model.Snapshot) { s.Capabilities = nil }, "UNKNOWN"},
		{"terminating_registration", func(s *model.Snapshot) { s.CSINodes[1].DeletionTimestamp = ptr(meta.NewTime(s.At)) }, "UNKNOWN"},
		{"incomplete_registration", func(s *model.Snapshot) { s.CSINodes[1].Spec.Drivers[0].NodeID = "" }, "UNKNOWN"},
		{"duplicate_driver", func(s *model.Snapshot) {
			s.CSINodes[1].Spec.Drivers = append(s.CSINodes[1].Spec.Drivers, s.CSINodes[1].Spec.Drivers[0])
		}, "UNKNOWN"},
		{"no_allocatable_budget_unknown", func(s *model.Snapshot) { s.CSINodes[1].Spec.Drivers[0].Allocatable = nil }, "UNKNOWN"},
		{"no_count_budget_unknown", func(s *model.Snapshot) { s.CSINodes[1].Spec.Drivers[0].Allocatable.Count = nil }, "UNKNOWN"},
		{"negative_limit", func(s *model.Snapshot) { s.CSINodes[1].Spec.Drivers[0].Allocatable.Count = ptr(int32(-1)) }, "UNKNOWN"},
		{"zero_limit", func(s *model.Snapshot) { s.CSINodes[1].Spec.Drivers[0].Allocatable.Count = ptr(int32(0)) }, "REJECTED"},
		{"missing_topology", func(s *model.Snapshot) { delete(s.Nodes[1].Labels, core.LabelTopologyZone) }, "UNKNOWN"},
		{"no_topology_supported", func(s *model.Snapshot) { s.CSINodes[1].Spec.Drivers[0].TopologyKeys = nil }, "ALLOWED"},
		{"missing_handle", func(s *model.Snapshot) { s.PVs[0].Spec.CSI.VolumeHandle = "" }, "UNKNOWN"},
		{"missing_driver", func(s *model.Snapshot) { s.PVs[0].Spec.CSI.Driver = "" }, "UNKNOWN"},
		{"missing_handle_unbounded", func(s *model.Snapshot) {
			s.PVs[0].Spec.CSI.VolumeHandle = ""
			s.CSINodes[1].Spec.Drivers[0].Allocatable = nil
		}, "UNKNOWN"},
		{"assigned_without_attachment_unknown", func(s *model.Snapshot) {
			claim := s.PVCs[0].DeepCopy()
			claim.Name = "other-claim"
			claim.Spec.VolumeName = "other-volume"
			s.PVCs = append(s.PVCs, claim)
			pv := s.PVs[0].DeepCopy()
			pv.Name = "other-volume"
			pv.Spec.CSI.VolumeHandle = "other-handle"
			s.PVs = append(s.PVs, pv)
			s.Pods[1].Spec.Volumes = []core.Volume{{Name: "data", VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: claim.Name}}}}
			s.CSINodes[1].Spec.Drivers[0].Allocatable.Count = ptr(int32(1))
		}, "UNKNOWN"},
		{"shared_volume_counts_once", func(s *model.Snapshot) {
			// An unassigned incoming Pod can reuse the candidate's observed volume.
			s.Pods[0].Spec.NodeName = ""
			s.Pods[0].Status.Phase = core.PodPending
			s.Pods[1].Spec.Volumes = s.Pods[0].Spec.Volumes
			s.VolumeAttachments[0].Spec.NodeName = "worker-02"
			s.CSINodes[1].Spec.Drivers[0].Allocatable.Count = ptr(int32(1))
		}, "ALLOWED"},
		{"shared_volume_conflicting_assignment", func(s *model.Snapshot) {
			s.Pods[1].Spec.Volumes = s.Pods[0].Spec.Volumes
			s.VolumeAttachments[0].Spec.NodeName = "worker-02"
		}, "UNKNOWN"},
		{"unknown_peer_claim", func(s *model.Snapshot) {
			s.Pods[1].Spec.Volumes = []core.Volume{{Name: "data", VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: "missing"}}}}
		}, "UNKNOWN"},
		{"inline_peer_usage", func(s *model.Snapshot) {
			s.Pods[1].Spec.Volumes = []core.Volume{{Name: "inline", VolumeSource: core.VolumeSource{CSI: &core.CSIVolumeSource{Driver: "example.invalid/test-csi"}}}}
		}, "UNKNOWN"},
		{"missing_peer_handle", func(s *model.Snapshot) {
			claim := s.PVCs[0].DeepCopy()
			claim.Name = "other-claim"
			claim.Spec.VolumeName = "other-volume"
			s.PVCs = append(s.PVCs, claim)
			pv := s.PVs[0].DeepCopy()
			pv.Name = "other-volume"
			pv.Spec.CSI.VolumeHandle = ""
			s.PVs = append(s.PVs, pv)
			s.Pods[1].Spec.Volumes = []core.Volume{{Name: "data", VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: claim.Name}}}}
		}, "UNKNOWN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := csiFixture()
			tc.change(&s)
			f := Candidates(s, s.Pods[0], "worker-01")
			if status(f, "worker-02") != tc.want {
				t.Fatalf("%+v", f)
			}
		})
	}
}

func TestCSICapabilityRefreshDoesNotReuseEvidence(t *testing.T) {
	s := csiFixture()
	if status(Candidates(s, s.Pods[0], "worker-01"), "worker-02") != "ALLOWED" {
		t.Fatal("registered driver not checked")
	}
	s = csiFixture()
	s.Capabilities[model.CSINodesResource] = model.Capability{State: model.Forbidden}
	if status(Candidates(s, s.Pods[0], "worker-01"), "worker-02") != "UNKNOWN" {
		t.Fatal("stale available capability")
	}
	s = csiFixture()
	s.CSINodes[1].Spec.Drivers = nil
	if status(Candidates(s, s.Pods[0], "worker-01"), "worker-02") != "REJECTED" {
		t.Fatal("stale driver registration")
	}
}
