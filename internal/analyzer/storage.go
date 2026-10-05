package analyzer

import (
	"context"
	"fmt"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
)

type StorageAnalyzer struct{}

func (StorageAnalyzer) Name() string { return "Storage" }
func (StorageAnalyzer) Analyze(ctx context.Context, in Input) ([]model.Finding, error) {
	var out []model.Finding
	for _, p := range pods(in) {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		for _, v := range p.Spec.Volumes {
			if v.EmptyDir != nil {
				out = append(out, podFinding(model.Warn, "Storage", p, "Data stored in emptyDir may be lost during eviction.", "volume="+v.Name, "Verify data is disposable or backed up; no force/data-deletion flags are generated."))
			}
			if v.HostPath != nil {
				out = append(out, podFinding(model.High, "Storage", p, "Workload depends on node-local filesystem.", "volume="+v.Name+"; type=hostPath", "Verify data availability on another node and plan any required migration."))
			}
			if v.Ephemeral != nil || v.CSI != nil {
				out = append(out, podUnknown("Storage", p, "Inline or generic ephemeral storage lifecycle needs manual review."))
			}
			if v.PersistentVolumeClaim == nil {
				continue
			}
			pvc := in.Snapshot.PVC(p.Namespace, v.PersistentVolumeClaim.ClaimName)
			if pvc == nil {
				out = append(out, podUnknown("Storage", p, "PVC is missing from the snapshot."))
				continue
			}
			pv := in.Snapshot.PV(pvc.Spec.VolumeName)
			if pvc.DeletionTimestamp != nil || pvc.Status.Phase == core.ClaimLost {
				out = append(out, podFinding(model.High, "Storage", p, "PVC is terminating or Lost.", fmt.Sprintf("PVC=%s; phase=%s; terminating=%t", pvc.Name, pvc.Status.Phase, pvc.DeletionTimestamp != nil), "Restore healthy storage evidence before maintenance."))
				continue
			}
			if pvc.Status.Phase != core.ClaimBound || pv == nil {
				f := podUnknown("Storage", p, "PVC is unbound or its PV is unavailable.")
				f.Evidence = fmt.Sprintf("PVC=%s; phase=%s; requested PV=%s; present=%t", pvc.Name, pvc.Status.Phase, pvc.Spec.VolumeName, pv != nil)
				out = append(out, f)
				continue
			}
			if pv.DeletionTimestamp != nil || pv.Status.Phase == core.VolumeReleased || pv.Status.Phase == core.VolumeFailed {
				out = append(out, podFinding(model.High, "Storage", p, "PV is terminating, Released or Failed.", fmt.Sprintf("PVC=%s; PV=%s; phase=%s; terminating=%t", pvc.Name, pv.Name, pv.Status.Phase, pv.DeletionTimestamp != nil), "Validate claim binding and backend recovery before maintenance."))
				continue
			}
			scName := pv.Spec.StorageClassName
			if pvc.Spec.StorageClassName != nil {
				scName = *pvc.Spec.StorageClassName
			}
			scDetail := "none"
			if scName != "" {
				scDetail = "UNKNOWN"
				for _, sc := range in.Snapshot.StorageClasses {
					if sc.Name == scName {
						scDetail = sc.Provisioner
						if sc.VolumeBindingMode != nil {
							scDetail += "; binding=" + string(*sc.VolumeBindingMode)
						}
					}
				}
				if scDetail == "UNKNOWN" {
					out = append(out, podUnknown("Storage", p, "StorageClass is missing from the snapshot."))
				}
			}
			mode := core.PersistentVolumeFilesystem
			if pvc.Spec.VolumeMode != nil {
				mode = *pvc.Spec.VolumeMode
			}
			evidence := fmt.Sprintf("PVC=%s; PV=%s; StorageClass=%s (%s); AccessModes=%v; VolumeMode=%s", pvc.Name, pv.Name, scName, scDetail, pvc.Spec.AccessModes, mode)
			if pv.Spec.CSI != nil {
				va := in.Snapshot.Capability(model.VolumeAttachmentsResource)
				detail := attachmentEvidence(in.Snapshot, pv)
				f := podFinding(model.Info, "Storage", p, "CSI attachment objects are observation evidence; sequential detach/reattach recovery requires validation.", detail, "Verify backend health and replacement readiness; attach/detach latency and data consistency remain UNKNOWN.")
				incoming := podCSIVolumes(in.Snapshot, p)
				if !va.Usable() || incoming[pv.Spec.CSI.Driver].unknown {
					f = podUnknown("Storage", p, "CSI attachment evidence is incomplete or unavailable.")
					f.Evidence = detail + "; VolumeAttachment=" + string(va.State)
				}
				out = append(out, f)
			}
			if pv.Spec.NodeAffinity != nil && pv.Spec.NodeAffinity.Required != nil {
				if n := in.Snapshot.Node(p.Spec.NodeName); n != nil {
					match, err := nodeaffinity.NewLazyErrorNodeSelector(pv.Spec.NodeAffinity.Required).Match(n)
					if err != nil {
						out = append(out, podUnknown("Storage", p, "PV affinity is invalid."))
					} else if !match {
						out = append(out, podFinding(model.High, "Storage", p, "PV affinity conflicts with the observed pod node.", evidence+"; observed node="+n.Name, "Validate binding and allowed topology; inspect candidate node evidence."))
					}
				}
			}
			if pv.Spec.Local != nil || pv.Spec.HostPath != nil {
				out = append(out, podFinding(model.High, "Storage", p, "Local PersistentVolume detected.", evidence, "Data is tied to a node; verify relocation or plan an application-specific outage."))
				continue
			}
			if pv.Spec.NodeAffinity != nil {
				out = append(out, podFinding(model.Warn, "Storage", p, "PV has node affinity constraints.", evidence, "Validate the PV's allowed nodes and storage topology."))
			}
			rwx := false
			for _, mode := range pvc.Spec.AccessModes {
				if mode == core.ReadWriteMany {
					rwx = true
				}
			}
			if rwx {
				out = append(out, podFinding(model.Info, "Storage", p, "RWX volume supports multi-node access; safety is not guaranteed.", evidence, "Validate backend health, mount permissions and workload consistency."))
			} else {
				out = append(out, podFinding(model.Warn, "Storage", p, "Volume reattachment requires validation.", evidence, "Check CSI detach/attach behavior, single-writer constraints and backend availability."))
			}
		}
	}
	return out, nil
}
