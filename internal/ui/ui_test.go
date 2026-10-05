package ui

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	"github.com/rivo/tview"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func press(u *UI, key tcell.Key, r rune) {
	e := u.key(tcell.NewEventKey(key, r, tcell.ModNone))
	if e != nil {
		if handler := u.app.GetFocus().InputHandler(); handler != nil {
			handler(e, func(p tview.Primitive) { u.app.SetFocus(p) })
		}
	}
}
func typeText(u *UI, s string) {
	for _, r := range s {
		press(u, tcell.KeyRune, r)
	}
}
func TestNavigationAndCommands(t *testing.T) {
	u := New(demo.Snapshot("mixed"))
	if len(u.rows) != 3 {
		t.Fatal("nodes missing")
	}
	press(u, tcell.KeyRune, 'j')
	if u.node != "worker-02" {
		t.Fatal("j did not move")
	}
	press(u, tcell.KeyRune, 'k')
	if u.node != "worker-01" {
		t.Fatal("k did not move")
	}
	press(u, tcell.KeyEnter, 0)
	if u.view != "pods" || len(u.rows) == 0 {
		t.Fatal("enter did not open node pods")
	}
	press(u, tcell.KeyEnter, 0)
	front, _ := u.pages.GetFrontPage()
	if front != "detail" {
		t.Fatal("pod evidence did not open")
	}
	press(u, tcell.KeyEscape, 0)
	for _, cmd := range []string{"nodes", "no", "pods", "po", "risks", "ri", "events", "ev", "e", "capacity", "pdb", "storage", "diagnosis", "plan"} {
		t.Run(cmd, func(t *testing.T) {
			press(u, tcell.KeyRune, ':')
			typeText(u, cmd)
			press(u, tcell.KeyEnter, 0)
			if len(u.rows) == 0 {
				t.Fatalf("view %s has no rows", cmd)
			}
		})
	}
	press(u, tcell.KeyRune, '?')
	front, _ = u.pages.GetFrontPage()
	if front != "detail" {
		t.Fatal("help missing")
	}
	press(u, tcell.KeyEscape, 0)
	for _, r := range []rune{'n', 'o', 'e', 'd', 'p', 'r'} {
		press(u, tcell.KeyRune, r)
	}
	press(u, tcell.KeyTAB, 0)
	if u.app.GetFocus() != u.sidebar {
		t.Fatal("sidebar focus")
	}
	press(u, tcell.KeyTAB, 0)
	u.command("invalid")
	if !strings.Contains(u.message, "Unknown") {
		t.Fatal("invalid command silently accepted")
	}
	u.command("help")
	press(u, tcell.KeyRune, 'q')
	front, _ = u.pages.GetFrontPage()
	if front != "main" {
		t.Fatal("q in help should go back")
	}
	press(u, tcell.KeyEscape, 0)
	if u.view != "nodes" {
		t.Fatal("Esc did not return to nodes")
	}
}
func TestInputSafetyFilterAndViews(t *testing.T) {
	u := New(demo.Snapshot("mixed"))
	press(u, tcell.KeyRune, '/')
	typeText(u, "worker-02")
	press(u, tcell.KeyEnter, 0)
	if len(u.rows) != 1 || u.rows[0].node != "worker-02" {
		t.Fatal("filter failed")
	}
	press(u, tcell.KeyEscape, 0)
	press(u, tcell.KeyRune, ':')
	typeText(u, "q:/")
	if u.input.GetText() != "q:/" || u.mode != "command" {
		t.Fatal("global hotkeys intercepted input")
	}
	press(u, tcell.KeyEscape, 0)
	u.filter = "no-match-at-all"
	u.render()
	if len(u.rows) != 0 || !strings.Contains(u.sidebar.GetText(false), "No matching") {
		t.Fatal("empty results")
	}
	press(u, tcell.KeyEnter, 0)
	press(u, tcell.KeyEscape, 0)
	u.snapshot.Namespace = "production"
	u.node = "worker-01"
	u.command("pods")
	for _, r := range u.rows {
		if r.cells[0] != "production" {
			t.Fatal("namespace display filter")
		}
	}
	u.command("diagnosis")
	foundDemo := false
	for _, r := range u.rows {
		if r.cells[1] == "demo" {
			foundDemo = true
		}
	}
	if !foundDemo {
		t.Fatal("diagnosis lost other namespace evidence")
	}
	u.compact = true
	u.command("nodes")
	if len(u.rows[0].cells) != 6 {
		t.Fatal("compact node layout")
	}
	u.command("pods")
	if len(u.rows[0].cells) != 4 {
		t.Fatal("compact pod layout")
	}
	if plain("safe\x1b\x00text") != "safetext" {
		t.Fatal("control characters")
	}
	if percent(1, 0) != "UNKNOWN" {
		t.Fatal("zero allocatable")
	}
	if age(time.Time{}, time.Now()) != "unknown" {
		t.Fatal("missing age")
	}
}
func TestScreenRenderingAndSnapshot(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(160, 44)
	u := New(demo.Snapshot("mixed"))
	u.pages.SetRect(0, 0, 160, 44)
	u.pages.Draw(screen)
	screen.Show()
	contents, w, h := screen.GetContents()
	var text strings.Builder
	for _, c := range contents {
		text.WriteString(string(c.Runes))
	}
	for _, want := range []string{"guard9s", "READ ONLY", "worker-01", "worker-02", "worker-03", "BLOCKED", "CRITICAL"} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("screen missing %q", want)
		}
	}
	if path := os.Getenv("GUARD9S_SCREENSHOT"); path != "" {
		if err := os.WriteFile(path, []byte(svg(contents, w, h)), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
func svg(cells []tcell.SimCell, w, h int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\"><title>guard9s actual tcell demo screen</title><rect width=\"100%%\" height=\"100%%\" fill=\"#000000\"/><g font-family=\"DejaVu Sans Mono,monospace\" font-size=\"14\">", w*9+32, h*20+32, w*9+32, h*20+32)
	for i, c := range cells {
		if len(c.Runes) == 0 {
			continue
		}
		x, y := (i%w)*9+16, (i/w)*20+16
		fg, bg, attr := c.Style.Decompose()
		if bg != tcell.ColorBlack && bg.Valid() {
			fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"9\" height=\"20\" fill=\"#%06x\"/>", x, y, bg.Hex())
		}
		s := string(c.Runes)
		if s == " " {
			continue
		}
		weight := "normal"
		if attr&tcell.AttrBold != 0 {
			weight = "bold"
		}
		color := fg.Hex()
		if !fg.Valid() {
			color = 0xffffff
		}
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" fill=\"#%06x\" font-weight=\"%s\">%s</text>", x, y+15, color, weight, html.EscapeString(s))
	}
	b.WriteString("</g></svg>\n")
	return b.String()
}

type staticSource struct{}

func (staticSource) Snapshot(context.Context) (model.Snapshot, error) {
	return demo.Snapshot("mixed"), nil
}
func TestRealEventLoopRefreshAndQuit(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	u := New(demo.Snapshot("mixed"))
	u.app.SetScreen(screen)
	started := make(chan struct{}, 1)
	u.app.SetAfterDrawFunc(func(tcell.Screen) {
		select {
		case started <- struct{}{}:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- u.Run(ctx, staticSource{}) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("TUI did not draw")
	}
	screen.InjectKey(tcell.KeyRune, 'r', 0)
	screen.InjectKey(tcell.KeyRune, 'q', 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TUI did not exit")
	}
}

func refreshSnapshot(u *UI, s model.Snapshot) {
	u.applyResult(analyze(context.Background(), s))
}

type failedSource struct{}

func (failedSource) Snapshot(context.Context) (model.Snapshot, error) {
	return model.Snapshot{}, errors.New("synthetic private endpoint")
}

func TestOpenPlanFollowsLatestEvidence(t *testing.T) {
	for _, cause := range []string{"blocker", "refresh_failure", "removed_node"} {
		t.Run(cause, func(t *testing.T) {
			u := New(demo.Snapshot("healthy"))
			u.command("plan")
			press(u, tcell.KeyEnter, 0)
			if !strings.Contains(u.detailView.GetText(false), "' drain '") {
				t.Fatal("healthy plan missing")
			}
			switch cause {
			case "blocker":
				refreshSnapshot(u, demo.Snapshot("mixed"))
			case "refresh_failure":
				for i := 0; i < 3; i++ {
					u.applyResult(collectResult(context.Background(), failedSource{}, u.snapshot))
				}
				if len(u.snapshot.Issues) != 1 {
					t.Fatal("refresh issues accumulated")
				}
			case "removed_node":
				s := demo.Snapshot("healthy")
				s.Nodes = s.Nodes[1:]
				refreshSnapshot(u, s)
			}
			text := u.detailView.GetText(false)
			if strings.Contains(text, "' drain '") || strings.Contains(text, "' cordon '") || strings.Contains(text, "synthetic private endpoint") {
				t.Fatalf("unsafe stale detail: %s", text)
			}
			if u.rows[0].cells[1] != "BLOCKED" && u.rows[0].cells[1] != "NOT RECOMMENDED" {
				t.Fatal("plan summary disagrees with detail")
			}
			refreshSnapshot(u, demo.Snapshot("healthy"))
			if !strings.Contains(u.detailView.GetText(false), "' drain '") || len(u.snapshot.Issues) != 0 {
				t.Fatal("successful refresh did not recover")
			}
		})
	}
}

func TestRefreshKeepsResourceSelectionAndScroll(t *testing.T) {
	s := demo.Snapshot("healthy")
	u := New(s)
	u.command("pods")
	u.table.Select(3, 0)
	key := u.rows[2].key
	u.sidebar.ScrollTo(2, 0)
	u.table.SetOffset(1, 0)
	extra := s.Pods[0].DeepCopy()
	extra.Name = "inserted-first"
	s.Pods = append([]*core.Pod{extra}, s.Pods...)
	refreshSnapshot(u, s)
	selected, _ := u.table.GetSelection()
	if selected != 4 || u.rows[selected-1].key != key {
		t.Fatal("refresh selected a different pod")
	}
	if scroll, _ := u.sidebar.GetScrollOffset(); scroll != 2 {
		t.Fatal("inspection scroll reset")
	}
	if scroll, _ := u.table.GetOffset(); scroll != 2 {
		t.Fatal("selected row moved on screen")
	}
	press(u, tcell.KeyEnter, 0)
	u.detailView.ScrollTo(3, 0)
	refreshSnapshot(u, s)
	if scroll, _ := u.detailView.GetScrollOffset(); scroll != 3 {
		t.Fatal("detail scroll reset")
	}
	var remaining []*core.Pod
	for _, p := range s.Pods {
		if p.Namespace+"/"+p.Name+"/"+string(p.UID) != key {
			remaining = append(remaining, p)
		}
	}
	s.Pods = remaining
	refreshSnapshot(u, s)
	if !strings.Contains(u.detailView.GetText(false), "no longer available") {
		t.Fatal("deleted pod detail remained visible")
	}
}

func TestPDBViewAndEmptyPlanAreConservative(t *testing.T) {
	s := demo.Snapshot("healthy")
	s.PDBs[0].Generation = s.PDBs[0].Status.ObservedGeneration + 1
	u := New(s)
	u.command("pdb")
	if len(u.rows) == 0 || u.rows[0].severity != model.High || !strings.Contains(strings.Join(u.rows[0].cells, " "), "UNKNOWN") || !strings.Contains(u.rows[0].detail, "UNKNOWN") {
		t.Fatal("stale positive PDB allowance displayed as safe")
	}
	u = New(model.Snapshot{})
	u.command("plan")
	if u.rows[0].cells[1] != "NOT RECOMMENDED" || strings.Contains(u.rows[0].detail, "' drain '") {
		t.Fatal("empty diagnosis showed READY")
	}
}

func TestCapacityViewUsesCurrentRefreshEvidence(t *testing.T) {
	u := New(demo.Snapshot("healthy"))
	u.command("capacity")
	press(u, tcell.KeyEnter, 0)
	if !strings.Contains(u.detailView.GetText(false), "Assessment: PASS") {
		t.Fatal("initial capacity missing")
	}
	s := demo.Snapshot("healthy")
	s.Pods[0].Spec.Containers[0].Resources.Requests[core.ResourceCPU] = resource.MustParse("100")
	refreshSnapshot(u, s)
	if u.rows[0].cells[7] != "HIGH" || !strings.Contains(u.detailView.GetText(false), "Assessment: HIGH") {
		t.Fatal("capacity cache retained an old PASS result")
	}
	u.applyResult(collectResult(context.Background(), failedSource{}, u.snapshot))
	if u.rows[0].cells[7] != "UNKNOWN" || !strings.Contains(u.detailView.GetText(false), "Assessment: UNKNOWN") {
		t.Fatal("failed collection retained capacity certainty")
	}
	refreshSnapshot(u, demo.Snapshot("healthy"))
	if u.rows[0].cells[7] != "PASS" {
		t.Fatal("capacity cache did not recover")
	}
	delete(u.capacity, "worker-01")
	u.render()
	if u.rows[0].cells[7] != "UNKNOWN" {
		t.Fatal("missing cached evidence looked safe")
	}
}

func TestFailedRefreshKeepsPriorSnapshotIsolated(t *testing.T) {
	s := demo.Snapshot("healthy")
	s.Issues = make([]string, 1, 4)
	s.Issues[0] = "existing watch issue"
	r := collectResult(context.Background(), failedSource{}, s)
	if len(r.snapshot.Issues) != 2 || len(s.Issues) != 1 || s.Issues[:2][1] != "" {
		t.Fatal("failed refresh mutated the prior issue slice")
	}
	if r.err == nil || r.capacity["worker-01"].Assessment != "UNKNOWN" {
		t.Fatal("failure result was not conservative")
	}
}
