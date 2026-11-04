package tests

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"strings"
	"testing"
	"worklog/internal/menus/loglist"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/report"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestWeeklySupportSummaryFetchesOnlyDistinctSupportRepos(t *testing.T) {
	previous := backend.Gh
	t.Cleanup(func() { backend.Gh = previous })
	calls := map[string]int{}
	backend.Gh = func(args []string, _ ...string) (string, error) {
		calls[args[1]]++
		parts := strings.Split(args[1], "/")
		return fmt.Sprintf(`{"name":%q}`, parts[len(parts)-1]), nil
	}
	var items []backend.WorklogItem
	for _, repo := range []string{"aeco-finance", "kenho-finance", "pc-image-operations", "kenho-finance"} {
		items = append(items, backend.WorklogItem{URL: "https://github.com/BigLedger-Support/" + repo + "/issues/1"})
	}
	items = append(items, backend.WorklogItem{URL: "https://github.com/BigLedger/other/issues/2"})
	got, err := logwork.SupportWeekSummary(items)
	if err != nil {
		t.Fatal(err)
	}
	want := "For the short summary this week:\n- I did some adhoc issues for AECO, Kenho, and PC Image."
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if len(calls) != 3 {
		t.Fatalf("fetched unrelated or duplicate repositories: %v", calls)
	}
	for _, count := range calls {
		if count != 1 {
			t.Fatal("repository was fetched twice")
		}
	}
	backend.Gh = func([]string, ...string) (string, error) { return "", fmt.Errorf("offline") }
	if partial, err := logwork.SupportWeekSummary(items); err == nil || partial != "" {
		t.Fatal("a failed fetch should not copy a partial customer list")
	}
}

func TestRemainingCycleProjectionFillsTodayWithoutDoubleCounting(t *testing.T) {
	cfg := backend.Config{BaseSalary: 2100}
	totals := map[string]int{"2026-11-05": 240, "2026-11-06": 120, "2026-11-07": 600}
	all, days := report.RemainingCycleProjection(cfg, totals, "2026-11-01", "2026-11-09", "2026-11-06", false)
	weekdays, wd := report.RemainingCycleProjection(cfg, totals, "2026-11-01", "2026-11-09", "2026-11-06", true)
	if days != 4 || wd != 2 || all.Receivable != 450 || weekdays.Receivable != 350 {
		t.Fatalf("remaining projections = all %.2f/%d, weekdays %.2f/%d", all.Receivable, days, weekdays.Receivable, wd)
	}
	if totals["2026-11-06"] != 120 {
		t.Fatal("projection changed actual totals")
	}
	closed, n := report.RemainingCycleProjection(cfg, totals, "2026-11-01", "2026-11-09", "2026-11-10", false)
	if n != 0 || closed.Receivable != 175 {
		t.Fatal("a closed cycle should retain the actual payable amount")
	}
}

func TestPendingBulkReviewsKeepTheirTitleAndOpenTheLogWorkEditor(t *testing.T) {
	ui := logListUI(t)
	defer fyne.CurrentApp().Quit()
	_, err := ui.Store.AppendRows([]backend.Row{{"date": "2026-08-26", "minutes": "90", "type": backend.KindBulkReview, "remarks": "review notes"}})
	if err != nil {
		t.Fatal(err)
	}
	pend := loglist.PendingByIssue(ui)
	if len(pend) != 1 || !strings.Contains(strings.ToLower(pend[0].Title), "code review") || strings.Contains(pend[0].Title, "no issue ref") {
		t.Fatalf("bulk review identity: %+v", pend)
	}
	previous := shell.Hooks.OpenPendingIssue
	t.Cleanup(func() { shell.Hooks.OpenPendingIssue = previous })
	opened := false
	shell.Hooks.OpenPendingIssue = func(_ *shell.UI, rows []backend.Row, groups []backend.Group) {
		opened = len(rows) == 1 && len(groups) == 0
	}
	loglist.DrawLogList(ui)
	walk(ui.ListBox, func(o fyne.CanvasObject) {
		if card, ok := o.(*shell.Tappable); ok && contains(labels(card), pend[0].Title) {
			test.Tap(card)
		}
	})
	if !opened {
		t.Fatal("pending review card did not route to the Log Work popup")
	}
}
