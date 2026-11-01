package tests

import (
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/menus/logwork"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// The day turns over at 08:00, not at midnight: work carried past midnight is
// still yesterday's, which is the day it has to be logged against.
func TestWorkDayTurnsOverAtEight(t *testing.T) {
	at := func(h int) time.Time {
		return time.Date(2026, 8, 12, h, 30, 0, 0, time.Local)
	}
	if got := backend.WorkDate(at(1)); got != "2026-08-11" {
		t.Fatalf("1:30am belongs to the day before, got %s", got)
	}
	if got := backend.WorkDate(at(7)); got != "2026-08-11" {
		t.Fatalf("7:30am is still the night before, got %s", got)
	}
	if got := backend.WorkDate(at(8)); got != "2026-08-12" {
		t.Fatalf("8:30am starts the new day, got %s", got)
	}
	if got := backend.WorkDate(at(23)); got != "2026-08-12" {
		t.Fatalf("late evening is its own day, got %s", got)
	}
}

// The percentage alone left the reader doing the arithmetic: 68% of 480 is how
// many minutes short? The number that decides whether to log more is spelled out.
func TestDayScoreLineSpellsOutWhatIsLeft(t *testing.T) {
	for _, c := range []struct {
		mins int
		want string
	}{
		{330, "330 min · 68% (150m left)"},
		{0, "0 min · 0% (480m left)"},
		{480, "480 min · 100% ✓"},
		{540, "540 min · 112% ✓"}, // over target owes nothing
	} {
		if got := logwork.DayScoreLine(c.mins); got != c.want {
			t.Fatalf("dayScoreLine(%d) = %q, want %q", c.mins, got, c.want)
		}
	}
}

func TestDraftBubblesSortByDurationOrCreationTime(t *testing.T) {
	rows := []backend.Row{
		{"id": "later-short", "minutes": "30", "logged_at": "2026-09-19T11:00:00"},
		{"id": "early-mid", "minutes": "60", "logged_at": "2026-09-19T09:00:00"},
		{"id": "middle-long", "minutes": "90", "logged_at": "2026-09-19T10:00:00"},
	}
	logwork.SortDraftRows(rows, logwork.DraftSortDuration)
	if rows[0]["id"] != "middle-long" || rows[1]["id"] != "early-mid" || rows[2]["id"] != "later-short" {
		t.Fatalf("duration order = %v", rows)
	}
	logwork.SortDraftRows(rows, logwork.DraftSortCreated)
	if rows[0]["id"] != "early-mid" || rows[1]["id"] != "middle-long" || rows[2]["id"] != "later-short" {
		t.Fatalf("creation order = %v", rows)
	}
}

// Two captions carrying that much text do not fit side by side in a column a
// seventh of the window wide, and fyne does not clip — the overflow prints over
// the next day. So the row reserves the second line it may need.
func TestSplitCaptionStacksWhenTheTwoWillNotFit(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())

	left := shell.WeekCaption("240 min · 50% (240m left) · +300 min")
	right := shell.WeekCaption("540 min · 112% ✓")
	objs := []fyne.CanvasObject{left, right}
	l := shell.SplitCaption{}

	// Room for both rows is reserved whether or not they end up used.
	if m := l.MinSize(objs); m.Height < left.MinSize().Height*2 {
		t.Fatalf("two captions should reserve two rows, got %v", m)
	}

	// Narrow: stacked, and the second sits below the first.
	l.Layout(objs, fyne.NewSize(120, l.MinSize(objs).Height))
	if right.Position().Y <= left.Position().Y {
		t.Fatalf("a narrow column should stack them: left %v right %v",
			left.Position(), right.Position())
	}
	if right.Position().X != 0 {
		t.Fatalf("a stacked caption starts at the left edge, got %v", right.Position())
	}

	// Wide: side by side, the second flush right.
	wide := left.MinSize().Width + right.MinSize().Width + 200
	l.Layout(objs, fyne.NewSize(wide, l.MinSize(objs).Height))
	if right.Position().Y != left.Position().Y {
		t.Fatalf("a wide column should keep them on one line: left %v right %v",
			left.Position(), right.Position())
	}
	if got, want := right.Position().X, wide-right.MinSize().Width; got != want {
		t.Fatalf("the second caption should be flush right: %v, want %v", got, want)
	}

	// An empty caption is not a second line.
	solo := []fyne.CanvasObject{shell.WeekCaption("nothing logged"), shell.WeekCaption("")}
	if m, one := (shell.SplitCaption{}).MinSize(solo), solo[0].MinSize(); m.Height > one.Height {
		t.Fatalf("one caption is one row, got %v for %v", m, one)
	}
}

// The support placeholder pencils an unlogged rota shift onto the weekdays so
// the week can be read as it will stand. Nothing is written for it.
func TestSupportPlaceholderCoversWeekdaysWithoutOne(t *testing.T) {
	cfg := backend.Config{WorklogOwner: "team-alex"}
	support := []backend.WorklogItem{{
		ParentTitle: "Alex: Weekly Support (28th August, 12PM - 3PM)",
		Title:       "Worklog: 2026-08-28",
	}}

	for _, c := range []struct {
		name  string
		on    bool
		date  string
		items []backend.WorklogItem
		want  int
	}{
		{"a weekday with nothing on it", true, "2026-08-24", nil, backend.SupportPlaceholderMins},
		// The real shift is already on the board; a second one pencilled on top
		// would claim three hours twice.
		{"a weekday that already has the shift", true, "2026-08-28", support, 0},
		// Matched on the parent, since the worklog under it is called
		// "Worklog: <date>" like every other one.
		{"matched on the item's own title", true, "2026-08-28",
			[]backend.WorklogItem{{Title: "Alex: Weekly Support (28th August)"}}, 0},
		{"other work on the day is not the shift", true, "2026-08-24",
			[]backend.WorklogItem{{ParentTitle: "Chinese translation"}}, backend.SupportPlaceholderMins},
		// The rota is weekdays; a weekend is its own arrangement.
		{"Saturday", true, "2026-08-29", nil, 0},
		{"Sunday", true, "2026-08-30", nil, 0},
		{"switched off", false, "2026-08-24", nil, 0},
		{"an unreadable date", true, "not a date", nil, 0},
	} {
		if got := shell.SupportPlaceholderFor(cfg, c.on, c.date, c.items); got != c.want {
			t.Fatalf("%s: got %d, want %d", c.name, got, c.want)
		}
	}

	// With no name configured there is nothing to match a support title on, so
	// nothing is ever treated as already covered.
	if shell.HasWeeklySupport(backend.Config{}, support) {
		t.Fatal("with no display name there is no prefix to match on")
	}
}

// It is saved for its own week: it does not follow you to another week, and it
// is still there when you walk back or reopen the app.
func TestSupportPlaceholderIsSavedPerWeek(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	week := ui.WeekStart

	sprintcalendar.SetSupport(ui, true, week)
	logwork.DrawWeekStrip(ui)
	if !sprintcalendar.SupportOn(ui, week) {
		t.Fatal("drawing the strip should not clear it")
	}

	next := buttonNamed(ui.WeekBox, "This week")
	if next == nil {
		t.Fatalf("the strip needs a way home: %v", labels(ui.WeekBox))
	}
	next.OnTapped()
	if sprintcalendar.SupportOn(ui, ui.WeekStart) {
		t.Fatal("another week should not inherit the placeholder")
	}
	if !sprintcalendar.SupportOn(ui, week) {
		t.Fatal("walking away should not forget the week it was ticked for")
	}
	if !ui.Store.LoadSupportWeeks()[week] {
		t.Fatal("the ticked week was not saved")
	}
	sprintcalendar.SetSupport(ui, false, week)
	if _, err := os.Stat(filepath.Join(ui.Store.Dir, backend.SupportWeeksFile)); !os.IsNotExist(err) {
		t.Fatal("an empty support file should be removed", err)
	}
}

// Switched on, a weekday reads as it would once the shift is logged — and says
// the extra came from the placeholder rather than from a saved entry.
func TestSupportPlaceholderShowsOnTheStrip(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	ui.Cfg.WorklogOwner = "team-alex"
	withProject(ui, "2026-08")

	key := shell.StatusCacheKey("2026-08")
	ui.ProjItems[key] = []backend.WorklogItem{
		{Date: "2026-08-10", Minutes: 240, URL: "https://github.com/exampleorg/sample-intranet/issues/1"},
	}
	ui.ProjLoaded[key] = true

	sprintcalendar.SetSupport(ui, true, ui.WeekStart)
	logwork.DrawRecent(ui)

	got := labels(ui.WeekBox)
	// 240 banked plus the 180 pencilled in is 420 of 480.
	if !contains(got, "+180 min (inc. support)") {
		t.Fatalf("the placeholder should be named on the day: %v", got)
	}
	if !contains(got, "420 min · 87% (60m left)") {
		t.Fatalf("the day should read as it would with the shift on it: %v", got)
	}
	// The weekend is not on the rota. Checked on the weekend's own columns:
	// "180 min · 37%" is the right reading for the empty weekdays beside it.
	for _, ds := range []string{"2026-08-15", "2026-08-16"} {
		col := ui.WeekCols[ds]
		if col == nil {
			t.Fatalf("the week is missing %s", ds)
		}
		if in := labels(col); contains(in, "support") || contains(in, "180 min") {
			t.Fatalf("%s is a weekend and carries no placeholder: %v", ds, in)
		}
	}
}

// A week is Monday to Sunday, and a Sunday belongs to the week that started six
// days earlier — not to the one about to start.
func TestWeekRunsMondayToSunday(t *testing.T) {
	if got := backend.WeekStartOf("2026-08-16"); got != "2026-08-10" { // a Sunday
		t.Fatalf("Sunday should close the week that began on the 10th, got %s", got)
	}
	if got := backend.WeekStartOf("2026-08-10"); got != "2026-08-10" {
		t.Fatalf("a Monday is its own week start, got %s", got)
	}
	days := shell.WeekDates("2026-08-10")
	if len(days) != 7 || days[0] != "2026-08-10" || days[6] != "2026-08-16" {
		t.Fatalf("a week is seven days Mon–Sun, got %v", days)
	}
	if got := shell.ShiftWeek("2026-08-10", -1); got != "2026-08-03" {
		t.Fatalf("back a week is seven days, got %s", got)
	}

	if got := shell.WeekRangeLabel("2026-08-10"); got != "10 – 16 Aug 2026" {
		t.Fatalf("a week inside one month names it once, got %q", got)
	}
	// Crossing a month has to say both, or the label reads as an impossible range.
	if got := shell.WeekRangeLabel("2026-08-31"); got != "31 Aug – 6 Sep 2026" {
		t.Fatalf("a week across two months names both, got %q", got)
	}
}

// weekUI is a Log tab with the strip parked on a fixed week, so a test never
// depends on which week it is run in.
//
// No project URL by default: the strip asks GitHub for any month it has not got,
// and a test that wanders into an uncached month would shell out to gh for real
// — in the background, landing on whatever test happened to be running by then.
// A test that wants banked minutes sets the URL and seeds the months it names.
func weekUI(t *testing.T) *shell.UI {
	t.Helper()
	ui := pendingUI(t)
	ui.Cfg = backend.Config{Repos: []string{"exampleorg"}}
	ui.WeekStart = "2026-08-10" // Monday
	return ui
}

// withProject points a test UI at a project and seeds the months it may read,
// this month included: the foot of the tab scores today whatever week is shown.
func withProject(ui *shell.UI, months ...string) {
	ui.Cfg.ProjectURL = "https://github.com/orgs/exampleorg/projects/9"
	ui.EnsureProjectCache()
	for _, m := range append(months, shell.ThisMonth()) {
		key := shell.StatusCacheKey(m)
		if !ui.ProjLoaded[key] {
			ui.ProjLoaded[key] = true
		}
	}
}

// The strip is the week as it stands: seven days, what GitHub already holds on
// each, how much of the 480 that is, and the entries themselves.
func TestWeekStripShowsWhatEachDayHolds(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	withProject(ui, "2026-08")
	key := shell.StatusCacheKey("2026-08")
	ui.ProjItems[key] = []backend.WorklogItem{
		{Date: "2026-08-12", Minutes: 240, ParentTitle: "DOC ITEM ENHANCEMENT",
			URL: "https://github.com/exampleorg/sample-intranet/issues/42"},
		{Date: "2026-08-13", Minutes: 480, Title: "Support rota",
			URL: "https://github.com/exampleorg/sample-intranet/issues/9"},
	}
	ui.ProjLoaded[key] = true
	if shell.ThisMonth() != "2026-08" {
		// The line at the foot of the tab scores today's month, which the seeded
		// August above only covers while the clock says August.
		ui.ProjLoaded[shell.StatusCacheKey(shell.ThisMonth())] = true
	}

	// One entry saved here and never pushed, sitting on the Monday.
	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-10", "minutes": "45", "description": "standup notes",
	}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)

	got := labels(ui.WeekBox)
	if !contains(got, "10 – 16 Aug 2026") {
		t.Fatalf("the strip should name the week it is showing: %v", got)
	}
	for _, day := range []string{"Mon 10", "Tue 11", "Sun 16"} {
		if !contains(got, day) {
			t.Fatalf("the week is missing %s: %v", day, got)
		}
	}
	if n := len(ui.WeekCols); n != 7 {
		t.Fatalf("a week is seven columns, got %d", n)
	}

	// 240 of 480 is half the day, and what it still owes is spelled out rather
	// than left as arithmetic on the percentage.
	if !contains(got, "240 min · 50% (240m left)") {
		t.Fatalf("a day should say what it still owes: %v", got)
	}
	if !contains(got, "480 min · 100% ✓") {
		t.Fatalf("a day that made target should be marked, with nothing left: %v", got)
	}
	if !contains(got, "nothing logged") {
		t.Fatalf("an empty day should say so rather than read as broken: %v", got)
	}
	// Work already on GitHub is counted, not listed: it is the bar and the score,
	// and listing it again buried the one card there is anything left to do to.
	//
	// Checked against the day columns alone. The by-issue panel below the strip
	// does name that work, on purpose — it is the week's summary — so scanning
	// the whole tab would now catch it there and say nothing about the columns.
	var inColumns []string
	for _, col := range ui.WeekCols {
		inColumns = append(inColumns, labels(col)...)
	}
	if contains(inColumns, "DOC ITEM") || contains(inColumns, "Support rota") {
		t.Fatalf("pushed worklogs should not be listed on the day: %v", inColumns)
	}
	// The unpushed entry shows on its day too, counted apart from the banked
	// minutes so the day's real total is never overstated, and totalled beside
	// them so the day it would come to is there to read.
	if !contains(got, "+45 min") {
		t.Fatalf("locally saved minutes should be called out separately: %v", got)
	}
	if !contains(got, "45 min · 9%") {
		t.Fatalf("the day should also score what it would come to: %v", got)
	}
	// The waiting entry is not a chip but the card itself, standing on its day
	// with the buttons that act on it.
	if !contains(got, "standup notes") {
		t.Fatalf("an unpushed entry should stand on its day: %v", got)
	}
	if !contains(got, "Push") {
		t.Fatalf("the card on the day should carry its own Push: %v", got)
	}
	if n := len(dragTilesIn(ui.WeekBox)); n != 1 {
		t.Fatalf("the card on the day should be the draggable one, got %d", n)
	}
	// And it is not repeated in the list underneath.
	if below := labels(ui.RecentBox); contains(below, "standup notes") {
		t.Fatalf("an entry on the week above should not be listed twice: %v", below)
	}
	if !contains(labels(ui.RecentBox), "1 on the week below") {
		t.Fatalf("the list should account for what it is no longer showing: %v", labels(ui.RecentBox))
	}
}

// A day holding several waiting cards stacks them biggest first, so the one
// worth the most minutes is the one at the top of the column.
func TestTheBiggestCardStandsAtTheTopOfItsDay(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	// Saved in the order they were typed, which is not the order they are worth.
	if _, err := ui.Store.AppendRows([]backend.Row{
		{"date": "2026-08-11", "minutes": "120", "description": "smallest of three"},
		{"date": "2026-08-11", "minutes": "240", "description": "biggest of three"},
		{"date": "2026-08-11", "minutes": "220", "description": "middling of three"},
	}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)

	got := labels(ui.WeekCols["2026-08-11"])
	if !inOrder(got, "240 min", "220 min", "120 min") {
		t.Fatalf("the day should stack its cards 240, 220, 120: %v", got)
	}
	// Descending by minutes, not by what the card says: the descriptions follow
	// their own cards rather than the order they were saved in.
	if !inOrder(got, "biggest of three", "middling of three", "smallest of three") {
		t.Fatalf("each card should carry its own description down the column: %v", got)
	}
}

// The card standing on a day says what it is worth and what it points at, both
// in full: a cut-off issue ref names no repo in particular.
func TestTheCardOnADaySpellsItselfOut(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	const ref = "exampleorg/sample-client-three#490"
	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-11", "minutes": "90", "description": "sample client fixes",
		"issue": ref,
	}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)

	got := labels(ui.WeekBox)
	if !contains(got, ref) {
		t.Fatalf("the issue ref should be readable in full, not elided: %v", got)
	}
	if !contains(got, "90 min") {
		t.Fatalf("the minutes are the number the card carries: %v", got)
	}
	for _, want := range []string{"Push", "sample client fixes"} {
		if !contains(got, want) {
			t.Fatalf("the card is missing %q: %v", want, got)
		}
	}
}

func TestDayCardDistinguishesIncompletePushFromRemainingCommits(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	const ref = "exampleorg/repo#7"
	ui.Pending.Groups = []backend.Group{{Issue: ref, Commits: []backend.Commit{{Sha: "abc1234"}}}}
	withCommitLeft := labels(logwork.DayRowCard(ui, backend.Row{
		"date": "2026-08-11", "minutes": "30", "issue": ref,
	}, func() {}))
	if !contains(withCommitLeft, "commits remaining") || contains(withCommitLeft, "partly saved") {
		t.Fatalf("unlogged commits should not look like a failed push: %v", withCommitLeft)
	}

	partial := backend.Row{
		"date": "2026-08-11", "minutes": "30", "issue": "exampleorg/repo#8",
		"issue_url": "https://github.com/exampleorg/repo/issues/8", "item_id": "PVTI_8",
	}
	partlySaved := labels(logwork.DayRowCard(ui, partial, func() {}))
	if !contains(partlySaved, "partly saved") || contains(partlySaved, "commits remaining") {
		t.Fatalf("an incomplete GitHub push should carry the partly-saved tag: %v", partlySaved)
	}
	if got := labels(logwork.RowTile(ui, partial, func() {})); !contains(got, "partly saved on GitHub — retry push") {
		t.Fatalf("the saved-entry bubble should explain the retry state: %v", got)
	}
}

func TestBulkReviewIssueRefShowsPartlySavedTag(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	row := backend.Row{
		"date": "2026-09-21", "minutes": "85", "type": backend.KindBulkReview,
		"issue": "exampleorg/sample-general-tasks#10440",
	}
	for _, card := range []fyne.CanvasObject{logwork.DayRowCard(ui, row, func() {}), logwork.RowTile(ui, row, func() {})} {
		if got := labels(card); !contains(got, "partly saved") {
			t.Fatalf("a created daily issue waiting for Worklog fields needs a visible tag: %v", got)
		}
	}

	row["issue"] = ""
	if got := labels(logwork.DayRowCard(ui, row, func() {})); contains(got, "partly saved") {
		t.Fatalf("a local-only draft must not have a partial-save tag: %v", got)
	}
	row["issue"] = "exampleorg/sample-general-tasks#10440"
	row["pushed_at"] = "2026-09-21T21:00:00"
	if got := labels(logwork.DayRowCard(ui, row, func() {})); contains(got, "partly saved") {
		t.Fatalf("a completed push must not have a partial-save tag: %v", got)
	}
}

// Dragging only patches the row date. The card title must therefore derive
// from that current date rather than replaying the description saved earlier.
func TestDayCardManualTitleUsesCurrentDate(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	ui.Cfg.WorklogOwner = "team-alex"

	row := backend.Row{
		"date": "2026-09-18", "minutes": "20", "type": backend.KindBulkReview,
		"description": "Alex Code Review: 2026-09-15",
		"remarks":     "https://github.com/o/r/pull/1 (20m)",
	}
	got := labels(logwork.DayRowCard(ui, row, func() {}))
	if !contains(got, "Alex Code Review: 2026-09-18") {
		t.Fatalf("day card kept the saved date instead of the current one: %v", got)
	}
	if contains(got, "Alex Code Review: 2026-09-15") {
		t.Fatalf("stale description leaked into the day card: %v", got)
	}
}

// greenness is how far a colour leans towards green, used to tell a finished day
// from an unfinished one without pinning the test to an exact theme colour.
func greenness(c color.Color) float64 {
	r, g, b, a := c.RGBA()
	if a == 0 {
		return 0
	}
	return float64(g) - float64(r+b)/2
}

// A day that made its target is green, so a week reads without arithmetic. One
// that only makes it by counting what is still waiting is a paler green, because
// that is a paler claim.
func TestFinishedDaysGoGreen(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	withProject(ui, "2026-08")
	key := shell.StatusCacheKey("2026-08")
	ui.ProjItems[key] = []backend.WorklogItem{
		{Date: "2026-08-12", Minutes: 480, Title: "a full day",
			URL: "https://github.com/exampleorg/sample-intranet/issues/9"},
	}
	ui.ProjLoaded[key] = true
	// The Monday reaches the target only on drafts.
	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-10", "minutes": "480", "description": "not sent yet",
	}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)

	pushed := greenness(ui.WeekCols["2026-08-12"].BaseFill)
	draft := greenness(ui.WeekCols["2026-08-10"].BaseFill)
	empty := greenness(ui.WeekCols["2026-08-11"].BaseFill)
	if pushed <= 0 {
		t.Fatalf("a day banked on GitHub should read as done, got %v", pushed)
	}
	if draft <= 0 || draft >= pushed {
		t.Fatalf("a day finished only on drafts should be a lighter green: draft %v, pushed %v",
			draft, pushed)
	}
	if empty != 0 {
		t.Fatalf("a day with nothing on it should stay unpainted, got %v", empty)
	}
}

// Carrying a card over a day makes that day answer: it breathes towards the
// colour it would take if the card landed there, and settles back when it does
// not.
func TestTheDayUnderACardBreathes(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-10", "minutes": "60", "description": "triage",
	}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	tile := dragTilesIn(ui.WeekBox)[0]

	wed := ui.WeekCols["2026-08-12"]
	if wed.Pulse != nil {
		t.Fatal("a day nothing is being carried over should sit still")
	}
	tile.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(4, -20)})
	wed.MouseIn(&desktop.MouseEvent{})
	if wed.Pulse == nil {
		t.Fatal("the day under the carried card should be animating")
	}
	if other := ui.WeekCols["2026-08-13"]; other.Pulse != nil {
		t.Fatal("only the day under the card should be animating")
	}

	wed.MouseOut()
	if wed.Pulse != nil {
		t.Fatal("the card moved on, so the day should have settled")
	}
	if wed.Frame.FillColor != wed.BaseFill {
		t.Fatal("a day that settles goes back to the colour it earned on its own")
	}
	tile.DragEnd()
}

// Nothing configured to score against: the strip still draws, and says why it
// is empty rather than showing every day as a day with nothing done.
func TestWeekStripWithNoProjectSaysSo(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()
	ui.WeekStart = "2026-08-10"
	logwork.DrawWeekStrip(ui)

	if got := labels(ui.WeekBox); !contains(got, "Set a project URL in Settings") {
		t.Fatalf("with no project the strip should explain itself: %v", got)
	}
}

// dragTilesIn collects the cards that can be picked up.
func dragTilesIn(o fyne.CanvasObject) []*logwork.DragTile {
	var out []*logwork.DragTile
	walk(o, func(c fyne.CanvasObject) {
		if d, ok := c.(*logwork.DragTile); ok {
			out = append(out, d)
		}
	})
	return out
}

// Dragging a saved card onto a day is how its date is set: the drop writes the
// date, so the Push button sends the day it was dropped on.
func TestDroppingACardMovesItToThatDay(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	made, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-10", "minutes": "90", "description": "fix the picker",
		"issue": "exampleorg/sample-intranet#42",
	}})
	if err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)

	// The card stands on the Monday it is filed under, in the strip itself.
	tiles := dragTilesIn(ui.WeekBox)
	if len(tiles) != 1 {
		t.Fatalf("an unpushed entry should be a card that can be picked up, got %d", len(tiles))
	}
	tile := tiles[0]

	// Picked up, carried over the Wednesday, dropped.
	tile.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(4, -60)})
	if ui.DragRow == nil {
		t.Fatal("the strip has to know a card is in the air, or nothing lights up")
	}
	if ui.DragGhost == nil {
		t.Fatal("the carried card should be drawn above the tab while it is in the air")
	}
	target := ui.WeekCols["2026-08-12"]
	target.MouseIn(&desktop.MouseEvent{})
	if target.Frame.StrokeColor != theme.Color(theme.ColorNamePrimary) {
		t.Fatal("the day under the card should be lit while it is being carried")
	}
	if other := ui.WeekCols["2026-08-13"]; other.Frame.StrokeColor == theme.Color(theme.ColorNamePrimary) {
		t.Fatal("only the day under the card should be lit")
	}
	tile.DragEnd()

	rows, err := ui.Store.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["date"] != "2026-08-12" {
		t.Fatalf("the drop should have moved the entry to the Wednesday, got %v", rows)
	}
	// Everything else about the entry is untouched — a drop sets a date, no more.
	if rows[0]["minutes"] != "90" || rows[0]["id"] != made[0]["id"] {
		t.Fatalf("a drop should change the date and nothing else, got %v", rows[0])
	}
	if ui.DragRow != nil || ui.HoverDay != "" {
		t.Fatal("the drop should have put the card down")
	}
	if ui.DragGhost != nil || len(ui.DragLayer.Objects) != 0 {
		t.Fatal("the carried card should be gone once it lands")
	}
}

// Let go over nothing, or over the day it already sits on: the entry stays as it
// was. A drag that missed must not quietly rewrite a date.
func TestDropOnNothingLeavesTheDateAlone(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-10", "minutes": "30", "description": "triage",
	}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	tile := dragTilesIn(ui.WeekBox)[0]

	tile.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, -20)})
	tile.DragEnd() // never over a day
	rows, _ := ui.Store.ReadRows()
	if rows[0]["date"] != "2026-08-10" {
		t.Fatalf("a drop on nothing should change nothing, got %s", rows[0]["date"])
	}

	tile = dragTilesIn(ui.WeekBox)[0]
	tile.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, -20)})
	ui.WeekCols["2026-08-10"].MouseIn(&desktop.MouseEvent{})
	tile.DragEnd() // back on the day it started on
	rows, _ = ui.Store.ReadRows()
	if len(rows) != 1 || rows[0]["date"] != "2026-08-10" {
		t.Fatalf("dropping a card where it already is changes nothing, got %v", rows)
	}
}

// A pushed entry is a date already on the board. It stays where it is rather
// than being dragged into disagreeing with GitHub.
func TestPushedEntriesCannotBeDragged(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	// Parked on a week neither entry belongs to, so both are in the list below
	// and this is a test of the list, not of the strip.
	ui.WeekStart = "2020-01-06"

	if _, err := ui.Store.AppendRows([]backend.Row{
		{"date": shell.Today(), "minutes": "60", "description": "already sent",
			"pushed_at": "2026-08-11T10:00:00"},
		{"date": shell.Today(), "minutes": "60", "description": "still here"},
	}); err != nil {
		t.Fatal(err)
	}
	ui.ShowPushed.SetChecked(true) // both on show
	logwork.DrawRecent(ui)

	tiles := dragTilesIn(ui.RecentBox)
	if len(tiles) != 1 {
		t.Fatalf("only the unpushed entry should be draggable, got %d", len(tiles))
	}
	if tiles[0].Row["description"] != "still here" {
		t.Fatalf("the wrong card is draggable: %v", tiles[0].Row)
	}
}

// The strip walks the weeks and comes back: a day two weeks out is reachable
// without touching the system clock.
func TestWeekStripWalksTheWeeks(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	// Two weeks back, wherever the clock happens to be standing.
	ui.WeekStart = shell.ShiftWeek(backend.WeekStartOf(shell.Today()), -2)
	logwork.DrawWeekStrip(ui)

	back := buttonNamed(ui.WeekBox, "This week")
	if back == nil {
		t.Fatal("the strip needs a way home from wherever it wandered to")
	}
	if back.Disabled() {
		t.Fatal("parked on another week, the way home should be live")
	}
	back.OnTapped()
	if ui.WeekStart != backend.WeekStartOf(shell.Today()) {
		t.Fatalf("home is the week today falls in, got %s", ui.WeekStart)
	}
	if b := buttonNamed(ui.WeekBox, "This week"); b == nil || !b.Disabled() {
		t.Fatal("already home, the button has nothing left to do")
	}
}

// A week that straddles two months reads both of them, or the days either side
// of the 1st would show as empty when they are not.
func TestWeekAcrossTwoMonthsReadsBoth(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	ui.WeekStart = "2026-08-31" // Mon 31 Aug – Sun 6 Sep

	withProject(ui, "2026-08", "2026-09")
	for month, item := range map[string]backend.WorklogItem{
		"2026-08": {Date: "2026-08-31", Minutes: 120, Title: "August tail"},
		"2026-09": {Date: "2026-09-01", Minutes: 300, Title: "September head"},
	} {
		key := shell.StatusCacheKey(month)
		ui.ProjItems[key] = []backend.WorklogItem{item}
		ui.ProjLoaded[key] = true
	}
	logwork.DrawWeekStrip(ui)

	got := labels(ui.WeekBox)
	if !contains(got, "120 min") || !contains(got, "300 min") {
		t.Fatalf("both months' days should be filled in: %v", got)
	}
	if !contains(got, strconv.Itoa(31)) {
		t.Fatalf("the week should still start on the 31st: %v", got)
	}
}
