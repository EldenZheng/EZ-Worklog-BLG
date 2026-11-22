package shell

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"worklog/internal/shared/backend"
)

func TestPendingTitlesAreReadyBeforeOneCombinedDraw(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	oldGh, oldInline := backend.Gh, RunAsyncInline
	oldRecent, oldPending := Hooks.DrawRecent, Hooks.RenderPending
	defer func() {
		backend.Gh, RunAsyncInline = oldGh, oldInline
		Hooks.DrawRecent, Hooks.RenderPending = oldRecent, oldPending
	}()
	RunAsyncInline = true
	backend.Gh = func([]string, ...string) (string, error) {
		return `{"title":"Ready title","url":"https://github.com/startup-probe/app/issues/42"}`, nil
	}
	ui := &UI{Pending: backend.PendingResult{Groups: []backend.Group{{Issue: "startup-probe/app#42"}}}}
	completed := false
	recent, pending := 0, 0
	Hooks.DrawRecent = func(*UI) {
		if !completed {
			t.Error("saved entries redrawn before pending titles finished")
		}
		recent++
	}
	Hooks.RenderPending = func(*UI) {
		if !completed {
			t.Error("pending entries redrawn before titles finished")
		}
		pending++
	}
	ui.loadIssueInfos(NewProgressComposer(func(backend.FetchProgress) {}), 20, func() {
		completed = true
		Hooks.RenderPending(ui)
		Hooks.DrawRecent(ui)
	})
	if !completed || recent != 1 || pending != 1 {
		t.Fatalf("title completion drew pending=%d, saved=%d; want once each", pending, recent)
	}
	if ui.IssueInfo["startup-probe/app#42"].Title != "Ready title" {
		t.Fatal("combined draw lost the fetched issue title")
	}
}
