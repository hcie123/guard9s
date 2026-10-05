package ui

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	core "k8s.io/api/core/v1"
)

type gatedSource struct {
	started                chan int
	release                chan struct{}
	calls, active, maximum atomic.Int32
}

func (s *gatedSource) Snapshot(ctx context.Context) (model.Snapshot, error) {
	call := s.calls.Add(1)
	active := s.active.Add(1)
	defer s.active.Add(-1)
	for {
		old := s.maximum.Load()
		if active <= old || s.maximum.CompareAndSwap(old, active) {
			break
		}
	}
	s.started <- int(call)
	if call == 1 {
		<-s.release
	} else {
		select {
		case <-s.release:
		case <-ctx.Done():
			return model.Snapshot{}, ctx.Err()
		}
	}
	out := demo.Snapshot("healthy")
	if call == 1 {
		out.Context = "older-synthetic"
	} else {
		out.Context = "latest-synthetic"
	}
	return out, nil
}
func TestRefreshBurstCoalescesAndLatestWins(t *testing.T) {
	u := New(demo.Snapshot("healthy"))
	source := &gatedSource{started: make(chan int, 10), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan result, 1)
	done := make(chan struct{})
	go func() { defer close(done); u.refreshWorker(ctx, source, u.snapshot, updates, func() {}) }()
	u.requestRefresh()
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	for range 100 {
		u.requestRefresh()
	}
	if len(u.refresh) != 1 {
		t.Fatal("pending requests grew")
	}
	close(source.release)
	select {
	case r := <-updates:
		if r.generation != u.refreshState.latest.Load() || r.snapshot.Context != "latest-synthetic" {
			t.Fatal("stale result published")
		}
		u.applyResult(r)
	case <-time.After(3 * time.Second):
		t.Fatal("latest result missing")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker leaked")
	}
	if source.calls.Load() != 2 || source.maximum.Load() != 1 || source.active.Load() != 0 {
		t.Fatalf("calls=%d concurrent=%d", source.calls.Load(), source.maximum.Load())
	}
	stale := analyze(context.Background(), demo.Snapshot("mixed"))
	stale.generation = 1
	u.applyResult(stale)
	if u.snapshot.Context != "latest-synthetic" {
		t.Fatal("queued stale result overwrote latest")
	}
}

type cancellationSource struct{ started, cancelled chan struct{} }

func (s cancellationSource) Snapshot(ctx context.Context) (model.Snapshot, error) {
	close(s.started)
	<-ctx.Done()
	close(s.cancelled)
	return model.Snapshot{}, ctx.Err()
}
func TestQuitWaitsForRefreshCancellation(t *testing.T) {
	u := New(demo.Snapshot("healthy"))
	screen := tcell.NewSimulationScreen("UTF-8")
	u.app.SetScreen(screen)
	drawn := make(chan struct{}, 1)
	u.app.SetAfterDrawFunc(func(tcell.Screen) {
		select {
		case drawn <- struct{}{}:
		default:
		}
	})
	s := cancellationSource{make(chan struct{}), make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- u.Run(context.Background(), s) }()
	select {
	case <-drawn:
	case <-time.After(time.Second):
		t.Fatal("no draw")
	}
	screen.InjectKey(tcell.KeyRune, 'r', 0)
	select {
	case <-s.started:
	case <-time.After(time.Second):
		t.Fatal("no refresh")
	}
	screen.InjectKey(tcell.KeyRune, 'q', 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("quit leaked worker")
	}
	select {
	case <-s.cancelled:
	default:
		t.Fatal("Run returned before worker cancellation")
	}
}
func TestFilterFieldsAndSortPreservation(t *testing.T) {
	u := New(demo.Snapshot("mixed"))
	u.command("risks")
	u.filter = "namespace:production risk>=HIGH category:storage"
	u.render()
	if len(u.rows) == 0 {
		t.Fatal("field filter empty")
	}
	for _, r := range u.rows {
		if r.namespace != "production" || r.severity < model.High || r.category != "Storage" {
			t.Fatal(r)
		}
	}
	u.filter = "risk<INFO"
	u.render()
	if len(u.rows) != 0 {
		t.Fatal("risk comparison")
	}
	u.filter = "unsupported:value"
	u.render()
	if len(u.rows) != 0 || !strings.Contains(u.message, "unsupported field") {
		t.Fatal("invalid filter silently accepted")
	}
	u.command("pods")
	u.filter = "name:nginx node=worker-01 status=Running"
	press(u, tcell.KeyRune, 's')
	if u.sorts["pods"] != "name" || len(u.rows) == 0 {
		t.Fatal("sort or field filter")
	}
	key := u.rows[0].key
	press(u, tcell.KeyEnter, 0)
	if !strings.Contains(u.detailView.GetText(false), "Candidate nodes") {
		t.Fatal("candidate explanations missing from pod detail")
	}
	press(u, tcell.KeyEscape, 0)
	refreshSnapshot(u, demo.Snapshot("mixed"))
	if u.filter == "" || u.sorts["pods"] != "name" || u.rows[0].key != key {
		t.Fatal("refresh lost filter/sort/selection")
	}
	for view, keys := range sortKeys {
		u.command(view)
		for range len(keys) {
			press(u, tcell.KeyRune, 's')
		}
		for _, key := range keys {
			u.sorts[view] = key
			u.render()
			for i := 1; i < len(u.rows); i++ {
				a, b := u.rows[i-1], u.rows[i]
				switch key {
				case "risk", "severity":
					if a.severity < b.severity {
						t.Fatal(key)
					}
				case "cpu":
					if a.cpu < b.cpu {
						t.Fatal(key)
					}
				case "memory":
					if a.memory < b.memory {
						t.Fatal(key)
					}
				case "pods":
					if a.podCount < b.podCount {
						t.Fatal(key)
					}
				case "restarts":
					if a.restarts < b.restarts {
						t.Fatal(key)
					}
				case "name", "resource":
					if a.name > b.name {
						t.Fatal(key)
					}
				case "category":
					if a.category > b.category {
						t.Fatal(key)
					}
				case "namespace":
					if a.namespace > b.namespace {
						t.Fatal(key)
					}
				}
			}
		}
	}
	u.command("events")
	press(u, tcell.KeyRune, 's')
	if !strings.Contains(u.message, "Sorting is available") {
		t.Fatal("unsupported sort")
	}
	for _, text := range []string{"risk=CRITICAL", "risk<=WARN", "risk>PASS", "namespace>demo", "risk:INVALID", "name:", "cpu:1"} {
		_, err := parseFilter(text)
		if (strings.Contains(text, "INVALID") || strings.Contains(text, ">") && strings.HasPrefix(text, "namespace") || text == "name:" || text == "cpu:1") && err == nil {
			t.Fatal(text)
		}
	}
}
func TestTerminalResizePreservesInteraction(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	s := demo.Snapshot("mixed")
	s.Pods[2].Status.ContainerStatuses[0].RestartCount = 10
	s.Pods[2].Status.ContainerStatuses[0].State = core.ContainerState{Waiting: &core.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
	s.Pods[2].Status.ContainerStatuses[0].LastTerminationState = core.ContainerState{Terminated: &core.ContainerStateTerminated{Reason: "OOMKilled"}}
	u := New(s)
	u.command("pods")
	u.filter = "nginx"
	u.sorts["pods"] = "name"
	u.render()
	u.table.Select(3, 0)
	key := u.rows[2].key
	press(u, tcell.KeyEnter, 0)
	u.detailView.ScrollTo(2, 0)
	for _, size := range [][2]int{{160, 44}, {80, 24}, {100, 30}, {120, 40}, {160, 44}} {
		screen.SetSize(size[0], size[1])
		u.resize(size[0])
		u.pages.SetRect(0, 0, size[0], size[1])
		u.pages.Draw(screen)
		screen.Show()
		r, _ := u.table.GetSelection()
		scroll, _ := u.detailView.GetScrollOffset()
		if u.rows[r-1].key != key || u.filter != "nginx" || u.view != "pods" || u.detailKey != key || scroll != 2 {
			t.Fatalf("state lost at %v", size)
		}
		x, y, w, h := u.pages.GetRect()
		if x < 0 || y < 0 || w < 0 || h < 0 {
			t.Fatal("negative layout")
		}
	}
	press(u, tcell.KeyEscape, 0)
	u.command("nodes")
	for _, size := range [][2]int{{80, 24}, {100, 30}, {120, 40}, {160, 44}} {
		screen.SetSize(size[0], size[1])
		u.resize(size[0])
		u.pages.SetRect(0, 0, size[0], size[1])
		u.pages.Draw(screen)
		screen.Show()
		cells, _, _ := screen.GetContents()
		var b strings.Builder
		for _, cell := range cells {
			b.WriteString(string(cell.Runes))
		}
		for _, text := range []string{"READ ONLY", "worker-01", "View: nodes"} {
			if !strings.Contains(b.String(), text) {
				t.Fatalf("%v missing %s", size, text)
			}
		}
	}
}
func TestRedrawRetainsSchedulingEvidence(t *testing.T) {
	u := New(demo.Snapshot("scheduling"))
	before := u.candidates
	for range 3 {
		u.render()
	}
	if !reflect.DeepEqual(before, u.candidates) {
		t.Fatal("redraw recomputed or altered candidate evidence")
	}
}
func FuzzFilterParser(f *testing.F) {
	f.Add("namespace:demo risk>=HIGH")
	f.Add("status:Pending")
	f.Add("risk>=\x00")
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 512 {
			return
		}
		filter, err := parseFilter(text)
		if err == nil {
			filter.matches(row{name: "synthetic", namespace: "demo", state: "Pending", severity: model.High})
		}
	})
}
