//go:build demo

package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/assemble"
	"worklog/internal/menus/projects"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// These walkthroughs use the real Projects page and an entirely fictional
// GitHub response. No request, including an accidental mutation, goes online.
func TestReadmeProjectsDemoFrames(t *testing.T) {
	out, frames := os.Getenv("DEMO_OUT"), os.Getenv("DEMO_FRAMES")
	if out == "" || frames == "" {
		t.Skip("DEMO_OUT and DEMO_FRAMES not set")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.Local)
	oldNow, oldGh := projects.Now, backend.Gh
	projects.Now = func() time.Time { return now }
	defer func() { projects.Now, backend.Gh = oldNow, oldGh }()
	answers := projectDemoAnswers(t, now)
	backend.Gh = func(args []string, _ ...string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "subIssues") {
			for number, response := range answers {
				if slices.Contains(args, "num="+number) {
					return response, nil
				}
			}
		}
		if strings.Contains(joined, "assignableUsers") {
			return `{"data":{"repository":{"assignableUsers":{"nodes":[{"login":"ana"},{"login":"bea"},{"login":"chen"}]}}}}`, nil
		}
		if len(args) > 1 && args[0] == "api" && strings.HasSuffix(args[1], "/repos") {
			return "", nil
		}
		if slices.Equal(args, []string{"api", "user", "--jq", ".login"}) {
			return "demo-user", nil
		}
		t.Errorf("unexpected GitHub call in documentation demo: %v", args)
		return "", fmt.Errorf("documentation demo is offline")
	}

	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, darkGold{shell.GoldTheme{Theme: theme.DefaultTheme()}})
	w := a.NewWindow(shell.WindowTitle)
	defer w.Close()
	w.Resize(fyne.NewSize(1680, 1050))
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w,
		CalMonth: "2026-11", RepMonth: "2026-11", HasCfg: true, ProfileLoaded: true}
	ui.Cfg = backend.Config{Repos: []string{"acme-corp", "acme-support"}, WorklogOwner: "demo-user", GithubAuthor: "demo-user"}
	if err := projects.Save(ui.Store, projects.Saved{Projects: []projects.Project{
		{ID: "platform", Name: "Customer Platform", Issues: []string{"acme-corp/web-app#100", "acme-support/helpdesk#200"}},
		{ID: "portal", Name: "Support Portal", Issues: []string{"acme-support/helpdesk#200"}},
	}}); err != nil {
		t.Fatal(err)
	}
	assemble.BuildAllTabs(ui)
	w.SetContent(ui.Tabs)
	openProjects(ui)
	page := projectsPage(ui)
	if !contains(labels(page), "Burn-up") {
		t.Fatal("fictional project did not load")
	}
	rec := newFrameRecorder(t, w, filepath.Join(frames, "projects"), [2]float32{1100, 750})
	base := rec.shot()
	if err := copyFile(filepath.Join(rec.dir, base), filepath.Join(out, "12-projects.png")); err != nil {
		t.Fatal(err)
	}
	rec.stay(base, 2400, false)
	for _, title := range []string{"Open", "Forecast"} {
		tap := tappableWith(page, title)
		if tap == nil {
			t.Fatal("missing project tile", title)
		}
		rec.glide(rec.shot(), centreOf(tap), 300)
		test.Tap(tap)
		rec.stay(rec.shot(), 2400, true)
		if title == "Forecast" {
			var pace *widget.Entry
			walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
				if e, ok := o.(*widget.Entry); ok && strings.HasPrefix(e.PlaceHolder, "issues a week") {
					pace = e
				}
			})
			if pace == nil {
				t.Fatal("forecast calculator missing")
			}
			rec.glide(rec.shot(), centreOf(pace), 250)
			pace.SetText("4")
			rec.stay(rec.shot(), 2200, true)
		}
		closeProjectDemoPopup(t, rec)
	}
	rec.stay(rec.shot(), 1800, false)
	rec.finish([4]float32{0, 0, w.Canvas().Size().Width, w.Canvas().Size().Height})

	// Open the real folding section, search the table, then switch to Gantt.
	rec = newFrameRecorder(t, w, filepath.Join(frames, "project-gantt"), [2]float32{1100, 750})
	rec.stay(rec.shot(), 1500, false)
	fold := tappableWith(page, "Issue List/Gantt Chart")
	if fold == nil {
		t.Fatal("issue section heading missing")
	}
	rec.glide(rec.shot(), centreOf(fold), 300)
	test.Tap(fold)
	var issueScroll *container.Scroll
	walk(page, func(o fyne.CanvasObject) {
		if scroll, ok := o.(*container.Scroll); ok && issueScroll == nil && contains(labels(scroll.Content), "Issue List/Gantt Chart") {
			issueScroll = scroll
		}
	})
	if issueScroll == nil {
		t.Fatal("project page scroll missing")
	}
	issueScroll.ScrollToBottom()
	rec.stay(rec.shot(), 2000, true)
	var search *widget.Entry
	var state *shell.GoldSelect
	var modes *shell.UnderlineTabs
	walk(page, func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *widget.Entry:
			if strings.HasPrefix(v.PlaceHolder, "Search issues") {
				search = v
			}
		case *shell.GoldSelect:
			if slices.Contains(v.Options, "Not planned") {
				state = v
			}
		case *shell.UnderlineTabs:
			if slices.Contains(v.Options, "Gantt") {
				modes = v
			}
		}
	})
	if search == nil || state == nil || modes == nil {
		t.Fatal("issue list controls missing")
	}
	rec.glide(rec.shot(), centreOf(search), 250)
	search.SetText("invoice")
	rec.stay(rec.shot(), 2000, true)
	search.SetText("")
	rec.stay(rec.shot(), 700, false)
	rec.glide(rec.shot(), centreOf(state), 250)
	state.Tapped(nil)
	rec.stay(rec.shot(), 900, true)
	rec.glide(rec.shot(), centreOf(state.Rows.Objects[3]), 200)
	state.Choose(3)
	issueScroll.ScrollToBottom()
	rec.stay(rec.shot(), 1600, true)
	// Capture the actual Gantt tab, rather than pointing at the whole tab row.
	ganttTab := test.WidgetRenderer(modes).Objects()[2]
	rec.glide(rec.shot(), centreOf(ganttTab), 300)
	modes.SetSelected("Gantt")
	issueScroll.ScrollToBottom()
	rec.stay(rec.shot(), 3200, true)
	var chart *projects.GanttChart
	walk(page, func(o fyne.CanvasObject) {
		if c, ok := o.(*projects.GanttChart); ok {
			chart = c
		}
	})
	if chart == nil || len(chart.Ranges) != 14 {
		t.Fatal("Gantt did not retain all fictional issues")
	}
	if err := copyFile(filepath.Join(rec.dir, rec.shot()), filepath.Join(out, "13-project-gantt.png")); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(out, "13-project-gantt.json"),
		[4]float32{0, boxOf(fold, "").Y - 8, w.Canvas().Size().Width, w.Canvas().Size().Height})
	var invoice *canvas.Text
	walk(chart.Content, func(o fyne.CanvasObject) {
		if text, ok := o.(*canvas.Text); ok && strings.HasPrefix(text.Text, "#410 ") {
			invoice = text
		}
	})
	if invoice == nil {
		t.Fatal("invoice Gantt row missing")
	}
	rec.glide(rec.shot(), centreOf(invoice), 300)
	point := fyne.PointEvent{Position: fyne.NewPos(100, invoice.Position().Y+invoice.Size().Height/2)}
	chart.MouseMoved(&desktop.MouseEvent{PointEvent: point})
	rec.stay(rec.shot(), 2200, false)
	chart.Tapped(&point)
	rec.stay(rec.shot(), 2400, true)
	closeProjectDemoPopup(t, rec)
	// Hide the app bar in this recording: the software renderer does not clip
	// custom dashboard shapes at the scroller's top edge while panning.
	rec.finish([4]float32{0, 110, w.Canvas().Size().Width, w.Canvas().Size().Height})
}

func closeProjectDemoPopup(t *testing.T, rec *frameRecorder) {
	t.Helper()
	var close *widget.Button
	walkPopup(rec.w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Close" {
			close = b
		}
	})
	if close == nil {
		t.Fatal("project popup Close missing")
	}
	rec.glide(rec.shot(), centreOf(close), 250)
	test.Tap(close)
	rec.stay(rec.shot(), 700, true)
}

func projectDemoAnswers(t *testing.T, now time.Time) map[string]string {
	t.Helper()
	date := func(days int) string { return now.AddDate(0, 0, days).Format(time.RFC3339) }
	names := []string{"Invoice export to PDF", "Invoice preview templates", "Customer search index", "Tenant access controls", "Payment retry policy", "Webhook rate limits", "Audit history", "Export progress indicator", "Invoice download permissions", "Webhook delivery dashboard", "Customer onboarding", "Payment sync diagnostics", "Support queue filters", "Help centre search"}
	statuses := []string{"Created", "In Progress", "In Review", "Pending Test", "Done"}
	options := make([]map[string]string, len(statuses))
	for i, s := range statuses {
		options[i] = map[string]string{"id": fmt.Sprint(i), "name": s}
	}
	answers := map[string]string{}
	for parent, span := range map[string][2]int{"100": {0, 11}, "200": {11, 14}} {
		repo, title := "acme-corp/web-app", "Platform foundations"
		if parent == "200" {
			repo, title = "acme-support/helpdesk", "Support experience"
		}
		var nodes []any
		for i := span[0]; i < span[1]; i++ {
			number := 410 + i
			status, state, closed := statuses[i%4], "OPEN", ""
			if i >= 3 && i <= 9 {
				status, state, closed = "Done", "CLOSED", date(-42+(i-3)*6)
			}
			milestone, due := "Beta launch", date(14)
			if i >= 11 {
				milestone, due = "Support rollout", date(28)
			}
			node := map[string]any{
				"number": number, "title": names[i], "url": fmt.Sprintf("https://github.com/%s/issues/%d", repo, number),
				"state": state, "stateReason": "COMPLETED", "createdAt": date(-60 + i*3), "closedAt": closed,
				"repository": map[string]any{"nameWithOwner": repo},
				"assignees":  map[string]any{"nodes": []any{map[string]string{"login": []string{"ana", "bea", "chen"}[i%3]}}},
				"milestone":  map[string]string{"title": milestone, "dueOn": due},
				"projectItems": map[string]any{"nodes": []any{map[string]any{
					"id": fmt.Sprintf("item-%d", i), "project": map[string]any{"id": "demo-board", "field": map[string]any{"id": "status", "options": options}},
					"status": map[string]string{"name": status}, "priority": map[string]string{"name": []string{"P1", "P2", "P3"}[i%3]},
					"sprint": map[string]any{"title": "Sprint 36", "startDate": "2026-11-09", "duration": 14},
				}}},
			}
			if i == 0 || i == 1 {
				blocker := 410 + (i + 1)
				node["blockedBy"] = map[string]any{"totalCount": 1, "nodes": []any{map[string]any{
					"number": blocker, "title": names[i+1], "url": fmt.Sprintf("https://github.com/%s/issues/%d", repo, blocker), "closed": false,
				}}}
			}
			if i == 10 {
				// An unscheduled issue uses its opened date, not its milestone due date.
				node["projectItems"].(map[string]any)["nodes"].([]any)[0].(map[string]any)["sprint"] = nil
			}
			nodes = append(nodes, node)
		}
		raw, err := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"issue": map[string]any{
			"title": title, "url": fmt.Sprintf("https://github.com/%s/issues/%s", repo, parent), "state": "OPEN",
			"subIssues": map[string]any{"pageInfo": map[string]any{"hasNextPage": false}, "nodes": nodes},
		}}}})
		if err != nil {
			t.Fatal(err)
		}
		answers[parent] = string(raw)
	}
	return answers
}
