//go:build demo

package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// V2 media uses real widgets, mocked GitHub replies and an isolated bare git
// remote. Every write (including the reference rewrite and push) stays in TEMP.
func TestReadmeV2DemoFrames(t *testing.T) {
	if os.Getenv("DEMO_FRAMES") == "" || os.Getenv("DEMO_OUT") == "" {
		t.Skip("DEMO_FRAMES and DEMO_OUT not set")
	}
	t.Run("worklog-and-pr", recordV2Worklog)
	t.Run("calendar", recordV2Calendar)
	t.Run("repo-folders", recordV2RepoFolders)
}

func v2UI(t *testing.T, width, height float32) *shell.UI {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	test.ApplyTheme(t, darkGold{shell.GoldTheme{Theme: theme.DefaultTheme()}})
	w := a.NewWindow(shell.WindowTitle)
	t.Cleanup(w.Close)
	w.Resize(fyne.NewSize(width, height))
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, HasCfg: true,
		CalMonth: shell.ThisMonth(), RepMonth: shell.CurrentPeriod(),
		Cfg: backend.Config{GithubAuthor: "alex", WorklogOwner: "team-alex", DisplayName: "Alex",
			Repos: []string{"acme-corp"}, LookbackDays: 14, Currency: "MYR", ArchiveDir: tempDir(t)},
		IssueInfo: map[string]backend.IssueInfo{}}
	for _, issue := range demoIssues {
		issue.URL = demoURL(issue)
		ui.IssueInfo[demoRef(issue)] = issue
	}
	return ui
}

type v2Recorder struct {
	*frameRecorder
	caption  string
	captions map[string]string
	crop     *[4]float32
}

func v2Record(t *testing.T, w fyne.Window, name string) *v2Recorder {
	return &v2Recorder{frameRecorder: newFrameRecorder(t, w,
		filepath.Join(os.Getenv("DEMO_FRAMES"), name), [2]float32{1080, 620}), captions: map[string]string{}}
}

func (r *v2Recorder) shot() string {
	file := r.frameRecorder.shot()
	r.captions[file] = r.caption
	return file
}

func (r *v2Recorder) hold(caption string, ms int) {
	r.caption = caption
	r.stay(r.shot(), ms, false)
}

func (r *v2Recorder) finish() {
	type frame struct {
		demoFrame
		Caption string `json:"caption"`
	}
	var frames []frame
	for _, f := range r.seq {
		frames = append(frames, frame{f, r.captions[f.File]})
	}
	sz := r.w.Canvas().Size()
	crop := [4]float32{0, 0, sz.Width, sz.Height}
	if r.crop != nil {
		crop = *r.crop
	}
	writeJSON(r.t, filepath.Join(r.dir, "frames.json"), map[string]any{
		"crop": crop, "frames": frames,
	})
}

func v2HoverButton(t *testing.T, root fyne.CanvasObject, text string) *shell.HoverButton {
	t.Helper()
	var found *shell.HoverButton
	walk(root, func(o fyne.CanvasObject) {
		if b, ok := o.(*shell.HoverButton); ok && b.Text == text {
			found = b
		}
	})
	if found == nil {
		t.Fatalf("missing %q action", text)
	}
	return found
}

func v2PopupAction(t *testing.T, w fyne.Window, text string) *widget.Button {
	t.Helper()
	if deepButton(w.Canvas().Overlays().Top(), text) == nil {
		var shown []string
		walkRendered(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
			if l, ok := o.(*widget.Label); ok {
				shown = append(shown, l.Text)
			}
		})
		t.Fatalf("missing %q; popup: %v", text, shown)
	}
	b := waitForButton(t, w, text)
	if b.OnTapped == nil {
		t.Fatalf("%q has no action", text)
	}
	return b
}

func recordV2Worklog(t *testing.T) {
	ui := v2UI(t, 1280, 760)
	root := tempDir(t)
	clone, remote := filepath.Join(root, "web-app"), filepath.Join(root, "remote.git")
	gitT(t, root, "init", "--quiet", "--bare", "-b", "main", remote)
	gitT(t, root, "init", "--quiet", "-b", "main", clone)
	for _, kv := range [][2]string{{"user.name", "Alex"}, {"user.email", "alex@example.test"},
		{"commit.gpgsign", "false"}, {"core.hooksPath", filepath.Join(root, "no-hooks")}, {"core.autocrlf", "false"}} {
		gitT(t, clone, "config", kv[0], kv[1])
	}
	gitT(t, clone, "remote", "add", "origin", "https://github.com/acme-corp/web-app.git")
	gitT(t, clone, "config", "url."+filepath.ToSlash(remote)+".insteadOf", "https://github.com/acme-corp/web-app.git")
	commit := func(file, message string) string {
		if err := os.WriteFile(filepath.Join(clone, file), []byte(message+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitT(t, clone, "add", file)
		gitT(t, clone, "commit", "--quiet", "-m", message)
		return gitT(t, clone, "rev-parse", "HEAD")
	}
	commit("base.txt", "Initial application")
	gitT(t, clone, "push", "--quiet", "origin", "main")
	gitT(t, clone, "remote", "set-head", "origin", "main")
	gitT(t, clone, "checkout", "--quiet", "-b", "feat/invoice-export")
	a := commit("pdf.txt", "Add PDF renderer for invoices")
	b := commit("pages.txt", "Paginate long invoices")
	if err := os.WriteFile(filepath.Join(clone, "pdf.txt"), []byte("Uncommitted layout adjustment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ui.Cfg.RepoFolders = []string{clone}

	oldGh, oldPushDay := backend.Gh, logwork.PushDay
	t.Cleanup(func() { backend.Gh, logwork.PushDay = oldGh, oldPushDay })
	logwork.PushDay = shell.Today
	created, parented, typed, minutesSet, prCreated := false, false, false, false, false
	backend.Gh = func(args []string, _ ...string) (string, error) {
		q := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(q, "api user"):
			return "alex", nil
		case strings.Contains(q, "filterBy:{createdBy:"):
			return `{"data":{"repository":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`, nil
		case strings.Contains(q, "subIssues(first:100)"):
			return `{"data":{"repository":{"issue":{"id":"PARENT","number":412,"title":"Invoice export to PDF","url":"https://github.com/acme-corp/web-app/issues/412","subIssues":{"nodes":[]},"projectItems":{"nodes":[{"id":"PI","project":{"id":"PROJECT","number":1,"fields":{"nodes":[{"id":"STATUS","name":"Status","options":[{"id":"DONE","name":"Done"}]},{"id":"DATE","name":"Worklog (Date)"},{"id":"OWNER","name":"Worklog Owner"},{"id":"MINUTES","name":"Worklog (mins)"},{"id":"REMARKS","name":"Worklog (Remarks)"},{"id":"ACTUAL-COLUMN","name":"Worklog Reported(mins)","isIssueField":true,"issueField":{"id":"ACTUAL","name":"Worklog Reported(mins)"}}]}}}]}}}}}`, nil
		case strings.HasPrefix(q, "issue create"):
			created = true
			return "https://github.com/acme-corp/web-app/issues/6001", nil
		case strings.HasPrefix(q, "issue view"):
			return `{"id":"WORKLOG"}`, nil
		case strings.Contains(q, "issueTypes(first:"):
			return `{"data":{"organization":{"issueTypes":{"nodes":[{"id":"WORKLOG-TYPE","name":"Worklog"}]}}}}`, nil
		case strings.Contains(q, "addSubIssue(input:"):
			parented = true
			return `{}`, nil
		case strings.Contains(q, "updateIssueIssueType(input:"):
			typed = true
			return `{}`, nil
		case strings.Contains(q, "setIssueFieldValue(input:"):
			minutesSet = strings.Contains(q, "minutes=120")
			return `{}`, nil
		case strings.Contains(q, "addProjectV2ItemById(input:"):
			return `{"data":{"addProjectV2ItemById":{"item":{"id":"WORKLOG-ITEM"}}}}`, nil
		case strings.Contains(q, "fieldValueByName(name:"):
			return `{"data":{"node":{"fieldValueByName":{"name":"Done"}}}}`, nil
		case strings.HasPrefix(q, "project item-edit"):
			return `{}`, nil
		case strings.Contains(q, "associatedPullRequests(first:"):
			return `{"data":{"repository":{}}}`, nil // local commits are not on GitHub yet
		case strings.HasPrefix(q, "pr list"):
			if strings.Contains(q, "--json number") {
				return "0", nil
			}
			return "", nil
		case strings.Contains(q, "/branches/"):
			return "false", nil
		case strings.HasPrefix(q, "pr create"):
			prCreated = true
			return "https://github.com/acme-corp/web-app/pull/6010", nil
		default:
			return "", backend.GhErr("Offline demo: unexpected request %s", q)
		}
	}
	refs := "acme-corp/web-app@" + backend.Short(a) + ",acme-corp/web-app@" + backend.Short(b)
	rows, err := ui.Store.AppendRows([]backend.Row{{"date": shell.Today(), "logged_at": shell.Today() + "T14:10:00",
		"commit_date": shell.Today(), "minutes": "120", "type": backend.KindCommit, "issue": "acme-corp/web-app#412",
		"issue_title": "Invoice export to PDF", "owner": "team-alex", "mode": "subissue", "refs": refs,
		"remarks": "- Add PDF renderer for invoices\n- Paginate long invoices"}})
	if err != nil {
		t.Fatal(err)
	}
	ui.Pending.Local = map[string]bool{}
	for _, sha := range []string{a, b} {
		ui.Pending.Local[backend.CommitRef{Repo: "acme-corp/web-app", Sha: backend.Short(sha)}.Key()] = true
	}
	var card fyne.CanvasObject
	stage := container.NewVBox()
	refresh := func() {
		current, err := ui.Store.ReadRows()
		if err != nil {
			t.Fatal(err)
		}
		card = logwork.RowTile(ui, current[0], func() {})
		stage.Objects = []fyne.CanvasObject{shell.SectionTitle("Saved locally"), card}
		stage.Refresh()
	}
	// The actual tile's callback refreshes this isolated documentation view.
	card = logwork.RowTile(ui, rows[0], refresh)
	stage.Objects = []fyne.CanvasObject{shell.SectionTitle("Saved locally"), card}
	shell.FitScreen(ui.Win, container.NewPadded(container.NewBorder(
		widget.NewLabelWithStyle("Log Work", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), nil, nil, nil,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(900, 320), shell.Panel(stage))))))
	flow := v2Record(t, ui.Win, "v2-worklog-flow")
	flow.crop = &[4]float32{140, 190, 1140, 630}
	flow.at = [2]float32{1030, 560}
	flow.hold("1 / 3   Save today's work. Pending Push marks commits that are still on your machine.", 2600)
	push := v2HoverButton(t, card, "Push")
	flow.glide(flow.shot(), centreOf(push), 350)
	flow.stay(flow.shot(), 140, true)
	push.OnTapped()
	add := v2PopupAction(t, ui.Win, "Add refs & push")
	if !created || !parented || !typed || !minutesSet {
		t.Fatalf("SOP steps missing: create=%v parent=%v type=%v minutes=%v; popup=%v", created, parented, typed, minutesSet, labels(ui.Win.Canvas().Overlays().Top()))
	}
	flow.hold("2 / 3   Worklog #6001 is created under issue #412, with 120 reported minutes.", 2800)
	flow.hold("Review the reference change. These local commits use a plain push; your uncommitted edits stay intact.", 3300)
	flow.glide(flow.shot(), centreOf(add), 400)
	flow.stay(flow.shot(), 160, true)
	add.OnTapped()
	create := v2PopupAction(t, ui.Win, "Create PR")
	current, _ := ui.Store.ReadRows()
	if current[0]["ref_state"] != backend.RefComplete {
		t.Fatalf("reference incomplete: %v", current[0])
	}
	if message := gitT(t, clone, "log", "-1", "--format=%B", "origin/feat/invoice-export"); !strings.Contains(message, "Refs acme-corp/web-app#6001") {
		t.Fatal(message)
	}
	if dirty := gitT(t, clone, "diff"); !strings.Contains(dirty, "Uncommitted layout adjustment") {
		t.Fatal("working tree changed")
	}
	flow.hold("3 / 3   Both commits now carry Refs acme-corp/web-app#6001. The branch is ready for a PR.", 3200)
	flow.finish()

	pr := v2Record(t, ui.Win, "v2-pull-request")
	pr.crop = flow.crop
	pr.at = [2]float32{1030, 560}
	pr.hold("1 / 3   When every commit of yours on a branch is logged, the app offers a pull request.", 3100)
	var page *widget.Hyperlink
	walkRendered(ui.Win.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if h, ok := o.(*widget.Hyperlink); ok && h.Text == "Open PR page" {
			page = h
		}
	})
	if page == nil || !strings.Contains(page.URL.RawQuery, "title=") {
		t.Fatal("prefilled compare link missing")
	}
	pr.glide(pr.shot(), centreOf(page), 500)
	pr.hold("Open PR page has the title and description filled in. Or create the PR directly below.", 2500)
	pr.glide(pr.shot(), centreOf(create), 500)
	pr.stay(pr.shot(), 160, true)
	create.OnTapped()
	_ = v2PopupAction(t, ui.Win, "OK")
	if !prCreated || !hasRenderedText(ui.Win.Canvas().Overlays().Top(), "Pull request created") {
		t.Fatal("PR result missing")
	}
	pr.hold("2 / 3   Create PR opens it through gh. The result gives you the GitHub link.", 2600)
	var copy *shell.HoverButton
	walkRendered(ui.Win.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if h, ok := o.(*shell.HoverButton); ok && h.Text == "Copy" {
			copy = h
		}
	})
	if copy == nil {
		t.Fatal("Copy missing")
	}
	pr.glide(pr.shot(), centreOf(copy), 400)
	copy.OnTapped()
	if fyne.CurrentApp().Clipboard().Content() != "https://github.com/acme-corp/web-app/pull/6010" {
		t.Fatal("wrong copied link")
	}
	pr.hold("3 / 3   Copy the link to share the pull request with your team.", 2600)
	pr.finish()
}

func recordV2Calendar(t *testing.T) {
	ui := v2UI(t, 1600, 940)
	oldPushDay := logwork.PushDay
	logwork.PushDay = shell.Today
	t.Cleanup(func() { logwork.PushDay = oldPushDay })
	ui.PendingLoaded = true
	ui.PendingAt = time.Now()
	ui.Cfg.ProjectURL = "https://github.com/orgs/acme-corp/projects/1"
	ui.WeekStart = "2026-11-23"
	ui.EnsureProjectCache()
	for _, month := range []string{"2026-11", shell.ThisMonth()} {
		key := shell.StatusCacheKey(month)
		ui.ProjLoaded[key] = true
	}
	ui.ProjItems[shell.StatusCacheKey("2026-11")] = []backend.WorklogItem{
		{Date: "2026-11-23", Minutes: 480, Owner: "team-alex", Title: "Invoice export to PDF", ParentURL: demoURL(demoIssues[0]), URL: demoURL(demoIssues[0]) + "1"},
		{Date: "2026-11-24", Minutes: 240, Owner: "team-alex", Title: "Customer search", ParentURL: demoURL(demoIssues[1]), URL: demoURL(demoIssues[1]) + "1"},
	}
	var rows []backend.Row
	titles := []string{"Invoice export to PDF", "Paginate invoice line items", "Check the PDF layout", "Add export regression coverage", "Review error handling", "Document the export options", "Verify large invoices", "Polish the download filename"}
	for i, title := range titles {
		rows = append(rows, backend.Row{"date": "2026-11-24", "minutes": "30", "issue": "acme-corp/web-app#412", "issue_title": title, "type": backend.KindCommit, "mode": "subissue",
			"owner": "team-alex", "logged_at": "2026-11-23T14:10:00", "commit_date": "2026-11-24", "refs": fmt.Sprintf("acme-corp/web-app@%07x", 0xabc1234+i), "remarks": "- " + title})
	}
	for i := 0; i < 3; i++ {
		rows = append(rows, backend.Row{"date": "2026-11-25", "minutes": "60", "issue": demoRef(demoIssues[2]), "issue_title": demoIssues[2].Title, "type": backend.KindCommit, "mode": "subissue", "legacy": "1", "remarks": "- Check webhook retries"})
	}
	rows = append(rows, backend.Row{"date": "2026-11-26", "minutes": "45", "issue": demoRef(demoIssues[4]), "issue_title": demoIssues[4].Title, "type": backend.KindCommit, "mode": "subissue", "refs": "acme-support/helpdesk@def1234", "merged_refs": "acme-support/helpdesk@def1234", "remarks": "- Review the merged payment fix"})
	if _, err := ui.Store.AppendRows(rows); err != nil {
		t.Fatal(err)
	}
	logView := logwork.BuildLogTab(ui)
	shell.FitScreen(ui.Win, logView)
	logwork.DrawRecent(ui)
	logwork.RenderPending(ui)
	scroll := logView.(*fyne.Container).Objects[0].(*container.ThemeOverride).Content.(*container.Scroll)
	// Position the calendar below the Saved locally heading before its arrival.
	column := ui.WeekCols["2026-11-23"]
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(column)
	scroll.Offset.Y += at.Y - 180
	scroll.Refresh()
	if scroll.OnScrolled != nil {
		scroll.OnScrolled(scroll.Offset)
	}
	r := v2Record(t, ui.Win, "v2-logwork-calendar")
	r.at = [2]float32{1500, 700}
	r.caption = "1 / 3   Days uncover first, then each day's cards wipe in. Logged and draft totals have distinct greens."
	base := fyne.CurrentApp()
	driver := &v2MotionDriver{Driver: base.Driver(), started: time.Now()}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: driver})
	ui.ReplayArrival("week")
	logwork.DrawWeekStrip(ui)
	// Wipes register from their staggered timers. Collect them before advancing
	// the virtual recording clock, so capture speed cannot hide late cards.
	time.Sleep(1400 * time.Millisecond)
	for ms := 0; ms <= 1600; ms += 50 {
		for i := range driver.anims {
			step := &driver.anims[i]
			elapsed := time.Duration(ms)*time.Millisecond - step.delay
			if elapsed < 0 || step.done {
				continue
			}
			p := min(float32(1), float32(elapsed)/float32(step.anim.Duration))
			share := p
			if step.anim.Curve != nil {
				share = step.anim.Curve(p)
			}
			step.anim.Tick(share)
			step.done = p >= 1
		}
		r.stay(r.shot(), 40, false)
	}
	fyne.SetCurrentApp(base)
	for _, step := range driver.anims {
		if !step.done {
			t.Fatal("an arrival animation was not captured to completion")
		}
	}
	r.hold(r.caption, 2400)
	// Settle the completed wipes with the normal same-week redraw. This also
	// avoids the software renderer's nested-scroll clipping limitation.
	logwork.DrawWeekStrip(ui)
	start := scroll.Offset.Y
	firstCard := dragTilesIn(ui.WeekCols["2026-11-24"])[0]
	beforeCardY := base.Driver().AbsolutePositionForObject(firstCard).Y
	r.caption = "2 / 3   Scroll the week: rounded day headers stay pinned while cards pass underneath."
	for i := 1; i <= 20; i++ {
		scroll.Offset.Y = start + float32(i)*17
		scroll.Refresh()
		if scroll.OnScrolled != nil {
			scroll.OnScrolled(scroll.Offset)
		}
		r.stay(r.shot(), 50, false)
	}
	r.hold(r.caption, 2300)
	if base.Driver().AbsolutePositionForObject(firstCard).Y >= beforeCardY || !ui.WeekCols["2026-11-24"].Edge.Visible() {
		t.Fatal("the cards must scroll beneath pinned weekday outlines")
	}
	for i := 19; i >= 0; i-- {
		scroll.Offset.Y = start + float32(i)*17
		scroll.Refresh()
		if scroll.OnScrolled != nil {
			scroll.OnScrolled(scroll.Offset)
		}
		r.stay(r.shot(), 40, false)
	}
	logwork.DrawWeekStrip(ui) // same-week redraw settles without replaying arrival
	tiles := dragTilesIn(ui.WeekCols["2026-11-24"])
	if len(tiles) == 0 {
		t.Fatal("saved cards missing")
	}
	r.glide(r.shot(), centreOf(tiles[0]), 400)
	tiles[0].OnTap()
	_ = v2PopupAction(t, ui.Win, "Back")
	r.hold("3 / 3   Open a saved entry: its issue title, Created date, Committed date and commit links are together.", 3500)
	r.finish()
}

type v2MotionStep struct {
	anim  *fyne.Animation
	delay time.Duration
	done  bool
}

type v2MotionDriver struct {
	fyne.Driver
	started time.Time
	anims   []v2MotionStep
}

func (d *v2MotionDriver) StartAnimation(a *fyne.Animation) {
	d.anims = append(d.anims, v2MotionStep{anim: a, delay: time.Since(d.started)})
}

func (d *v2MotionDriver) StopAnimation(*fyne.Animation) {}

func recordV2RepoFolders(t *testing.T) {
	ui := v2UI(t, 1500, 1000)
	ui.Cfg.ProjectURL = "https://github.com/orgs/acme-corp/projects/1"
	ui.Cfg.RepoFolders = []string{`D:\Code\team-projects`, `D:\Code\web-app`}
	view := settings.BuildSettingsTab(ui)
	shell.FitScreen(ui.Win, view)
	var folders *widget.Entry
	walk(view, func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok && e.Text == strings.Join(ui.Cfg.RepoFolders, "\n") {
			folders = e
		}
	})
	if folders == nil {
		t.Fatal("Repo folders setting missing")
	}
	folders.FocusGained()
	out := os.Getenv("DEMO_OUT")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	capturePNG(t, ui.Win, filepath.Join(out, "v2-repo-folders.png"))
	writeJSON(t, filepath.Join(out, "v2-repo-folders.json"), []demoBox{boxOf(folders, "Add your clone folders here")})
}
