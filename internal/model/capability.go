package model

import (
	core "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
)

type CapabilityState string

const (
	Available                 CapabilityState = "Available"
	Forbidden                 CapabilityState = "Forbidden"
	Unsupported               CapabilityState = "Unsupported"
	Unavailable               CapabilityState = "Unavailable"
	NamespacesResource                        = "namespaces"
	CSINodesResource                          = "csinodes"
	VolumeAttachmentsResource                 = "volumeattachments"
)

// Available means a complete initial list was observed without a later recorded
// failure. Empty inventory and unavailable collection are separate evidence.
type Capability struct {
	State  CapabilityState `json:"state"`
	Detail string          `json:"detail,omitempty"`
	Synced bool            `json:"synced,omitempty"`
	Failed bool            `json:"failed,omitempty"`
}

func (s Snapshot) Capability(resource string) Capability {
	if c, ok := s.Capabilities[resource]; ok {
		return c
	}
	return Capability{State: Unavailable, Detail: "collection evidence is not recorded"}
}

func (s Snapshot) NamespaceObject(name string) *core.Namespace {
	if s.index != nil {
		return s.index.namespaces[name]
	}
	for _, n := range s.Namespaces {
		if n.Name == name {
			return n
		}
	}
	return nil
}

func (s Snapshot) CSINode(name string) *storage.CSINode {
	if s.index != nil {
		return s.index.csiNodes[name]
	}
	for _, n := range s.CSINodes {
		if n.Name == name {
			return n
		}
	}
	return nil
}

func (s Snapshot) NamespacePods(name string) []*core.Pod {
	if s.index != nil {
		return s.index.podsByNamespace[name]
	}
	var out []*core.Pod
	for _, p := range s.Pods {
		if p.Namespace == name {
			out = append(out, p)
		}
	}
	return out
}

// Usable excludes failed evidence even if a caller retains an Available state.
// Available already implies initial sync; Synced records that fact explicitly.
func (c Capability) Usable() bool { return c.State == Available && !c.Failed }

func OptionalResource(resource string) bool {
	return resource == CSINodesResource || resource == VolumeAttachmentsResource
}

func (s Snapshot) VolumeAttachmentByNode(node string) []*storage.VolumeAttachment {
	if s.index != nil {
		return s.index.attachmentsByNode[node]
	}
	var out []*storage.VolumeAttachment
	for _, a := range s.VolumeAttachments {
		if a.Spec.NodeName == node {
			out = append(out, a)
		}
	}
	return out
}
func (s Snapshot) VolumeAttachmentByPV(pv string) []*storage.VolumeAttachment {
	if s.index != nil {
		return s.index.attachmentsByPV[pv]
	}
	var out []*storage.VolumeAttachment
	for _, a := range s.VolumeAttachments {
		if a.Spec.Source.PersistentVolumeName != nil && *a.Spec.Source.PersistentVolumeName == pv {
			out = append(out, a)
		}
	}
	return out
}
