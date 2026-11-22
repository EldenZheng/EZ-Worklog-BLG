package tests

import (
	"image/color"
	"os"
	"path/filepath"
	"strconv"
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

	"worklog/internal/menus/logwork"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestWorkDayTurnsOverAtMalaysiaMidnight(t *testing.T) {
	midnight := time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC)
	if got := backend.WorkDate(midnight.Add(-time.Nanosecond)); got != "2026-11-20" {
		t.Fatalf("just before midnight MYT: %s", got)
	}
	for _, moment := range []time.Time{midnight, midnight.Add(7*time.Hour + 59*time.Minute)} {
		if got := backend.WorkDate(moment); got != "2026-11-21" {
			t.Fatalf("%s: Malaysia date = %s", moment, got)
		}
	}
}

func TestOpenWorklogFormFollowsTodayUntilADateIsChosen(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	today := shell.Today()
	if len(ui.LogDayListeners) != 1 {
		t.Fatalf("expected one mounted date picker, got %d", len(ui.LogDayListeners))
	}
	for object, refresh := range ui.LogDayListeners {
		picker := object.(*logwork.WorklogDatePicker)
		picker.Set(backend.AddDays(today, -1))
		refresh()
		if picker.ISO() != today {
			t.Fatalf("new-entry default after midnight = %s", picker.ISO())
		}
		tomorrow := backend.AddDays(today, 1)
		picker.Entry.SetText(tomorrow)
		refresh()
		if picker.ISO() != tomorrow {
			t.Fatalf("explicitly selected date changed to %s", picker.ISO())
		}
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
		Minutes:     180,
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
			[]backend.WorklogItem{{Title: "Alex: Weekly Support (28th August)", Minutes: 180}}, 0},
		// The rota creates every weekday's slot up front at 0 minutes; an empty
		// slot is not a logged shift.
		{"an empty rota slot is not the shift", true, "2026-08-28",
			[]backend.WorklogItem{{Title: "Alex: Weekly Support (28th August, 12PM - 3PM)"}}, backend.SupportPlaceholderMins},
		{"other work on the day is not the shift", true, "2026-08-24",
			[]backend.WorklogItem{{ParentTitle: "Chinese translation"}}, backend.SupportPlaceholderMins},
		// The rota is weekdays; a weekend is its own arrangement.
		{"Saturday", true, "2026-08-29", nil, 0},
		{"Sunday", true, "2026-08-30", nil, 0},
		{"switched off", false, "2026-08-24", nil, 0},
		// The user's support week: the rota slot turns it on without a tick.
		{"a rota slot needs no tick", false, "2026-08-28",
			[]backend.WorklogItem{{Title: "Alex: Weekly Support (28th August, 12PM - 3PM)"}}, backend.SupportPlaceholderMins},
		{"a logged rota slot is the shift", false, "2026-08-28", support, 0},
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
	// Content assertions use the spacious layout; narrow-column geometry has
	// separate coverage in small_screen_test.go.
	ui.Win.Resize(fyne.NewSize(2400, 1000))
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
	if !contains(got, shell.ShortIssueRef(ref)) || contains(got, ref) {
		t.Fatalf("the day card should show repo#number without its owner: %v", got)
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
	if !contains(withCommitLeft, "Commits remaining") || contains(withCommitLeft, "Partially pushed") {
		t.Fatalf("unlogged commits should not look like a failed push: %v", withCommitLeft)
	}

	partial := backend.Row{
		"date": "2026-08-11", "minutes": "30", "issue": "exampleorg/repo#8",
		"issue_url": "https://github.com/exampleorg/repo/issues/8", "item_id": "PVTI_8",
	}
	partlySaved := labels(logwork.DayRowCard(ui, partial, func() {}))
	if !contains(partlySaved, "Partially pushed") || contains(partlySaved, "Commits remaining") {
		t.Fatalf("an incomplete GitHub push should carry the partly-saved tag: %v", partlySaved)
	}
	if got := labels(logwork.RowTile(ui, partial, func() {})); !contains(got, "partly saved on GitHub — retry push") {
		t.Fatalf("the saved-entry bubble should explain the retry state: %v", got)
	}
}

func TestSingleReviewCardsShowPurpleOutlineByTime(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	for _, kind := range []string{backend.KindCodeReview, backend.KindBulkReview, backend.KindOther} {
		row := backend.Row{"date": "2026-08-11", "minutes": "30", "issue": "exampleorg/repo#7", "type": kind}
		for _, card := range []fyne.CanvasObject{logwork.DayRowCard(ui, row, func() {}), logwork.RowTile(ui, row, func() {})} {
			if got := contains(labels(card), "Code review"); got != (kind == backend.KindCodeReview) {
				t.Fatalf("review badge for %s: %v", kind, labels(card))
			}
			if kind == backend.KindCodeReview {
				assertOutlineTag(t, card, "Code review", shell.PlannerPlanColor)
			}
		}
	}
	legacy := backend.Row{"type": backend.KindCodeReview, "remarks": "https://github.com/o/r/pull/1 (30m)"}
	for _, card := range []fyne.CanvasObject{logwork.DayRowCard(ui, legacy, func() {}), logwork.RowTile(ui, legacy, func() {})} {
		if contains(labels(card), "Code review") {
			t.Fatal("legacy bulk reviews must not get a single-review badge")
		}
	}
}

func TestOlderSavedReviewsGetBadgeWithoutBadgingBulk(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	for _, tc := range []struct {
		name, kind, description, remarks string
		want                             bool
	}{
		{"saved type", backend.KindCodeReview, "Old issue title", "", true},
		{"saved label", "Code Review", "Old issue title", "", true},
		{"old manual description", backend.KindOther, "Code Review: 2026-08-01", "reviewed changes", true},
		{"missing old kind", "", "Code Review: 2026-08-01", "", true},
		{"old saved name", backend.KindOther, "Code Review", "", true},
		{"bulk kind", backend.KindBulkReview, "Code Review: 2026-08-01", "", false},
		{"legacy bulk", backend.KindCodeReview, "Code Review: 2026-08-01", "https://github.com/o/r/pull/1 (30m)", false},
		{"old manual bulk", backend.KindOther, "Code Review: 2026-08-01", "https://github.com/o/r/pull/1 (30m)", false},
		{"named bulk title", backend.KindOther, "Alex Code Review: 2026-08-01", "", false},
		{"ordinary issue", backend.KindOther, "Improve Code Review flow", "", false},
		{"commit title", backend.KindCommit, "Code Review: 2026-08-01", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := backend.Row{"date": "2026-08-11", "minutes": "30", "issue": "exampleorg/repo#7", "type": tc.kind, "description": tc.description, "remarks": tc.remarks}
			for _, card := range []fyne.CanvasObject{logwork.DayRowCard(ui, row, func() {}), logwork.RowTile(ui, row, func() {})} {
				if got := contains(labels(card), "Code review"); got != tc.want {
					t.Fatalf("badge=%v, want %v: %v", got, tc.want, labels(card))
				}
			}
		})
	}
}

func TestBulkReviewCardsOmitMissingIssueReference(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	for _, kind := range []string{backend.KindBulkReview, backend.KindCodeReview} {
		row := backend.Row{"date": "2026-08-11", "minutes": "30", "type": kind, "remarks": "https://github.com/o/r/pull/1 (30m)"}
		for _, card := range []fyne.CanvasObject{logwork.DayRowCard(ui, row, func() {}), logwork.RowTile(ui, row, func() {})} {
			if got := labels(card); strings.Contains(strings.Join(got, " "), "no issue ref") || contains(got, "Code review") {
				t.Fatalf("bulk review needs neither a badge nor a missing-reference caption: %v", got)
			}
		}
		row["issue"] = "exampleorg/repo#7"
		if got := labels(logwork.DayRowCard(ui, row, func() {})); !contains(got, "repo#7") {
			t.Fatalf("a bulk review with a real issue must still show it: %v", got)
		}
	}
}

func TestSavedStatusTagsHaveGoldOutlines(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	for _, label := range []string{"partly saved", "commits remaining"} {
		assertOutlineTag(t, shell.SavedTag(label), label, widgets.LogoGold)
	}
}

func assertOutlineTag(t *testing.T, card fyne.CanvasObject, text string, ink color.Color) {
	t.Helper()
	labelFound, outlineFound := false, false
	walk(card, func(o fyne.CanvasObject) {
		if label, ok := o.(*canvas.Text); ok && label.Text == text {
			labelFound = label.Color == ink
		}
		if frame, ok := o.(*canvas.Rectangle); ok && frame.StrokeColor == ink && frame.StrokeWidth > 0 {
			_, _, _, alpha := frame.FillColor.RGBA()
			outlineFound = outlineFound || alpha == 0
		}
	})
	if !labelFound || !outlineFound {
		t.Fatalf("%q needs matching ink and a transparent outlined pill", text)
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
		if got := labels(card); !contains(got, "Partially pushed") {
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
	shell.SettleFades() // the edge fades in; look at where it settles
	if target.Frame.StrokeColor != widgets.LogoGold || target.Frame.StrokeWidth != 2 {
		t.Fatal("the day under the card should be edged gold while the drop leaves it short")
	}
	target.Pulse.Tick(1)
	if want := shell.BlendColor(theme.Color(theme.ColorNameBackground), widgets.LogoGold, backend.WeekDraftTint); !sameColor(target.Frame.FillColor, want) {
		t.Fatal("the available drop target should pulse gold")
	}
	if other := ui.WeekCols["2026-08-13"]; other.Frame.StrokeColor != theme.Color(theme.ColorNameInputBorder) || other.Frame.StrokeWidth != 1 {
		t.Fatal("other available days should retain their resting outline")
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

// The strip's own buttons redraw it. Each must come through the redraw as the
// same button, or the one that was pressed is thrown away before its press is
// seen and a fresh one stands in its place.
func TestWeekStripKeepsItsButtonsThroughARedraw(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	ui.WeekStart = shell.ShiftWeek(backend.WeekStartOf(shell.Today()), -2)
	logwork.DrawWeekStrip(ui)
	for _, name := range []string{"This week", "Refresh calendar"} {
		before := buttonNamed(ui.WeekBox, name)
		if before == nil {
			t.Fatalf("the strip has no %q button", name)
		}
		logwork.DrawWeekStrip(ui)
		if buttonNamed(ui.WeekBox, name) != before {
			t.Fatalf("%q was built again by the redraw", name)
		}
	}
	home := buttonNamed(ui.WeekBox, "This week")
	home.OnTapped()
	if buttonNamed(ui.WeekBox, "This week") != home || !home.Disabled() {
		t.Fatal("walking home should leave the pressed button standing, now with nothing to do")
	}
	// Walked away again, the same button comes back to life.
	ui.WeekStart = shell.ShiftWeek(backend.WeekStartOf(shell.Today()), -1)
	logwork.DrawWeekStrip(ui)
	if buttonNamed(ui.WeekBox, "This week") != home || home.Disabled() {
		t.Fatal("parked on another week, the way home should be live again")
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

// A week the rota puts the user on support says so: its slots are on the
// board from the start, at 0 minutes, before any of it is logged.
func TestSupportRotaNamesTheWeek(t *testing.T) {
	cfg := backend.Config{WorklogOwner: "team-alex"}
	slot := []backend.WorklogItem{{Title: "Alex: Weekly Support (28th August, 12PM - 3PM)"}}
	if !shell.OnSupportRota(cfg, slot) {
		t.Fatal("an empty rota slot still means it is the user's support week")
	}
	if shell.OnSupportRota(cfg, []backend.WorklogItem{{ParentTitle: "Chinese translation", Minutes: 60}}) {
		t.Fatal("other work is not the rota")
	}
	if shell.OnSupportRota(backend.Config{}, slot) {
		t.Fatal("with no display name there is no prefix to match on")
	}
	if shell.SupportCheckText(true) != "Support Week" {
		t.Fatalf("a support week reads %q", shell.SupportCheckText(true))
	}
	if !strings.HasPrefix(shell.SupportCheckText(false), "+180 min") {
		t.Fatalf("another week reads %q", shell.SupportCheckText(false))
	}

	// In a support week the checkbox gives way to the label.
	check := widget.NewCheck(shell.SupportCheckText(false), nil)
	view := shell.SupportChoice(check, true)
	if !view.Rota() || view.View.MinSize().Width == 0 {
		t.Fatal("a support week shows the Support Week label")
	}
	view.Set(false)
	if view.Rota() {
		t.Fatal("another week shows the checkbox again")
	}
}

// Dropping a card onto another card still lands on that card's day: the day
// is found from where the pointer is, not from hovering, which the cards in
// a day take for themselves.
func TestDroppingOntoACardLandsOnItsDay(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	if _, err := ui.Store.AppendRows([]backend.Row{
		{"date": "2026-08-10", "minutes": "60", "description": "carried"},
		{"date": "2026-08-12", "minutes": "30", "description": "already there"},
	}); err != nil {
		t.Fatal(err)
	}
	w := test.NewWindow(ui.WeekBox)
	defer w.Close()
	w.Resize(fyne.NewSize(1600, 900))
	logwork.DrawRecent(ui)

	carried := dragTilesIn(ui.WeekCols["2026-08-10"])[0]
	onWednesday := dragTilesIn(ui.WeekCols["2026-08-12"])[0]
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(onWednesday).
		Add(fyne.NewPos(onWednesday.Size().Width/2, onWednesday.Size().Height/2))
	carried.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}, Dragged: fyne.NewDelta(4, 4)})
	if ui.HoverDay != "2026-08-12" {
		t.Fatalf("over Wednesday's card the drop should aim at Wednesday, aims at %q", ui.HoverDay)
	}
	carried.DragEnd()

	rows, _ := ui.Store.ReadRows()
	for _, r := range rows {
		if r["description"] == "carried" && r["date"] != "2026-08-12" {
			t.Fatalf("the card should have landed on Wednesday, is on %s", r["date"])
		}
	}
}

// The card on a day stays lit while the pointer is on its Push or Delete
// button, not only on the card around them.
func TestDayCardStaysLitOverItsButtons(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	shell.ReleaseBubble()
	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-10", "minutes": "60", "description": "triage",
	}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)

	var area *shell.HoverArea
	type hoverButton interface {
		desktop.Hoverable
	}
	var buttons []hoverButton
	walk(ui.WeekCols["2026-08-10"], func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *shell.HoverArea:
			area = x
		}
	})
	if area == nil {
		t.Fatal("the day's card should be a hover bubble")
	}
	walkRendered(area, func(o fyne.CanvasObject) {
		switch b := o.(type) {
		case *shell.HoverButton:
			buttons = append(buttons, b)
		case *shell.FadeIconButton:
			buttons = append(buttons, b)
		}
	})
	// A click on the card edits it, so it carries Push and Delete only.
	if len(buttons) != 2 {
		t.Fatalf("the card should carry Push and Delete, found %d", len(buttons))
	}
	bubble := area.Content.(*shell.StripeBubble)
	for _, b := range buttons {
		area.MouseIn(&desktop.MouseEvent{})
		area.MouseOut() // the pointer moves onto the button
		b.MouseIn(&desktop.MouseEvent{})
		if !bubble.Lit() {
			t.Fatalf("the card went dark under its %T button", b)
		}
		b.MouseOut()
		if bubble.Lit() {
			t.Fatal("the card should go dark once the pointer leaves it")
		}
	}
}

// A card that would take the day to the target edges that day in green.
func TestADropThatFillsTheDayIsEdgedGreen(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-10", "minutes": "480", "description": "whole day",
		"issue": "exampleorg/sample-intranet#42",
	}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	tile := dragTilesIn(ui.WeekBox)[0]
	tile.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(4, -60)})
	target := ui.WeekCols["2026-08-12"]
	target.MouseIn(&desktop.MouseEvent{})
	shell.SettleFades()
	if target.Frame.StrokeColor != theme.Color(theme.ColorNameSuccess) {
		t.Fatal("a drop reaching 480 should edge the day in green")
	}
	ui.DragRow, ui.HoverDay = nil, ""
	logwork.MarkDropTargets(ui)
}

func TestLogWorkWeekdayHeadingsStayAtTheTopWhenScrolled(t *testing.T) {
	ui := weekUI(t)
	app := fyne.CurrentApp()
	defer app.Quit()
	var rows []backend.Row
	for range 16 {
		rows = append(rows, backend.Row{"date": "2026-08-10", "minutes": "15", "description": "saved work"})
	}
	if _, err := ui.Store.AppendRows(rows); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	ui.Win.Resize(fyne.NewSize(1500, 500))
	// The page scroll paints no shade over the pinned headings.
	plain := ui.Win.Content().(*fyne.Container).Objects[0].(*container.ThemeOverride)
	if c := plain.Theme.Color(theme.ColorNameShadow, theme.VariantDark); !sameColor(c, color.Transparent) {
		t.Fatal("the page scroll should not shade the weekday headings")
	}
	scroll := plain.Content.(*container.Scroll)
	head := ui.WeekCols["2026-08-10"].Heading
	toolbar := ui.WeekBox.Objects[0]
	top := app.Driver().AbsolutePositionForObject(scroll).Y
	before := app.Driver().AbsolutePositionForObject(head).Y
	if before <= top {
		t.Fatal("the header should start above the calendar in the page")
	}
	scroll.ScrollToBottom()
	pad := theme.Padding()
	for day, column := range ui.WeekCols {
		if y := app.Driver().AbsolutePositionForObject(column.Heading).Y; y != top+2*pad {
			t.Fatalf("%s heading y=%v after scrolling, viewport starts at %v", day, y, top)
		}
		// The day's outline comes down with its heading, a padding above it.
		if y := app.Driver().AbsolutePositionForObject(column.Frame).Y; y != top+pad {
			t.Fatalf("%s outline starts at y=%v after scrolling, want %v", day, y, top)
		}
		// Its outline is drawn over the cards passing behind the heading.
		if !column.Edge.Visible() || column.Edge.StrokeColor != column.Frame.StrokeColor || !column.Cover.Visible() {
			t.Fatalf("%s: cards would run over the day's top edge", day)
		}
		// As in Kanban, cards are gone by the heading's middle line.
		clipEnd := app.Driver().AbsolutePositionForObject(column.Clip).Y + column.Clip.Size().Height
		headMid := app.Driver().AbsolutePositionForObject(column.Heading).Y + column.Heading.Size().Height/2
		if !column.Clip.Visible() || clipEnd != headMid {
			t.Fatalf("%s: cards are clipped at y=%v, the heading's middle is %v", day, clipEnd, headMid)
		}
	}
	if !contains(labels(head), "nothing logged") {
		t.Fatal("the day summary should scroll with its weekday heading")
	}
	if y := app.Driver().AbsolutePositionForObject(toolbar).Y; y+toolbar.Size().Height > top {
		t.Fatal("the week toolbar should scroll off the page normally")
	}
	last := dragTilesIn(ui.WeekBox)[len(rows)-1]
	if y := app.Driver().AbsolutePositionForObject(last).Y + last.Size().Height; y > top+scroll.Size().Height {
		t.Fatalf("the last card ends below the scroll viewport at %v", y)
	}
	scroll.ScrollToTop()
	if y := app.Driver().AbsolutePositionForObject(head).Y; y != before {
		t.Fatalf("calendar header did not return to its place after scrolling up: y=%v, want %v", y, before)
	}
	if col := ui.WeekCols["2026-08-10"]; col.Frame.Position().Y != 0 || col.Edge.Visible() || col.Cover.Visible() || col.Clip.Visible() {
		t.Fatal("the outline should return to the top of the day")
	}
}

func TestLogWorkGoldDayPreviewFollowsTheHoveredDate(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	if _, err := ui.Store.AppendRows([]backend.Row{{"date": "2026-08-12", "minutes": "480", "description": "full day"}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	day := ui.WeekCols["2026-08-11"]
	if day.HeadingFill.CornerRadius != day.Frame.CornerRadius ||
		!sameColor(day.HeadingFill.FillColor, shell.BlendColor(theme.Color(theme.ColorNameBackground), color.White, 0.12)) {
		t.Fatal("the header should retain rounded corners and a lighter day surface")
	}
	dateLink := func(day *shell.DayColumn) *shell.Tappable {
		t.Helper()
		var dateLink *shell.Tappable
		walk(day.Heading, func(o fyne.CanvasObject) {
			if link, ok := o.(*shell.Tappable); ok && link.OnHover != nil {
				dateLink = link
			}
		})
		if dateLink == nil {
			t.Fatal("the weekday heading should have a date link")
		}
		return dateLink
	}
	day.MouseIn(nil)
	shell.SettleFades()
	if day.DateHoverFrame.StrokeWidth != 0 {
		t.Fatal("hovering the body of a day should leave the date preview clear")
	}
	day.MouseOut()
	dateLink(day).MouseIn(nil)
	shell.SettleFades()
	for date, col := range ui.WeekCols {
		frame := col.DateHoverFrame
		if col == day {
			if col.Frame.StrokeColor != widgets.LogoGold || frame.StrokeWidth != 0 || !sameColor(frame.FillColor, widgets.WithAlpha(widgets.LogoGold, 0.16)) {
				t.Fatal("hovering a date should show a gold outline and overlay")
			}
		} else if frame.StrokeWidth != 0 || !sameColor(frame.FillColor, color.Transparent) {
			t.Fatalf("hovering one date should leave %s clear", date)
		}
	}
	dateLink(day).MouseOut()
	shell.SettleFades()
	if day.DateHoverFrame.StrokeWidth != 0 || !sameColor(day.DateHoverFrame.FillColor, color.Transparent) {
		t.Fatal("the date preview should clear when the pointer leaves")
	}
	full := ui.WeekCols["2026-08-12"]
	dateLink(full).MouseIn(nil)
	shell.SettleFades()
	green := theme.Color(theme.ColorNameSuccess)
	headingBase := shell.BlendColor(full.BaseFill, green, 0.22)
	if full.Frame.StrokeColor != green ||
		!sameColor(full.HeadingFill.FillColor, shell.BlendColor(headingBase, green, 0.22)) ||
		!sameColor(full.DateHoverFrame.FillColor, widgets.WithAlpha(green, 0.22)) {
		t.Fatal("a completed day and its header should get a stronger green tint")
	}
	dateLink(full).MouseOut()
	shell.SettleFades()
	if l, d := luma(full.HeadingFill.FillColor), luma(full.BaseFill); l <= d {
		t.Fatalf("a day filled by drafts keeps its brighter green heading, %v against %v", l, d)
	}
	// Logged in full, the heading turns a shade darker than its day.
	withProject(ui, "2026-08")
	ui.ProjItems[shell.StatusCacheKey("2026-08")] = []backend.WorklogItem{{Date: "2026-08-13", Minutes: 480}}
	logwork.DrawRecent(ui)
	done := ui.WeekCols["2026-08-13"]
	if l, d := luma(done.HeadingFill.FillColor), luma(done.BaseFill); l >= d {
		t.Fatalf("a logged day's heading should be darker than the day, %v against %v", l, d)
	}
}

// The week lines up with the form panel above it, edge for edge.
func TestLogWorkWeekLinesUpWithTheFormPanel(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	logwork.DrawRecent(ui)
	d := fyne.CurrentApp().Driver()
	var panel fyne.CanvasObject
	walk(ui.LogBody, func(o fyne.CanvasObject) {
		if r, ok := o.(*canvas.Rectangle); ok && panel == nil && r.CornerRadius > 0 && r.Size().Width > 0 {
			panel = r
		}
	})
	if panel == nil {
		t.Fatal("no form panel")
	}
	left, right := d.AbsolutePositionForObject(panel).X, d.AbsolutePositionForObject(panel).X+panel.Size().Width
	first, last := ui.WeekCols["2026-08-10"], ui.WeekCols["2026-08-16"]
	if x := d.AbsolutePositionForObject(first).X; x != left {
		t.Fatalf("Monday starts at %v, the panel at %v", x, left)
	}
	if end := d.AbsolutePositionForObject(last).X + last.Size().Width; end-right > 0.01 || right-end > 0.01 {
		t.Fatalf("Sunday ends at %v, the panel at %v", end, right)
	}
	// The week's buttons end at its right edge, level with Sunday's; the
	// email export is Log List's alone.
	withProject(ui, "2026-08")
	logwork.DrawRecent(ui)
	var calendar fyne.CanvasObject
	walk(ui.LogBody, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok {
			switch b.Text {
			case "Refresh calendar":
				calendar = b
			case "Export week for email":
				t.Fatal("Export week for email belongs to Log List")
			}
		}
	})
	sunday := ui.WeekCols["2026-08-16"]
	endOf := func(o fyne.CanvasObject) float32 { return d.AbsolutePositionForObject(o).X + o.Size().Width }
	if calendar == nil || endOf(sunday)-endOf(calendar) > 0.5 || endOf(calendar)-endOf(sunday) > 0.5 {
		t.Fatal("Refresh calendar should end level with the week's right edge")
	}
}

// A long repo#number on a narrow card ends in an ellipsis inside its bubble.
func TestLogWorkLongIssueRefStaysInsideItsBubble(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	row := backend.Row{"date": "2026-08-12", "minutes": "30", "description": "x",
		"issue": "exampleorg/a-very-long-repository-name-for-a-narrow-card#12345"}
	card := logwork.DayRowCard(ui, row, func() {})
	w := test.NewWindow(card)
	defer w.Close()
	w.Resize(fyne.NewSize(160, 300))
	var link *shell.HoverLink
	walk(card, func(o fyne.CanvasObject) {
		if l, ok := o.(*shell.HoverLink); ok && strings.Contains(l.Text, "#12345") {
			link = l
		}
	})
	if link == nil {
		t.Fatal("no issue link on the card")
	}
	cardEnd := d0(card).X + card.Size().Width
	if end := d0(link).X + link.Size().Width; end > cardEnd {
		t.Fatalf("the link runs to %v, past the card's edge at %v", end, cardEnd)
	}
	if link.Truncation != fyne.TextTruncateEllipsis {
		t.Fatal("a long reference should be cut short with an ellipsis")
	}
	// Given room for the whole reference, it is never cut.
	w.Resize(fyne.NewSize(900, 300))
	if link.Truncation != fyne.TextTruncateOff {
		t.Fatal("a reference that fits should show in full")
	}
}

func d0(o fyne.CanvasObject) fyne.Position {
	return fyne.CurrentApp().Driver().AbsolutePositionForObject(o)
}

func TestLogWorkDayHeaderHeightsMatchAcrossScores(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	withProject(ui, "2026-08")
	ui.ProjItems[shell.StatusCacheKey("2026-08")] = []backend.WorklogItem{{Date: "2026-08-11", Minutes: 480}}
	if _, err := ui.Store.AppendRows([]backend.Row{{"date": "2026-08-12", "minutes": "240", "description": "draft"}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	for _, width := range []float32{1100, 2300} {
		ui.Win.Resize(fyne.NewSize(width, 650))
		want := float32(0)
		for _, col := range ui.WeekCols {
			want = max(want, col.HeadingContent.MinSize().Height)
		}
		for date, col := range ui.WeekCols {
			if got := col.Heading.Size().Height; got != want || col.HeadingFill.Size().Height != want {
				t.Fatalf("%s at width %g has heading height %g, tallest natural height is %g", date, width, got, want)
			}
			content := col.HeadingContent.(*fyne.Container)
			pad := theme.Padding()
			if content.Position().X != pad || col.Heading.Size().Width-content.Size().Width != 2*pad {
				t.Fatalf("%s at width %g should have equal padding inside the heading sides", date, width)
			}
			head, meter, caption := content.Objects[0], content.Objects[1], content.Objects[2]
			if meter.Position().Y-head.Size().Height != pad/2 ||
				caption.Position().Y-meter.Position().Y-meter.Size().Height != pad*1.5 {
				t.Fatalf("%s at width %g should have compact gaps around the progress bar", date, width)
			}
			// The first line of every caption starts right under the bar,
			// one line or two, side by side or stacked.
			if first := caption.(*fyne.Container).Objects[0]; first.Visible() && first.Position().Y > 0.5 {
				t.Fatalf("%s at width %g: caption starts %g lower than the others", date, width, first.Position().Y)
			}
			if below := content.Size().Height - caption.Position().Y - caption.Size().Height; below < logwork.HeadingFoot() {
				t.Fatalf("%s at width %g: only %g under the caption", date, width, below)
			}
		}
	}
}

// luma is a colour's brightness, for "darker than" checks.
func luma(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)
}
