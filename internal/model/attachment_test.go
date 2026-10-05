package model

import (
	storage "k8s.io/api/storage/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestVolumeAttachmentIndexesRefresh(t *testing.T) {
	pv := "volume-a"
	a := &storage.VolumeAttachment{ObjectMeta: meta.ObjectMeta{Name: "attachment-a"}, Spec: storage.VolumeAttachmentSpec{NodeName: "worker-a", Attacher: "example.csi.io", Source: storage.VolumeAttachmentSource{PersistentVolumeName: &pv}}}
	s := Snapshot{VolumeAttachments: []*storage.VolumeAttachment{a}}
	for _, snap := range []Snapshot{s, s.WithLookups(), s.Indexed()} {
		if len(snap.VolumeAttachmentByNode("worker-a")) != 1 || len(snap.VolumeAttachmentByPV(pv)) != 1 {
			t.Fatal("index lost attachment")
		}
		if len(snap.VolumeAttachmentByNode("absent")) != 0 || len(snap.VolumeAttachmentByPV("absent")) != 0 {
			t.Fatal("index invented attachment")
		}
	}
	before := s.Indexed()
	a = a.DeepCopy()
	a.Spec.NodeName = "worker-b"
	after := Snapshot{VolumeAttachments: []*storage.VolumeAttachment{a}}.Indexed()
	if len(before.VolumeAttachmentByNode("worker-a")) != 1 || len(after.VolumeAttachmentByNode("worker-a")) != 0 || len(after.VolumeAttachmentByNode("worker-b")) != 1 {
		t.Fatal("index reused across refresh")
	}
	if (Capability{State: Available, Failed: true}).Usable() {
		t.Fatal("stale failed capability was usable")
	}
}
