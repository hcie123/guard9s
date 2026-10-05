// Package ui implements a keyboard-first, display-only terminal application.
package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/hcie123/guard9s/internal/analyzer"
	"github.com/hcie123/guard9s/internal/model"
	"github.com/rivo/tview"
)

type Source interface {
	Snapshot(context.Context) (model.Snapshot, error)
}
type row struct {
	cells                            []string
	detail, node, key                string
	severity                         model.Severity
	namespace, name, state, category string
	cpu, memory, podCount, restarts  int64
	candidate                        *analyzer.Feasibility
}

func (r row) evidence() string {
	if r.candidate != nil {
		return r.detail + "\n\n" + analyzer.CandidateText(*r.candidate)
	}
	return r.detail
}

type result struct {
	snapshot   model.Snapshot
	findings   map[string][]model.Finding
	capacity   map[string]analyzer.Capacity
	candidates map[string]map[string]analyzer.Feasibility
	err        error
	generation uint64
}
type UI struct {
	left                              *tview.Flex
	preview                           *tview.TextView
	app                               *tview.Application
	root                              *tview.Flex
	body                              *tview.Flex
	compact                           bool
	pages                             *tview.Pages
	header, sidebar, status, footer   *tview.TextView
	table                             *tview.Table
	input                             *tview.InputField
	snapshot                          model.Snapshot
	findings                          map[string][]model.Finding
	capacity                          map[string]analyzer.Capacity
	candidates                        map[string]map[string]analyzer.Feasibility
	view, node, filter, mode, message string
	rows                              []row
	refresh                           chan struct{}
	refreshState                      refreshState
	sorts                             map[string]string
	width                             int
	rendering                         bool
	renderedView                      string
	detailView                        *tview.TextView
	detailKey                         string
}

func New(s model.Snapshot) *UI {
	r := analyze(context.Background(), s)
	u := &UI{app: tview.NewApplication(), snapshot: r.snapshot, view: "nodes", findings: r.findings, capacity: r.capacity, candidates: r.candidates, refresh: make(chan struct{}, 1), sorts: map[string]string{"risks": "severity"}, width: 160}
	if len(s.Nodes) > 0 {
		u.node = s.Nodes[0].Name
	}
	u.header = tview.NewTextView().SetDynamicColors(true)
	u.sidebar = tview.NewTextView().SetDynamicColors(false).SetScrollable(true).SetWrap(true)
	u.sidebar.SetBorder(true).SetTitle(" INSPECT ").SetBorderColor(tcell.ColorDarkCyan)
	u.table = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false).SetSeparator(' ').SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite))
	u.table.SetBorder(true).SetBorderColor(tcell.ColorDarkCyan)
	u.status = tview.NewTextView().SetDynamicColors(true)
	u.footer = tview.NewTextView().SetDynamicColors(true)
	u.input = tview.NewInputField().SetFieldBackgroundColor(tcell.ColorDarkSlateGray).SetFieldTextColor(tcell.ColorWhite)
	u.input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			u.closeInput()
			return
		}
		if key == tcell.KeyEnter {
			text := u.input.GetText()
			mode := u.mode
			u.closeInput()
			if mode == "filter" {
				u.filter = text
				u.message = ""
				u.render()
			} else {
				u.command(text)
			}
		}
	})
	u.preview = tview.NewTextView().SetWrap(true)
	u.preview.SetBorder(true).SetTitle(" PRIORITY FINDINGS ").SetBorderColor(tcell.ColorDarkCyan)
	u.left = tview.NewFlex().SetDirection(tview.FlexRow).AddItem(u.table, 9, 0, true).AddItem(u.preview, 0, 1, false)
	body := tview.NewFlex().AddItem(u.left, 0, 3, true).AddItem(u.sidebar, 0, 1, false)
	u.body = body
	u.root = tview.NewFlex().SetDirection(tview.FlexRow).AddItem(u.header, 3, 0, false).AddItem(body, 0, 1, true).AddItem(u.status, 1, 0, false).AddItem(u.footer, 1, 0, false)
	u.pages = tview.NewPages().AddPage("main", u.root, true, true)
	u.table.SetSelectionChangedFunc(func(r, c int) { u.selectRow(r) })
	u.app.SetRoot(u.pages, true).SetFocus(u.table).SetInputCapture(u.key)
	u.render()
	return u
}

func analyze(ctx context.Context, s model.Snapshot) result {
	if ctx.Err() != nil {
		return result{snapshot: s, err: ctx.Err()}
	}
	s = s.Indexed()
	out := result{snapshot: s, findings: map[string][]model.Finding{}, capacity: map[string]analyzer.Capacity{}, candidates: map[string]map[string]analyzer.Feasibility{}}
	for name, d := range analyzer.AnalyzeAll(ctx, s) {
		out.findings[name], out.capacity[name] = d.Findings, d.Capacity
		out.candidates[name] = d.Candidates
	}
	return out
}

func collectResult(ctx context.Context, source Source, previous model.Snapshot) result {
	s, err := source.Snapshot(ctx)
	if ctx.Err() != nil {
		return result{snapshot: previous, err: ctx.Err()}
	}
	if err != nil {
		s = previous
		s.Capabilities = make(map[string]model.Capability, len(previous.Capabilities))
		for resource, c := range previous.Capabilities {
			if c.State == model.Available {
				c.State, c.Detail = model.Unavailable, "latest refresh failed; prior inventory is retained"
			}
			s.Capabilities[resource] = c
		}
		s.Issues = slices.Clone(previous.Issues)
		if !slices.Contains(s.Issues, "Latest refresh failed") {
			s.Issues = append(s.Issues, "Latest refresh failed")
		}
	}
	r := analyze(ctx, s)
	r.err = err
	return r
}

// Run updates only from informer cache; all widget mutation stays on tview's loop.
// A nonblocking screen event wakes the loop without QueueUpdateDraw shutdown races.
func (u *UI) Run(ctx context.Context, source Source) error {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	updates := make(chan result, 1)
	var start sync.Once
	u.app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		width, _ := screen.Size()
		u.resize(width)
		start.Do(func() {
			previous := u.snapshot
			workers.Add(2)
			go func() {
				defer workers.Done()
				u.refreshWorker(ctx, source, previous, updates, func() { _ = screen.PostEvent(tcell.NewEventKey(tcell.KeyF24, 0, tcell.ModNone)) })
			}()
			go func() {
				defer workers.Done()
				ticker := time.NewTicker(10 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						u.requestRefresh()
					}
				}
			}()
		})
		select {
		case r := <-updates:
			u.applyResult(r)
		default:
		}
		return false
	})
	workers.Add(1)
	go func() { defer workers.Done(); <-ctx.Done(); u.app.Stop() }()
	return u.app.Run()
}

func (u *UI) applyResult(r result) {
	if r.generation != 0 && r.generation != u.refreshState.latest.Load() {
		return
	}
	u.snapshot, u.findings, u.capacity = r.snapshot, r.findings, r.capacity
	u.candidates = r.candidates
	if r.err != nil {
		u.message = "Refresh failed; diagnosis is UNKNOWN. Reconnect and retry."
	} else {
		u.message = "Cache refreshed"
	}
	u.render()
}
func (u *UI) resize(width int) {
	if width == u.width {
		return
	}
	u.width = width
	u.compact = width < 120
	if u.compact {
		u.body.ResizeItem(u.sidebar, 0, 0)
	} else {
		u.body.ResizeItem(u.sidebar, 0, 1)
	}
	u.render()
}

func (u *UI) key(e *tcell.EventKey) *tcell.EventKey {
	if e.Key() == tcell.KeyCtrlC {
		u.app.Stop()
		return nil
	}
	if u.mode != "" {
		return e
	} // q, :, / are ordinary text while typing.
	if front, _ := u.pages.GetFrontPage(); front != "main" {
		if e.Key() == tcell.KeyEscape || e.Rune() == 'q' {
			u.pages.RemovePage("detail")
			u.detailView = nil
			u.detailKey = ""
			u.app.SetFocus(u.table)
			return nil
		}
		return e
	}
	if e.Key() == tcell.KeyEscape {
		u.filter = ""
		u.message = ""
		u.view = "nodes"
		u.render()
		return nil
	}
	if e.Key() == tcell.KeyEnter {
		r, _ := u.table.GetSelection()
		if r > 0 && r <= len(u.rows) {
			if u.view == "nodes" {
				u.node = u.rows[r-1].node
				u.view = "pods"
				u.filter = ""
				u.render()
			} else {
				u.modal(" Resource detail ", u.rows[r-1].evidence())
				u.detailKey = u.rows[r-1].key
			}
		}
		return nil
	}
	if e.Key() == tcell.KeyTAB {
		if u.compact {
			u.app.SetFocus(u.table)
			return nil
		}
		if u.app.GetFocus() == u.sidebar {
			u.app.SetFocus(u.table)
		} else {
			u.app.SetFocus(u.sidebar)
		}
		return nil
	}
	if e.Key() == tcell.KeyF24 {
		return nil
	}
	switch e.Rune() {
	case 'q':
		u.app.Stop()
	case 'j':
		return tcell.NewEventKey(tcell.KeyDown, 0, e.Modifiers())
	case 'k':
		return tcell.NewEventKey(tcell.KeyUp, 0, e.Modifiers())
	case '/':
		u.openInput("filter")
	case ':':
		u.openInput("command")
	case '?':
		u.modal(" Help ", help)
	case 'r':
		u.message = "Refreshing cache..."
		u.requestRefresh()
		u.render()
	case 's':
		u.cycleSort()
		u.render()
	case 'n':
		u.command("nodes")
	case 'o':
		u.command("pods")
	case 'e':
		u.command("events")
	case 'd':
		u.command("diagnosis")
	case 'p':
		u.command("plan")
	default:
		return e
	}
	return nil
}
func (u *UI) openInput(mode string) {
	u.mode = mode
	label := ": "
	if mode == "filter" {
		label = "/ "
	}
	u.input.SetLabel(label).SetText("")
	u.root.RemoveItem(u.footer).AddItem(u.input, 1, 0, true)
	u.app.SetFocus(u.input)
}
func (u *UI) closeInput() {
	u.mode = ""
	u.root.RemoveItem(u.input).AddItem(u.footer, 1, 0, false)
	u.app.SetFocus(u.table)
}
func (u *UI) command(cmd string) {
	cmd = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(cmd, ":")))
	aliases := map[string]string{"no": "nodes", "po": "pods", "ev": "events", "e": "events", "ri": "risks"}
	if full, ok := aliases[cmd]; ok {
		cmd = full
	}
	switch cmd {
	case "nodes", "pods", "events", "risks", "capacity", "pdb", "storage", "diagnosis", "plan":
		u.view = cmd
		u.filter = ""
		u.message = ""
		u.render()
	case "help":
		u.modal(" Help ", help)
	default:
		u.message = "Unknown command. Use :help"
		u.render()
	}
}
func (u *UI) modal(title, text string) {
	v := tview.NewTextView().SetText(detailText(text)).SetScrollable(true).SetWrap(true)
	v.SetBorder(true).SetTitle(title).SetBorderColor(tcell.ColorAqua)
	u.detailView = v
	u.detailKey = ""
	u.pages.AddPage("detail", v, true, true)
	u.app.SetFocus(v)
}

func detailText(text string) string {
	return plain(text) + "\n\nEsc: back | j/k or arrows: scroll"
}

func (u *UI) refreshDetail(rows []row) {
	if u.detailView == nil || u.detailKey == "" {
		return // Help is static.
	}
	text := "This resource or finding is no longer available in the latest snapshot. Close this panel and re-run diagnosis."
	for _, r := range rows {
		if r.key == u.detailKey {
			text = r.evidence()
			break
		}
	}
	r, c := u.detailView.GetScrollOffset()
	u.detailView.SetText(detailText(text)).ScrollTo(r, c)
}

func (u *UI) selectRow(r int) {
	if u.rendering {
		return
	}
	if r < 1 || r > len(u.rows) {
		u.sidebar.SetText("No matching resources. Esc clears the filter.")
		u.preview.SetText("")
		return
	}
	x := u.rows[r-1]
	if x.node != "" {
		u.node = x.node
	}
	u.sidebar.SetText(plain(x.evidence())).ScrollToBeginning()
	if u.view == "nodes" {
		var b strings.Builder
		fmt.Fprintf(&b, " Maintenance: %s\n All namespaces evaluated · evidence from current snapshot\n\n", model.Readiness(u.findings[u.node]))
		count := 0
		for _, f := range u.findings[u.node] {
			if f.Severity < model.Warn && !f.Unknown {
				continue
			}
			fmt.Fprintf(&b, " %s · %s · %s/%s\n %s\n Evidence: %s\n\n", f.Severity, f.Category, f.Namespace, f.Resource, f.Reason, f.Evidence)
			count++
			if count == 5 {
				break
			}
		}
		if count == 0 {
			b.WriteString(" No blocking or warning findings in this snapshot.\n\n Revalidate PDBs, capacity and application health before maintenance.")
		}
		b.WriteString("\n d: complete diagnosis   p: maintenance plan   Enter: node workloads")
		u.preview.SetText(plain(b.String())).ScrollToBeginning()
	}
	u.updateHeader()
}
func (u *UI) render() {
	if u.view == "nodes" {
		u.left.ResizeItem(u.table, 9, 0)
		u.left.ResizeItem(u.preview, 0, 1)
	} else {
		u.left.ResizeItem(u.table, 0, 1)
		u.left.ResizeItem(u.preview, 0, 0)
	}
	selected, _ := u.table.GetSelection()
	oldSelected := selected
	selectedKey := ""
	if u.renderedView == u.view && selected > 0 && selected <= len(u.rows) {
		selectedKey = u.rows[selected-1].key
	}
	sideRow, sideCol := u.sidebar.GetScrollOffset()
	previewRow, previewCol := u.preview.GetScrollOffset()
	tableRow, tableCol := u.table.GetOffset()
	u.rendering = true // Clear/Select callbacks must not change the target mid-render.
	headers, rows := u.buildRows()
	for i := range rows {
		r := &rows[i]
		if u.view == "pods" || r.category == "Scheduling" {
			if f, ok := u.candidates[r.node][r.namespace+"/"+r.name]; ok {
				r.candidate = &f
			}
		}
	}
	u.sortRows(rows)
	filter, filterErr := parseFilter(u.filter)
	if filterErr != nil {
		u.message = filterErr.Error()
	}
	u.rows = nil
	for _, x := range rows {
		if filterErr == nil && filter.matches(x) {
			u.rows = append(u.rows, x)
		}
	}
	u.table.Clear()
	u.table.SetTitle(" " + strings.ToUpper(u.view) + " ")
	for c, h := range headers {
		u.table.SetCell(0, c, tview.NewTableCell(h).SetSelectable(false).SetTextColor(tcell.ColorAqua).SetAttributes(tcell.AttrBold))
	}
	for r, x := range u.rows {
		for c, s := range x.cells {
			maxWidth := 44
			if u.compact {
				maxWidth = max(3, (u.width-4)/max(1, len(headers))-1)
			}
			u.table.SetCell(r+1, c, tview.NewTableCell(tview.Escape(plain(s))).SetTextColor(color(x.severity)).SetExpansion(1).SetMaxWidth(maxWidth))
		}
	}
	selected = 1
	for i, x := range u.rows {
		if (selectedKey != "" && x.key == selectedKey) || (selectedKey == "" && u.view == "nodes" && x.node == u.node) {
			selected = i + 1
			break
		}
	}
	u.rendering = false
	u.renderedView = u.view
	if len(u.rows) > 0 {
		u.table.Select(selected, 0)
		u.selectRow(selected)
		if selectedKey != "" && u.rows[selected-1].key == selectedKey {
			u.sidebar.ScrollTo(sideRow, sideCol)
			u.preview.ScrollTo(previewRow, previewCol)
			u.table.SetOffset(max(0, tableRow+selected-oldSelected), tableCol)
		} else {
			u.table.ScrollToBeginning()
		}
	} else {
		u.selectRow(0)
	}
	u.refreshDetail(rows)
	warnings, critical := 0, 0
	for _, f := range u.findings {
		for _, x := range f {
			if x.Severity == model.Warn {
				warnings++
			}
			if x.Severity == model.Critical {
				critical++
			}
		}
	}
	u.updateHeader()
	u.status.SetText(fmt.Sprintf(" [aqua]%s[-] | NS:%s | Nodes:%d Pods:%d | WARN:%d CRIT:%d | [green]READ ONLY[-] | %s", tview.Escape(plain(u.snapshot.Context)), tview.Escape(plain(u.snapshot.Namespace)), len(u.snapshot.Nodes), len(u.snapshot.Pods), warnings, critical, tview.Escape(plain(u.message))))
	u.footer.SetText(" [yellow]j/k[-] Move  [yellow]Enter[-] Detail  [yellow]/[-] Filter  [yellow]:[-] Command  [yellow]d[-] Diagnose  [yellow]p[-] Plan  [yellow]e[-] Events  [yellow]r[-] Refresh  [yellow]?[-] Help  [yellow]q[-] Quit")
	if u.compact {
		u.footer.SetText(" j/k Move  Enter Detail  / Filter  s Sort  : Views  d/p Dx/Plan  r Refresh  q Quit")
	}
}
func color(s model.Severity) tcell.Color {
	return [...]tcell.Color{tcell.ColorLightGreen, tcell.ColorLightGray, tcell.ColorYellow, tcell.ColorOrangeRed, tcell.ColorRed}[s]
}
func plain(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' || r == 127 {
			return -1
		}
		return r
	}, s)
}

const help = `guard9s — Kubernetes SRE Terminal UI

Navigation
  j / k / arrows  Move or scroll
  Enter           Node → Pods; resource → evidence
  Esc             Back to Nodes / clear filter
  Tab             Focus table or inspection panel
  /               Filter current view (Enter applies; Esc cancels)
  s               Cycle sort order (retained on refresh)
                  Filter: namespace:demo status:Pending risk>=HIGH
                  category:Storage node:worker-01 name:nginx
  :               Command input

Views
  n / :nodes      Nodes (:no)
  o / :pods       Pods on selected node (:po)
  :risks          Cluster risks, severity sorted (:ri)
  e / :events     Cluster warning and relevant events (:ev, :e)
  :capacity       Relocation CPU, memory and pod slots
  :pdb            Budgets matched to selected node's pods
  :storage        Storage evidence for selected node
  d               Full node diagnosis
  p               Suggested maintenance plan

General
  r               Refresh from informer cache (also every 10 seconds)
  ? / :help       Help
  q / Ctrl+C      Quit (q is literal in input fields)

Safety
  Every command is display-only. guard9s never executes kubectl.
  UNKNOWN evidence prevents a READY recommendation.
  Scheduling/capacity is best effort, not a scheduler simulation.
  Namespace filters display; diagnosis always uses ALL namespaces.
  Risk counts represent findings per node, not unique incidents.`

func (u *UI) updateHeader() {
	u.header.SetText(fmt.Sprintf("[aqua::b] guard9s [-::-] [gray]SRE maintenance intelligence[-]   [green::b]READ ONLY[-::-]\n Context: %s | Namespace: %s | Node: %s\n View: %s | Filter: %s | Sort: %s", tview.Escape(plain(u.snapshot.Context)), tview.Escape(plain(u.snapshot.Namespace)), tview.Escape(plain(u.node)), u.view, tview.Escape(plain(u.filter)), u.sorts[u.view]))
}
