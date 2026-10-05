package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hcie123/guard9s/internal/model"
)

type filterTerm struct {
	field, op, value string
	level            model.Severity
}
type rowFilter []filterTerm

func parseFilter(text string) (rowFilter, error) {
	var out rowFilter
	for _, token := range strings.Fields(strings.ToLower(text)) {
		t := filterTerm{value: token}
		for _, op := range []string{">=", "<=", ">", "<", "=", ":"} {
			if i := strings.Index(token, op); i > 0 {
				t.field, t.op, t.value = token[:i], op, token[i+len(op):]
				break
			}
		}
		if t.field != "" {
			switch t.field {
			case "namespace", "status", "category", "node", "name":
				if t.op != ":" && t.op != "=" {
					return nil, fmt.Errorf("Filter: use : or = for %s", t.field)
				}
			case "risk":
				level, ok := severity(t.value)
				if !ok {
					return nil, fmt.Errorf("Filter: risk must be PASS, INFO, WARN, HIGH or CRITICAL")
				}
				t.level = level
			default:
				return nil, fmt.Errorf("Filter: unsupported field %s", t.field)
			}
			if t.value == "" {
				return nil, fmt.Errorf("Filter: missing value for %s", t.field)
			}
		}
		out = append(out, t)
	}
	return out, nil
}
func severity(value string) (model.Severity, bool) {
	for _, s := range []model.Severity{model.Pass, model.Info, model.Warn, model.High, model.Critical} {
		if strings.EqualFold(value, s.String()) {
			return s, true
		}
	}
	return model.Pass, false
}
func (f rowFilter) matches(r row) bool {
	for _, t := range f {
		if t.field == "risk" {
			ok := false
			switch t.op {
			case ">=":
				ok = r.severity >= t.level
			case "<=":
				ok = r.severity <= t.level
			case ">":
				ok = r.severity > t.level
			case "<":
				ok = r.severity < t.level
			default:
				ok = r.severity == t.level
			}
			if !ok {
				return false
			}
			continue
		}
		var value string
		switch t.field {
		case "namespace":
			value = r.namespace
		case "name":
			value = r.name
		case "status":
			value = r.state
		case "category":
			value = r.category
		case "node":
			value = r.node
		default:
			value = strings.Join(r.cells, " ") + " " + r.detail
		}
		value = strings.ToLower(value)
		if t.op == "=" {
			if value != t.value {
				return false
			}
		} else if !strings.Contains(value, t.value) {
			return false
		}
	}
	return true
}

var sortKeys = map[string][]string{"nodes": {"name", "risk", "cpu", "memory", "pods"}, "pods": {"name", "namespace", "risk", "restarts", "cpu", "memory"}, "risks": {"severity", "namespace", "category", "resource"}}

func (u *UI) cycleSort() {
	keys := sortKeys[u.view]
	if len(keys) == 0 {
		u.message = "Sorting is available in Nodes, Pods and Risks"
		return
	}
	index := -1
	for i, key := range keys {
		if key == u.sorts[u.view] {
			index = i
		}
	}
	u.sorts[u.view] = keys[(index+1)%len(keys)]
	u.message = "Sort: " + u.sorts[u.view]
}
func (u *UI) sortRows(rows []row) {
	key := u.sorts[u.view]
	if key == "" {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch key {
		case "risk", "severity":
			if a.severity != b.severity {
				return a.severity > b.severity
			}
		case "cpu":
			if a.cpu != b.cpu {
				return a.cpu > b.cpu
			}
		case "memory":
			if a.memory != b.memory {
				return a.memory > b.memory
			}
		case "pods":
			if a.podCount != b.podCount {
				return a.podCount > b.podCount
			}
		case "restarts":
			if a.restarts != b.restarts {
				return a.restarts > b.restarts
			}
		case "namespace":
			if a.namespace != b.namespace {
				return a.namespace < b.namespace
			}
		case "category":
			if a.category != b.category {
				return a.category < b.category
			}
		case "name", "resource":
			if a.name != b.name {
				return a.name < b.name
			}
		}
		return a.key < b.key
	})
}
