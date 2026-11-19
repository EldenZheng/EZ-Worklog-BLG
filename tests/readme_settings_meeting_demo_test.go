//go:build demo

package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/menus/settings"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// These recordings render real controls off-screen with disposable sample data.
// No app executable, native picker or GitHub request is used.
func TestReadmeSettingsMeetingDemoFrames(t *testing.T) {
	frames := os.Getenv("DEMO_FRAMES")
	if frames == "" {
		t.Skip("DEMO_FRAMES not set")
	}
	t.Run("settings", func(t *testing.T) {
		a := test.NewApp()
		defer a.Quit()
		test.ApplyTheme(t, darkGold{shell.GoldTheme{Theme: theme.DefaultTheme()}})
		w := a.NewWindow(shell.WindowTitle)
		defer w.Close()
		w.Resize(fyne.NewSize(1680, 1050))
		ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, HasCfg: true,
			Cfg: backend.Config{GithubAuthor: "demo-user", WorklogOwner: "team-alex", DisplayName: "Alex",
				Repos: []string{"acme-corp", "acme-support"}, ProjectURL: "https://github.com/orgs/acme-corp/projects/1",
				MeetingRepo: "acme-corp/meetings", BulkCodeReviewRepo: "acme-corp/reviews", Currency: "USD",
				LookbackDays: 14, ArchiveDir: tempDir(t)}}
		var rows []backend.Row
		for i := 0; i < 12; i++ {
			rows = append(rows, backend.Row{"type": backend.KindOther, "date": "2026-11-18", "minutes": "60",
				"pushed_at": "2026-11-18T17:00:00", "issue": "acme-corp/web-app#412", "remarks": "- Finish invoice export and verify the PDF layout"})
		}
		rows = append(rows, backend.Row{"type": backend.KindMeeting, "date": "2026-11-19", "minutes": "30", "meeting_title": "Sprint planning", "remarks": "- Review priorities"})
		if _, err := ui.Store.AppendRows(rows); err != nil {
			t.Fatal(err)
		}
		if err := ui.Store.SetIgnored("sample-ignored-commit", true); err != nil {
			t.Fatal(err)
		}
		archive := backend.HistoryArchive{Version: 1, Owner: "team-alex", Projects: []string{"orgs/acme-corp/1"},
			From: backend.HistoryStartDate, Through: "2026-11-19", FetchedAt: "2026-11-19T17:00:00+08:00"}
		for i := 0; i < 120; i++ {
			archive.Items = append(archive.Items, backend.WorklogItem{Owner: "team-alex", Date: "2025-07-21", Minutes: 30,
				URL: fmt.Sprintf("https://github.com/acme-corp/web-app/issues/%d", 500+i), Title: "Invoice export worklog", Remarks: "- Review and verify invoice exports"})
		}
		if err := backend.SaveHistoryArchive(ui.Cfg, archive); err != nil {
			t.Fatal(err)
		}
		view := settings.BuildSettingsTab(ui)
		w.SetContent(view)
		// Display a fictional profile path instead of the renderer's temp path.
		walk(view, func(o fyne.CanvasObject) {
			if l, ok := o.(*widget.Label); ok && l.Text == backend.HistoryDirectory(ui.Cfg) {
				l.SetText(`C:\Users\Alex\AppData\Roaming\EZ-Worklog\history`)
			}
		})
		rec := newFrameRecorder(t, w, filepath.Join(frames, "settings-storage"), [2]float32{1150, 300})
		rec.stay(rec.shot(), 1900, false)
		var sidebar, main *container.Scroll
		walk(view, func(o fyne.CanvasObject) {
			if s, ok := o.(*container.Scroll); ok {
				if hasRenderedText(s.Content, "Storage") {
					sidebar = s
				} else if hasRenderedText(s.Content, "Sources") {
					main = s
				}
			}
		})
		if sidebar == nil || main == nil {
			t.Fatal("Settings scroll regions missing")
		}
		main.ScrollToBottom()
		rec.stay(rec.shot(), 1900, false)
		clear := storageCategoryClearButton(t, view, "Logged worklogs")
		rec.glide(rec.shot(), centreOf(clear), 250)
		test.Tap(clear)
		rec.stay(rec.shot(), 2200, true)
		tapStoragePopupButton(t, w.Canvas().Overlays().Top(), "Cancel")
		sidebar.ScrollToBottom()
		rec.stay(rec.shot(), 2000, false)
		var more *widget.Button
		walk(view, func(o fyne.CanvasObject) {
			if b, ok := o.(*widget.Button); ok && b.Text == "…" {
				more = b
			}
		})
		if more == nil {
			t.Fatal("backup menu missing")
		}
		rec.glide(rec.shot(), centreOf(more), 250)
		test.Tap(more)
		rec.stay(rec.shot(), 1800, true)
		tapStoragePopupButton(t, w.Canvas().Overlays().Top(), "Reallocate backup")
		walkRendered(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
			if e, ok := o.(*widget.Entry); ok {
				e.OnChanged = nil
				e.SetText(`C:\Users\Alex\AppData\Roaming\EZ-Worklog\history`)
			}
		})
		rec.stay(rec.shot(), 2400, true)
		// Stop at the reviewable choice; never open a native desktop picker.
		tapStoragePopupButton(t, w.Canvas().Overlays().Top(), "Cancel")
		rec.stay(rec.shot(), 1000, false)
		rec.finish([4]float32{0, 0, w.Canvas().Size().Width, w.Canvas().Size().Height})
	})
	t.Run("meeting", func(t *testing.T) {
		a := test.NewApp()
		defer a.Quit()
		test.ApplyTheme(t, darkGold{shell.GoldTheme{Theme: theme.DefaultTheme()}})
		ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: a.NewWindow(shell.WindowTitle),
			Cfg:      backend.Config{Repos: []string{"acme-corp"}, GithubAuthor: "demo-user", WorklogOwner: "team-alex", MeetingRepo: "acme-corp/meetings"},
			CalMonth: shell.ThisMonth(), RepMonth: shell.CurrentPeriod(), HasCfg: true}
		defer ui.Win.Close()
		ui.Win.SetContent(logwork.BuildLogTab(ui))
		ui.Win.Resize(fyne.NewSize(1680, 1050))
		view := ui.Win.Content()
		manualKindPicker(t, view).SetSelected("Meeting")
		entryByPlaceholder(view, "Meeting title (e.g. Sprint planning)").SetText("Sprint planning: invoice exports")
		entryByPlaceholder(view, "minutes").SetText("45")
		notes := entryByPlaceholder(view, "- one bullet per thing done; this becomes the worklog remarks")
		notes.SetText("- Review invoice export milestones and assign follow-up actions.\n- Agree the sprint scope and the test plan for the PDF rollout.")
		rec := newFrameRecorder(t, ui.Win, filepath.Join(frames, "meeting-worklog"), [2]float32{1150, 380})
		rec.stay(rec.shot(), 2100, false)
		recording := entryByPlaceholder(view, "https://… (meeting recording)")
		rec.glide(rec.shot(), centreOf(recording), 300)
		recording.SetText("https://example.com/recordings/sprint-planning")
		rec.stay(rec.shot(), 1900, true)
		notes.SetText(strings.Repeat("Review invoice export scope, PDF layout checks, rollout owners and follow-up actions. ", 13))
		driver := collectMotion(t)
		check := draftSplitCheck(view)
		if check == nil {
			t.Fatal("split remarks control missing")
		}
		rec.glide(rec.shot(), centreOf(check), 400)
		check.SetChecked(true)
		for i := 1; i <= 8; i++ {
			for _, anim := range driver.anims {
				anim.Tick(float32(i) / 8)
			}
			rec.stay(rec.shot(), 50, i == 1)
		}
		var pane *logwork.CommitTextPane
		walk(view, func(o fyne.CanvasObject) {
			if p, ok := o.(*logwork.CommitTextPane); ok && p.Visible() && p.Remarks.Visible() {
				pane = p
			}
		})
		if pane == nil {
			t.Fatal("separate remarks did not open")
		}
		multiLineEntry(pane.Remarks).SetText("- Planned invoice export milestones, owners and PDF rollout checks.")
		rec.stay(rec.shot(), 2600, false)
		rec.finish([4]float32{0, 0, ui.Win.Canvas().Size().Width, 700})
	})
}
