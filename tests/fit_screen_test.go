package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// A page wider than the screen must not force the window wider: it scrolls.
func TestFitScreenLetsTheWindowBeSmallerThanThePage(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("fit")
	defer w.Close()
	page := sizedBox(3000, 2000)
	shell.FitScreen(w, page)
	if m := w.Content().MinSize(); m.Width > 100 || m.Height > 100 {
		t.Fatalf("the window should not have to fit the page: %v", m)
	}
	w.Resize(fyne.NewSize(1000, 700))
	if s := page.Size(); s.Width != 3000 || s.Height != 2000 {
		t.Fatalf("the page keeps its size and scrolls: %v", s)
	}
}

// With room to spare the page fills the window, padded as a window pads.
func TestFitScreenLeavesAPageThatFitsAlone(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("fit")
	defer w.Close()
	page := sizedBox(100, 100)
	shell.FitScreen(w, page)
	w.Resize(fyne.NewSize(1000, 700))
	if s := page.Size(); s.Width <= 900 || s.Height <= 600 {
		t.Fatalf("the page should fill the window: %v", s)
	}
}

// A long issue title must not make a popup wider than the window.
func TestPopupStaysInsideTheWindow(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("popup")
	defer w.Close()
	w.SetContent(sizedBox(1, 1))
	w.Resize(fyne.NewSize(640, 480))
	ui := &shell.UI{Win: w}
	size := ui.PopupSize()
	canvasSize := w.Canvas().Size()
	if size.Width > canvasSize.Width || size.Height > canvasSize.Height {
		t.Fatalf("popup %v is bigger than the window %v", size, canvasSize)
	}
	if got := ui.PopupSizeFor(sizedBox(5000, 5000)); got.Width > canvasSize.Width || got.Height > canvasSize.Height {
		t.Fatalf("popup %v is bigger than the window %v", got, canvasSize)
	}
	w.Resize(fyne.NewSize(1000, 800))
	if got := ui.PopupSizeFor(sizedBox(900, 100)); got.Width < 900 {
		t.Fatalf("with room, the popup grows to its content: %v", got)
	}
	w.Resize(fyne.NewSize(640, 480))
	canvasSize = w.Canvas().Size()
	long := "A very long issue title that goes on and on and on and on and on and on and on and on and on and on"
	title := ui.PopupTitle(long)
	if title == long || fyne.MeasureText(title, 20, fyne.TextStyle{Bold: true}).Width > canvasSize.Width {
		t.Fatalf("title not cut to the window: %q", title)
	}
	w.Resize(fyne.NewSize(2560, 1400))
	if got := ui.PopupTitle(long); got != long {
		t.Fatalf("a roomy window keeps the whole title: %q", got)
	}
}

// Columns take their share, or their minimum if the share is too small, and the
// row asks only for the sum of its columns.
func TestFluidRow(t *testing.T) {
	test.NewApp()
	side, main := sizedBox(300, 10), sizedBox(400, 10)
	row := widgets.NewFluidRow(0.2, 0.8)
	box := container.New(row, side, main)
	if w := row.MinSize(box.Objects).Width; w > 720 {
		t.Fatalf("minimum should be the sum of the columns, got %g", w)
	}
	box.Resize(fyne.NewSize(2004, 100))
	if w := side.Size().Width; w != 400 {
		t.Fatalf("a fifth of the row: got %g", w)
	}
	box.Resize(fyne.NewSize(1004, 100))
	if s, m := side.Size().Width, main.Size().Width; s != 300 || m != 700 {
		t.Fatalf("the sidebar keeps its minimum and the rest goes to the main column: %g %g", s, m)
	}
}

func TestWrapRowWrapsOnlyWhenItMustAndSidesStack(t *testing.T) {
	test.NewApp()
	a, b := sizedBox(300, 40), sizedBox(300, 40)
	wrap := &widgets.WrapRowLayout{}
	row := container.New(wrap, a, b)
	row.Resize(fyne.NewSize(700, 40))
	if b.Position().Y != 0 || b.Position().X != 304 {
		t.Fatalf("both fit on one line: %v", b.Position())
	}
	row.Resize(fyne.NewSize(500, 200))
	if b.Position().X != 0 || b.Position().Y <= 0 {
		t.Fatalf("the second should wrap: %v", b.Position())
	}
	if h := wrap.MinSize(row.Objects).Height; h <= 40 {
		t.Fatalf("two lines need more height: %g", h)
	}

	left, right := sizedBox(300, 40), sizedBox(200, 40)
	sides := container.New(widgets.NewSidesLayout(left, right, 0), left, right)
	sides.Resize(fyne.NewSize(1000, 40))
	if right.Position().X != 800 || right.Position().Y != 0 {
		t.Fatalf("the right group stands at the right: %v", right.Position())
	}
	sides.Resize(fyne.NewSize(400, 100))
	if right.Position().X != 0 || right.Position().Y <= 0 {
		t.Fatalf("too narrow: the groups stack: %v", right.Position())
	}
}

func readIgnoredDates(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "ignored-dates.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A commit ignored over a month ago is forgotten; a recent one is not, and one
// with no date starts its clock now rather than being dropped on a guess.
func TestPruneIgnoredForgetsOnlyOldCommits(t *testing.T) {
	dir := tempDir(t)
	s := backend.NewStore(dir)
	if err := os.WriteFile(filepath.Join(dir, "ignored.json"), []byte(`["old","recent","edge","undated"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	dates := `{"old":"2026-10-06","recent":"2026-11-01","edge":"2026-10-07","gone":"2026-11-01"}`
	if err := os.WriteFile(filepath.Join(dir, "ignored-dates.json"), []byte(dates), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := s.PruneIgnored(31, 5, "2026-11-07")
	if err != nil || removed != 1 {
		t.Fatalf("removed %d, %v", removed, err)
	}
	set, err := s.IgnoredShas()
	if err != nil || set["old"] || !set["recent"] || !set["edge"] || !set["undated"] {
		t.Fatalf("wrong set: %v %v", set, err)
	}
	raw := readIgnoredDates(t, dir)
	if contains([]string{raw}, "gone") {
		t.Fatalf("a date with no commit should go: %s", raw)
	}
	// The undated one is dated today now, so it lasts another month.
	if removed, _ = s.PruneIgnored(31, 5, "2026-12-07"); removed == 0 {
		t.Fatal("the rest should have aged out by now")
	}
	set, _ = s.IgnoredShas()
	if !set["undated"] {
		t.Fatalf("31 days from today has not passed for the undated commit: %v", set)
	}
}

// A longer look back keeps commits for as long as it could still find them.
func TestPruneIgnoredRespectsALongLookback(t *testing.T) {
	dir := tempDir(t)
	s := backend.NewStore(dir)
	if err := s.SetIgnored("abc", true); err != nil {
		t.Fatal(err)
	}
	today := time.Now()
	in := func(days int) string { return today.AddDate(0, 0, days).Format("2006-01-02") }
	if removed, _ := s.PruneIgnored(31, 60, in(40)); removed != 0 {
		t.Fatal("a 60 day look back could still find it")
	}
	if removed, _ := s.PruneIgnored(31, 5, in(40)); removed != 1 {
		t.Fatal("a 5 day look back can no longer find it")
	}
}

// Clean up left blank forgets nothing, however old, but still dates new entries.
func TestPruneIgnoredOffKeepsEverything(t *testing.T) {
	dir := tempDir(t)
	s := backend.NewStore(dir)
	if err := os.WriteFile(filepath.Join(dir, "ignored.json"), []byte(`["old","undated"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored-dates.json"), []byte(`{"old":"2020-01-01"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed, err := s.PruneIgnored(0, 5, "2026-11-07"); err != nil || removed != 0 {
		t.Fatalf("removed %d, %v", removed, err)
	}
	if set, _ := s.IgnoredShas(); !set["old"] || !set["undated"] {
		t.Fatalf("nothing should go: %v", set)
	}
	if !strings.Contains(readIgnoredDates(t, dir), "undated") {
		t.Fatal("the undated commit should have started its clock")
	}
}

func refsByIssue(t *testing.T, s *backend.Store) map[string]string {
	t.Helper()
	rows, err := s.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{}
	for _, r := range rows {
		refs[r["issue"]] = r["refs"]
	}
	return refs
}

// Old pushed entries lose their commit list and nothing else. Entries not yet
// pushed, and recent ones, keep theirs.
func TestPruneCommitRefs(t *testing.T) {
	s := backend.NewStore(tempDir(t))
	today := time.Now().Format("2006-01-02")
	if _, err := s.AppendRows([]backend.Row{
		{"date": "2020-01-01", "minutes": "60", "issue": "pushed", "refs": "o/r@aaa", "remarks": "keep me", "pushed_at": "2020-01-02T10:00:00"},
		{"date": "2020-01-01", "minutes": "60", "issue": "waiting", "refs": "o/r@bbb"},
		{"date": today, "minutes": "60", "issue": "recent", "refs": "o/r@ccc", "pushed_at": today + "T10:00:00"},
	}); err != nil {
		t.Fatal(err)
	}
	later := time.Now().AddDate(0, 0, 40).Format("2006-01-02")

	// Blank in Settings: nothing changes, however old.
	if n, err := s.PruneCommitRefs(0, 5, later); err != nil || n != 0 {
		t.Fatalf("clean up off changed %d, %v", n, err)
	}
	// The entries were saved just now, so nothing is old enough yet.
	if n, _ := s.PruneCommitRefs(31, 5, today); n != 0 {
		t.Fatalf("changed %d entries saved today", n)
	}
	// A look back that reaches further wins: the sweep could still find them.
	if n, _ := s.PruneCommitRefs(31, 60, later); n != 0 {
		t.Fatalf("changed %d inside the look back", n)
	}
	if n, err := s.PruneCommitRefs(31, 5, later); err != nil || n != 2 {
		t.Fatalf("changed %d, %v", n, err)
	}
	refs := refsByIssue(t, s)
	if refs["pushed"] != "" || refs["recent"] != "" {
		t.Fatalf("pushed entries past the cut off keep nothing: %v", refs)
	}
	if refs["waiting"] != "o/r@bbb" {
		t.Fatalf("an entry not pushed yet keeps its commits: %v", refs)
	}
	rows, _ := s.ReadRows()
	if len(rows) != 3 {
		t.Fatalf("no entry should go: %d", len(rows))
	}
	for _, r := range rows {
		if r["issue"] == "pushed" && (r["remarks"] != "keep me" || r["minutes"] != "60") {
			t.Fatalf("the entry itself changed: %v", r)
		}
	}
	logged, _ := s.LoggedShas()
	if logged["aaa"] || !logged["bbb"] {
		t.Fatalf("logged set: %v", logged)
	}
}

// Taking a commit back out takes its date with it.
func TestUnignoreDropsTheDate(t *testing.T) {
	dir := tempDir(t)
	s := backend.NewStore(dir)
	if err := s.SetIgnored("abc", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetIgnored("abc", false); err != nil {
		t.Fatal(err)
	}
	if raw := readIgnoredDates(t, dir); raw != "{}" {
		t.Fatalf("date left behind: %s", raw)
	}
}
