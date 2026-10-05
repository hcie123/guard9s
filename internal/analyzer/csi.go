package analyzer

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
)

type volumeUsage struct {
	handles map[string]bool
	unknown bool
}
type csiVolumes map[string]volumeUsage

func podCSIVolumes(s model.Snapshot, p *core.Pod) csiVolumes {
	volumes := csiVolumes{}
	for _, v := range p.Spec.Volumes {
		if v.PersistentVolumeClaim == nil {
			continue
		}
		pvc := s.PVC(p.Namespace, v.PersistentVolumeClaim.ClaimName)
		if pvc == nil || pvc.Status.Phase != core.ClaimBound {
			continue
		}
		pv := s.PV(pvc.Spec.VolumeName)
		if pv == nil || pv.Spec.CSI == nil {
			continue
		}
		c := pv.Spec.CSI
		u := volumes[c.Driver]
		if u.handles == nil {
			u.handles = map[string]bool{}
		}
		if c.Driver == "" || c.VolumeHandle == "" {
			u.unknown = true
		} else {
			u.handles[c.VolumeHandle] = true
		}
		// Absence, false Attached, duplicate objects and multi-node access are not
		// proof of unused attachment slots. Require unambiguous observed evidence.
		attachments := s.VolumeAttachmentByPV(pv.Name)
		if len(attachments) != 1 || pvc.DeletionTimestamp != nil || pv.DeletionTimestamp != nil {
			u.unknown = true
		}
		for _, a := range attachments {
			if a.Spec.Attacher != c.Driver || !a.Status.Attached || a.DeletionTimestamp != nil || a.Status.AttachError != nil || a.Status.DetachError != nil {
				u.unknown = true
			}
			// A stale/orphan attachment or a conflicting source node cannot prove
			// the assigned Pod's current volume identity and attachment state.
			if a.Spec.NodeName == "" || s.Node(a.Spec.NodeName) == nil || (p.Spec.NodeName != "" && a.Spec.NodeName != p.Spec.NodeName) {
				u.unknown = true
			}
		}
		for _, mode := range pv.Spec.AccessModes {
			if mode == core.ReadWriteMany || mode == core.ReadOnlyMany {
				u.unknown = true
			}
		}
		volumes[c.Driver] = u
	}
	return volumes
}

// usage is snapshot-scoped and memoized by node/driver. Known handles are a
// lower bound: incomplete or failed inventory can never establish a PASS.
func (x *schedulingEvidence) usage(s model.Snapshot, node, driver string) volumeUsage {
	key := node + "\x00" + driver
	if u, ok := x.csiUsage[key]; ok {
		return u
	}
	u := volumeUsage{handles: map[string]bool{}, unknown: !s.Capability(model.VolumeAttachmentsResource).Usable()}
	for _, a := range s.VolumeAttachmentByNode(node) {
		if a.Spec.Attacher == "" {
			u.unknown = true
			continue
		}
		if a.Spec.Attacher != driver {
			continue
		}
		if a.Spec.Source.PersistentVolumeName == nil {
			u.unknown = true
			continue
		}
		pv := s.PV(*a.Spec.Source.PersistentVolumeName)
		if pv == nil || pv.Spec.CSI == nil || pv.Spec.CSI.Driver != driver || pv.Spec.CSI.VolumeHandle == "" {
			u.unknown = true
			continue
		}
		if a.Status.Attached {
			u.handles[pv.Spec.CSI.VolumeHandle] = true
		}
		if !a.Status.Attached || a.DeletionTimestamp != nil || pv.DeletionTimestamp != nil || a.Status.AttachError != nil || a.Status.DetachError != nil || len(s.VolumeAttachmentByPV(pv.Name)) != 1 {
			u.unknown = true
		}
	}
	for _, p := range s.NodePods(node) {
		for _, v := range p.Spec.Volumes {
			if v.CSI != nil || v.Ephemeral != nil {
				u.unknown = true
			}
			if v.PersistentVolumeClaim == nil {
				continue
			}
			pvc := s.PVC(p.Namespace, v.PersistentVolumeClaim.ClaimName)
			if pvc == nil || pvc.Status.Phase != core.ClaimBound || pvc.DeletionTimestamp != nil {
				u.unknown = true
				continue
			}
			pv := s.PV(pvc.Spec.VolumeName)
			if pv == nil || pv.DeletionTimestamp != nil {
				u.unknown = true
				continue
			}
			if migratedStorage(pv) {
				u.unknown = true
			}
			if c := pv.Spec.CSI; c != nil && c.Driver == driver {
				if c.VolumeHandle == "" || !u.handles[c.VolumeHandle] {
					u.unknown = true
				}
			}
		}
	}
	x.csiUsage[key] = u
	return u
}

func migratedStorage(pv *core.PersistentVolume) bool {
	return pv.Spec.AWSElasticBlockStore != nil || pv.Spec.GCEPersistentDisk != nil || pv.Spec.AzureDisk != nil || pv.Spec.AzureFile != nil || pv.Spec.Cinder != nil || pv.Spec.VsphereVolume != nil || pv.Spec.PortworxVolume != nil
}

func (v csiVolumes) check(s model.Snapshot, n *core.Node, x *schedulingEvidence) (rejected, unknown, checked []string) {
	if len(v) == 0 {
		return
	}
	if !s.Capability(model.CSINodesResource).Usable() {
		unknown = append(unknown, "CSI capability unavailable: CSINode "+string(s.Capability(model.CSINodesResource).State))
		return
	}
	c := s.CSINode(n.Name)
	if c == nil || c.DeletionTimestamp != nil {
		unknown = append(unknown, "CSI node registration evidence is missing or terminating")
		return
	}
	drivers := make([]string, 0, len(v))
	for driver := range v {
		drivers = append(drivers, driver)
	}
	sort.Strings(drivers)
	for _, name := range drivers {
		incoming := v[name]
		if name == "" {
			unknown = append(unknown, "CSI PV driver name is missing")
			continue
		}
		var found *storage.CSINodeDriver
		duplicate := false
		for i := range c.Spec.Drivers {
			if c.Spec.Drivers[i].Name == name {
				if found != nil {
					duplicate = true
				}
				found = &c.Spec.Drivers[i]
			}
		}
		if found == nil {
			rejected = append(rejected, "CSI driver is not registered on candidate: "+name)
			continue
		}
		if duplicate || found.NodeID == "" {
			unknown = append(unknown, "CSI driver registration is incomplete or duplicated: "+name)
			continue
		}
		checked = append(checked, "CSI driver is registered: "+name)
		keys := slices.Clone(found.TopologyKeys)
		sort.Strings(keys)
		for _, key := range keys {
			if value, ok := n.Labels[key]; !ok {
				unknown = append(unknown, "CSI topology key is missing from candidate: "+key)
			} else {
				checked = append(checked, "CSI topology "+key+"="+value)
			}
		}
		if found.Allocatable == nil || found.Allocatable.Count == nil {
			unknown = append(unknown, "CSI advertised attach limit is unspecified; attachment budget is UNKNOWN: "+name)
			continue
		}
		if *found.Allocatable.Count < 0 {
			unknown = append(unknown, "CSI allocatable limit is invalid: "+name)
			continue
		}
		u := x.usage(s, n.Name, name)
		count := len(u.handles)
		for h := range incoming.handles {
			if !u.handles[h] {
				count++
			}
		}
		limit := int(*found.Allocatable.Count)
		proof := fmt.Sprintf("CSI driver=%s observed allocatable limit=%d observed attached unique volumes plus incoming=%d", name, limit, count)
		if count > limit {
			rejected = append(rejected, "Observed CSI attachment usage would exceed advertised limit: "+proof)
		} else if !s.Capability(model.VolumeAttachmentsResource).Usable() {
			unknown = append(unknown, "VolumeAttachment capability unavailable: "+string(s.Capability(model.VolumeAttachmentsResource).State))
		} else if u.unknown || incoming.unknown {
			unknown = append(unknown, "CSI volume usage evidence is incomplete: "+name)
		} else {
			checked = append(checked, proof)
		}
	}
	return
}

// relocationAttachments reserves unique volume identities across the complete
// deterministic placement attempt. It never releases source attachments or
// predicts detach, replacement readiness, backend health or PDB recovery.
type relocationAttachments map[string]map[string]bool

func (v csiVolumes) reserve(s model.Snapshot, node string, x *schedulingEvidence, reserved relocationAttachments) bool {
	for driver, incoming := range v {
		c := s.CSINode(node)
		if c == nil || !s.Capability(model.CSINodesResource).Usable() || !s.Capability(model.VolumeAttachmentsResource).Usable() {
			continue
		}
		var count *int32
		for _, d := range c.Spec.Drivers {
			if d.Name == driver && d.Allocatable != nil {
				count = d.Allocatable.Count
			}
		}
		if count == nil || *count < 0 {
			continue
		}
		key := node + "\x00" + driver
		used := reserved[key]
		if used == nil {
			used = map[string]bool{}
			for h := range x.usage(s, node, driver).handles {
				used[h] = true
			}
		}
		total := len(used)
		for h := range incoming.handles {
			if !used[h] {
				total++
			}
		}
		if total > int(*count) {
			return false
		}
	}
	// Commit reservations only after every driver fits (all-or-nothing placement).
	for driver, incoming := range v {
		key := node + "\x00" + driver
		if reserved[key] == nil {
			reserved[key] = map[string]bool{}
			for h := range x.usage(s, node, driver).handles {
				reserved[key][h] = true
			}
		}
		for h := range incoming.handles {
			reserved[key][h] = true
		}
	}
	return true
}

func attachmentEvidence(s model.Snapshot, pv *core.PersistentVolume) string {
	var items []string
	for _, a := range s.VolumeAttachmentByPV(pv.Name) {
		items = append(items, fmt.Sprintf("VolumeAttachment=%s node=%s attached=%t attacher=%s", a.Name, a.Spec.NodeName, a.Status.Attached, a.Spec.Attacher))
	}
	sort.Strings(items)
	return fmt.Sprintf("CSI driver=%s PV=%s attachment objects=%d; %s", pv.Spec.CSI.Driver, pv.Name, len(items), strings.Join(items, "; "))
}
