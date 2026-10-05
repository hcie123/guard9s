package report

import (
	"fmt"
	"net"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type identity struct{ kind, ns, name string }
type redactor struct {
	ids       map[identity]string
	tokens    map[string]string
	addresses map[string]string
	keys      []string
	root      *tokenTrie
}
type tokenTrie struct {
	next  map[byte]*tokenTrie
	alias string
}

func newRedactor(s model.Snapshot, extra ...identity) *redactor {
	r := &redactor{ids: map[identity]string{}, tokens: map[string]string{}, addresses: map[string]string{}}
	identities := map[identity]bool{}
	for _, id := range extra {
		identities[id] = true
		if id.ns != "" {
			identities[identity{"namespace", "", id.ns}] = true
		}
	}
	add := func(kind string, o meta.Object) {
		identities[identity{kind, o.GetNamespace(), o.GetName()}] = true
		if ns := o.GetNamespace(); ns != "" {
			identities[identity{"namespace", "", ns}] = true
		}
	}
	identities[identity{"context", "", s.Context}] = true
	for _, x := range s.Namespaces {
		add("namespace", x)
	}
	for _, x := range s.CSINodes {
		add("csinode", x)
		for _, d := range x.Spec.Drivers {
			identities[identity{"csidriver", "", d.Name}] = true
		}
	}
	for _, x := range s.VolumeAttachments {
		add("volumeattachment", x)
		identities[identity{"csidriver", "", x.Spec.Attacher}] = true
		// Attachment evidence can reference objects missing from the current
		// Node/PV inventory. Register those identifiers directly so basic
		// redaction does not leak orphaned or stale attachment references.
		identities[identity{"node", "", x.Spec.NodeName}] = true
		if x.Spec.Source.PersistentVolumeName != nil {
			identities[identity{"pv", "", *x.Spec.Source.PersistentVolumeName}] = true
		}
	}
	for _, x := range s.Nodes {
		add("node", x)
		for key, value := range x.Labels {
			_, numericErr := strconv.ParseFloat(value, 64)
			if !strings.HasPrefix(key, "kubernetes.io/") && !strings.HasPrefix(key, "node.kubernetes.io/") && len(value) >= 4 && numericErr != nil {
				identities[identity{"label", "", value}] = true
			}
		}
		for _, address := range x.Status.Addresses {
			identities[identity{"address", "", address.Address}] = true
		}
	}
	for _, p := range s.Pods {
		add("pod", p)
		for _, cs := range [][]core.Container{p.Spec.Containers, p.Spec.InitContainers} {
			for _, c := range cs {
				identities[identity{"container", p.Namespace, c.Name}] = true
				identities[identity{"image", "", c.Image}] = true
				for _, m := range c.VolumeMounts {
					identities[identity{"path", "", m.MountPath}] = true
				}
			}
		}
		for _, v := range p.Spec.Volumes {
			if v.HostPath != nil {
				identities[identity{"path", "", v.HostPath.Path}] = true
			}
		}
	}
	for _, x := range s.Deployments {
		add("deployment", x)
	}
	for _, x := range s.ReplicaSets {
		add("replicaset", x)
	}
	for _, x := range s.StatefulSets {
		add("statefulset", x)
	}
	for _, x := range s.DaemonSets {
		add("daemonset", x)
	}
	for _, x := range s.Jobs {
		add("job", x)
	}
	for _, x := range s.CronJobs {
		add("cronjob", x)
	}
	for _, x := range s.PDBs {
		add("pdb", x)
	}
	for _, x := range s.PVCs {
		add("pvc", x)
	}
	for _, x := range s.StorageClasses {
		add("storageclass", x)
	}
	for _, x := range s.PVs {
		add("pv", x)
		if x.Spec.CSI != nil {
			identities[identity{"csidriver", "", x.Spec.CSI.Driver}] = true
			identities[identity{"csivolume", "", x.Spec.CSI.VolumeHandle}] = true
		}
		if x.Spec.Local != nil {
			identities[identity{"path", "", x.Spec.Local.Path}] = true
		}
		if x.Spec.HostPath != nil {
			identities[identity{"path", "", x.Spec.HostPath.Path}] = true
		}
	}
	for _, x := range s.Events {
		add("event", x)
		if x.InvolvedObject.Name != "" {
			identities[identity{canonicalKind(x.InvolvedObject.Kind), x.InvolvedObject.Namespace, x.InvolvedObject.Name}] = true
		}
	}
	list := make([]identity, 0, len(identities))
	for id := range identities {
		if id.name != "" {
			list = append(list, id)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		return a.kind+"/"+a.ns+"/"+a.name < b.kind+"/"+b.ns+"/"+b.name
	})
	counts := map[string]int{}
	for _, id := range list {
		counts[id.kind]++
		alias := fmt.Sprintf("%s-%03d", id.kind, counts[id.kind])
		r.ids[id] = alias
		if id.kind == "address" {
			r.addresses[id.name] = alias
		}
	}
	// Collect ambiguous plain/namespace-qualified tokens before choosing aliases.
	// Map iteration must never choose between a Pod and Deployment with one name.
	choices := map[string][]string{}
	addToken := func(token, alias string) {
		if !slices.Contains(choices[token], alias) {
			choices[token] = append(choices[token], alias)
		}
	}
	for _, id := range list {
		alias := r.ids[id]
		addToken(id.name, alias)
		if id.ns != "" {
			addToken(id.ns+"/"+id.name, r.ids[identity{"namespace", "", id.ns}]+"/"+alias)
		}
	}
	tokens := make([]string, 0, len(choices))
	for token := range choices {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	ambiguous := 0
	for _, token := range tokens {
		values := choices[token]
		// CSINode and Node share a physical node identity. Preserve the Node
		// alias in untyped prose unless a genuinely different kind also matches.
		nodeAlias, nodeOnly := "", true
		for _, value := range values {
			if strings.HasPrefix(value, "node-") {
				nodeAlias = value
			} else if !strings.HasPrefix(value, "csinode-") {
				nodeOnly = false
			}
		}
		if nodeOnly && nodeAlias != "" {
			r.tokens[token] = nodeAlias
		} else if len(values) == 1 {
			r.tokens[token] = values[0]
		} else {
			ambiguous++
			r.tokens[token] = fmt.Sprintf("identifier-%03d", ambiguous)
		}
	}
	for _, id := range list {
		alias := r.ids[id]
		kind := displayKind(id.kind)
		addTyped := func(prefix, suffix string) { r.tokens[prefix+"/"+suffix] = prefix + "/" + alias }
		addTyped(kind, id.name)
		addTyped(id.kind, id.name)
		if id.ns != "" {
			ns := r.ids[identity{"namespace", "", id.ns}]
			r.tokens[kind+"/"+id.ns+"/"+id.name] = kind + "/" + ns + "/" + alias
			r.tokens[id.kind+"/"+id.ns+"/"+id.name] = id.kind + "/" + ns + "/" + alias
		}
	}
	for key := range r.tokens {
		r.keys = append(r.keys, key)
	}
	sort.Slice(r.keys, func(i, j int) bool {
		if len(r.keys[i]) != len(r.keys[j]) {
			return len(r.keys[i]) > len(r.keys[j])
		}
		return r.keys[i] < r.keys[j]
	})
	r.root = &tokenTrie{next: map[byte]*tokenTrie{}}
	for _, key := range r.keys {
		node := r.root
		for i := 0; i < len(key); i++ {
			child := node.next[key[i]]
			if child == nil {
				child = &tokenTrie{next: map[byte]*tokenTrie{}}
				node.next[key[i]] = child
			}
			node = child
		}
		node.alias = r.tokens[key]
	}
	return r
}
func (r *redactor) id(kind, ns, name string) string {
	if name == "" {
		return ""
	}
	if value, ok := r.ids[identity{kind, ns, name}]; ok {
		return value
	}
	return kind + "-redacted"
}
func displayKind(kind string) string {
	switch kind {
	case "pod":
		return "Pod"
	case "node":
		return "Node"
	case "namespace":
		return "Namespace"
	case "deployment":
		return "Deployment"
	case "replicaset":
		return "ReplicaSet"
	case "statefulset":
		return "StatefulSet"
	case "daemonset":
		return "DaemonSet"
	case "cronjob":
		return "CronJob"
	case "job":
		return "Job"
	case "pv":
		return "PersistentVolume"
	case "pvc":
		return "PersistentVolumeClaim"
	case "pdb":
		return "PodDisruptionBudget"
	case "storageclass":
		return "StorageClass"
	case "csinode":
		return "CSINode"
	case "volumeattachment":
		return "VolumeAttachment"
	case "event":
		return "Event"
	default:
		return kind
	}
}
func findingKind(f ExportFinding) string {
	if f.ResourceKind != "" {
		return canonicalKind(f.ResourceKind)
	}
	if f.Category == "Node" || f.Category == "Capacity" || f.Namespace == "" {
		return "node"
	}
	return "pod"
}
func canonicalKind(kind string) string {
	switch strings.ToLower(kind) {
	case "persistentvolume":
		return "pv"
	case "persistentvolumeclaim":
		return "pvc"
	case "poddisruptionbudget":
		return "pdb"
	default:
		return strings.ToLower(kind)
	}
}
func nameRune(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '-' || c == '.'
}
func (r *redactor) text(text string) string {
	// Scan the original once; replacements are never redacted a second time.
	var out strings.Builder
	for i := 0; i < len(text); {
		if strings.HasPrefix(text[i:], "<redacted-") {
			if end := strings.IndexByte(text[i:], '>'); end >= 0 {
				out.WriteString(text[i : i+end+1])
				i += end + 1
				continue
			}
		}
		end, alias := i, ""
		if i == 0 || !nameRune(rune(text[i-1])) {
			node := r.root
			for j := i; j < len(text); j++ {
				node = node.next[text[j]]
				if node == nil {
					break
				}
				if node.alias != "" && (j+1 == len(text) || !nameRune(rune(text[j+1]))) {
					end, alias = j+1, node.alias
				}
			}
		}
		if alias != "" {
			out.WriteString(alias)
			i = end
		} else {
			out.WriteByte(text[i])
			i++
		}
	}
	value := out.String()
	value = networkText.ReplaceAllStringFunc(value, func(token string) string {
		// Evidence and validation commands contain dotted field paths, not
		// addresses. Keep only the exact field names emitted by the report.
		if token == "status.conditions" || token == "status.phase" || token == "spec.unschedulable" || token == "spec.nodeName" || token == "kubernetes.io" || token == "k8s.io" || strings.HasSuffix(token, ".kubernetes.io") || strings.HasSuffix(token, ".k8s.io") {
			return token
		}
		if strings.Contains(token, "://") || net.ParseIP(strings.Trim(token, "[]")) != nil || domainText.MatchString(token) {
			if alias, ok := r.addresses[token]; ok {
				return alias
			}
			alias := fmt.Sprintf("address-%03d", len(r.addresses)+1)
			r.addresses[token] = alias
			return alias
		}
		return token
	})
	return value
}

var domainText = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9.-]*\.[a-z]{2,}$`)
var networkText = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s;'"<>]+|[a-z0-9][a-z0-9.-]*\.[a-z]{2,}|(?:[0-9]{1,3}\.){3}[0-9]{1,3}|[0-9a-f]*:[0-9a-f:]+`)

func basicRedaction(report Report, s model.Snapshot) Report {
	extra := []identity{{"context", "", report.Context}, {"node", "", report.Node}}
	for _, f := range report.Findings {
		extra = append(extra, identity{findingKind(f), f.Namespace, f.Resource})
	}
	r := newRedactor(s, extra...)
	report.Redacted, report.CommandsRedacted, report.RedactionMode = true, true, "basic"
	report.Context = r.id("context", "", report.Context)
	report.Node = r.id("node", "", report.Node)
	for i := range report.Findings {
		f := &report.Findings[i]
		ns := f.Namespace
		kind := findingKind(*f)
		f.Namespace = r.id("namespace", "", ns)
		f.Resource = r.id(kind, ns, f.Resource)
		f.Reason = r.text(f.Reason)
		f.Evidence = r.text(f.Evidence)
		f.Recommendation = r.text(f.Recommendation)
	}
	for i := range report.Scheduling {
		p := &report.Scheduling[i]
		ns := p.Namespace
		p.Namespace = r.id("namespace", "", ns)
		p.Pod = r.id("pod", ns, p.Pod)
		for j := range p.Nodes {
			n := &p.Nodes[j]
			n.Node = r.id("node", "", n.Node)
			for k := range n.Reasons {
				n.Reasons[k] = r.text(n.Reasons[k])
			}
		}
	}
	for i, name := range report.Capacity.EligibleNodes {
		report.Capacity.EligibleNodes[i] = r.id("node", "", name)
	}
	report.Capacity.Detail = r.text(report.Capacity.Detail)
	report.Plan = r.text(report.Plan) + "\n\nBASIC REDACTION: Redacted commands are documentation-only and must not be executed verbatim."
	for resource, c := range report.Capabilities {
		c.Detail = r.text(c.Detail)
		report.Capabilities[resource] = c
	}
	limitation := "Basic redaction masks known snapshot identifiers, images, recorded paths and detected network addresses. Arbitrary business prose, encoded secrets and unknown identifiers may remain; review before sharing. Resource aliases are type-aware and deterministic for the same snapshot. Ambiguous untyped prose uses identifier aliases."
	if len(report.Limitations) > 3 {
		report.Limitations[3] = limitation
	} else {
		report.Limitations = append(report.Limitations, limitation)
	}
	return report
}
