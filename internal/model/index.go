package model

import (
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	storage "k8s.io/api/storage/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	podresource "k8s.io/component-helpers/resource"
)

type budgetSelector struct {
	pdb      *policy.PodDisruptionBudget
	selector labels.Selector
	err      error
}
type snapshotIndex struct {
	nodes             map[string]*core.Node
	namespaces        map[string]*core.Namespace
	csiNodes          map[string]*storage.CSINode
	attachmentsByNode map[string][]*storage.VolumeAttachment
	attachmentsByPV   map[string][]*storage.VolumeAttachment
	podsByNamespace   map[string][]*core.Pod
	podsByNode        map[string][]*core.Pod
	podsByOwner       map[string][]*core.Pod
	pvcs              map[string]*core.PersistentVolumeClaim
	pvs               map[string]*core.PersistentVolume
	owners            map[*core.Pod]Owner
	requests          map[*core.Pod]Resources
	used              map[string]Resources
	budgets           map[string][]budgetSelector
	pending           Resources
}

// Indexed returns a copy with lookup evidence for this immutable snapshot.
// Objects and slices must remain read-only after this call. A freshly collected
// snapshot gets a fresh index; there is no global cache or cross-refresh reuse.
func (s Snapshot) Indexed() Snapshot {
	if s.index != nil && s.index.requests != nil {
		return s
	}
	s = s.WithLookups()
	x := *s.index
	x.podsByOwner = map[string][]*core.Pod{}
	x.owners = make(map[*core.Pod]Owner, len(s.Pods))
	x.requests = make(map[*core.Pod]Resources, len(s.Pods))
	x.used = map[string]Resources{}
	reuse := core.ResourceList{}
	controllerOwners := map[string]Owner{}
	for _, p := range s.Pods {
		var o Owner
		ref := meta.GetControllerOfNoCopy(p)
		_, mirror := p.Annotations[core.MirrorPodAnnotationKey]
		controllerKey := ""
		if ref != nil && !mirror {
			controllerKey = p.Namespace + "/" + ref.Kind + "/" + ref.Name + "/" + string(ref.UID)
		}
		var cached bool
		if controllerKey != "" {
			o, cached = controllerOwners[controllerKey]
		}
		if !cached {
			o = s.Owner(p)
			if controllerKey != "" {
				controllerOwners[controllerKey] = o
			}
		}
		x.owners[p] = o
		key := ownerKey(p.Namespace, o)
		x.podsByOwner[key] = append(x.podsByOwner[key], p)
		if !Active(p) {
			continue
		}
		resources := podresource.PodRequests(p, podresource.PodResourcesOptions{Reuse: reuse, UseStatusResources: true, InPlacePodLevelResourcesVerticalScalingEnabled: true})
		r := Resources{resources.Cpu().MilliValue(), resources.Memory().Value(), 1}
		x.requests[p] = r
		x.used[p.Spec.NodeName] = x.used[p.Spec.NodeName].Add(r)
		if p.Spec.NodeName == "" {
			x.pending = x.pending.Add(r)
		}
	}
	s.index = &x
	return s
}

// WithLookups builds inventory lookups without evaluating every pod's requests.
// Scheduling-only calls do not pay for capacity or owner-group computation.
func (s Snapshot) WithLookups() Snapshot {
	if s.index != nil {
		return s
	}
	x := &snapshotIndex{nodes: map[string]*core.Node{}, podsByNode: map[string][]*core.Pod{}, pvcs: map[string]*core.PersistentVolumeClaim{}, pvs: map[string]*core.PersistentVolume{}, budgets: map[string][]budgetSelector{}}
	x.namespaces = map[string]*core.Namespace{}
	x.csiNodes = map[string]*storage.CSINode{}
	x.attachmentsByNode = map[string][]*storage.VolumeAttachment{}
	x.attachmentsByPV = map[string][]*storage.VolumeAttachment{}
	for _, a := range s.VolumeAttachments {
		x.attachmentsByNode[a.Spec.NodeName] = append(x.attachmentsByNode[a.Spec.NodeName], a)
		if a.Spec.Source.PersistentVolumeName != nil {
			key := *a.Spec.Source.PersistentVolumeName
			x.attachmentsByPV[key] = append(x.attachmentsByPV[key], a)
		}
	}
	x.podsByNamespace = map[string][]*core.Pod{}
	for _, n := range s.Namespaces {
		x.namespaces[n.Name] = n
	}
	for _, n := range s.CSINodes {
		x.csiNodes[n.Name] = n
	}
	for _, n := range s.Nodes {
		x.nodes[n.Name] = n
	}
	for _, p := range s.Pods {
		x.podsByNamespace[p.Namespace] = append(x.podsByNamespace[p.Namespace], p)
		if Active(p) {
			x.podsByNode[p.Spec.NodeName] = append(x.podsByNode[p.Spec.NodeName], p)
		}
	}
	for _, p := range s.PVCs {
		x.pvcs[p.Namespace+"/"+p.Name] = p
	}
	for _, p := range s.PVs {
		x.pvs[p.Name] = p
	}
	for _, p := range s.PDBs {
		selector, err := meta.LabelSelectorAsSelector(p.Spec.Selector)
		x.budgets[p.Namespace] = append(x.budgets[p.Namespace], budgetSelector{p, selector, err})
	}
	s.index = x
	return s
}

func ownerKey(ns string, o Owner) string { return ns + "/" + o.Kind + "/" + o.Name + "/" + o.UID }
func (s Snapshot) OwnerPods(ns string, o Owner) []*core.Pod {
	if s.index != nil && s.index.podsByOwner != nil {
		return s.index.podsByOwner[ownerKey(ns, o)]
	}
	var out []*core.Pod
	for _, p := range s.Pods {
		if p.Namespace == ns && s.Owner(p) == o {
			out = append(out, p)
		}
	}
	return out
}
func (s Snapshot) PodRequests(p *core.Pod) Resources {
	if s.index != nil {
		if r, ok := s.index.requests[p]; ok {
			return r
		}
	}
	return Requests(p)
}
func (s Snapshot) PendingRequests() Resources {
	if s.index != nil && s.index.requests != nil {
		return s.index.pending
	}
	var r Resources
	for _, p := range s.Pods {
		if p.Spec.NodeName == "" && Active(p) {
			r = r.Add(Requests(p))
		}
	}
	return r
}
