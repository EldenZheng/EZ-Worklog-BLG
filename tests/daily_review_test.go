package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

const reviewFirst = "https://github.com/exampleorg/accounts/pull/16 (20m)\nhttps://github.com/exampleorg/customer/pull/49 (10m)"
const reviewSecond = "https://github.com/exampleorg/invoice/pull/6 (15m)\nhttps://github.com/exampleorg/invoice/pull/7 (15m)"

func reviewSnapshot(body, remarks string, minutes int) string {
	bodyJSON, _ := json.Marshal(body)
	remarksJSON, _ := json.Marshal(remarks)
	return fmt.Sprintf(`{"data":{"repository":{"issue":{"url":"https://github.com/exampleorg/tasks/issues/1","body":%s,
 "projectItems":{"nodes":[{"id":"item","project":{"id":"project","number":9,"fields":{"nodes":[
 {"id":"date","name":"Worklog (Date)","dataType":"DATE"},
 {"id":"owner","name":"Worklog Owner (blg-xxxx)","dataType":"TEXT"},
 {"id":"mins","name":"Worklog (mins)","dataType":"NUMBER"},
 {"id":"remarks","name":"Worklog (Remarks)","dataType":"TEXT"}]}},"fieldValues":{"nodes":[
 {"date":"2026-11-10","field":{"name":"Worklog (Date)"}},
 {"text":"team-alex","field":{"name":"Worklog Owner (blg-xxxx)"}},
 {"number":%d,"field":{"name":"Worklog (mins)"}},
 {"text":%s,"field":{"name":"Worklog (Remarks)"}}]}}]}}}}}`, bodyJSON, minutes, remarksJSON)
}

func reviewBatch() backend.Row {
	return backend.Row{"date": "2026-11-10", "type": backend.KindBulkReview,
		"issue": "exampleorg/tasks#1", "mode": "issue", "owner": "team-alex",
		"minutes": "30", "remarks": reviewSecond}
}

func TestDailyReviewCombinesDescriptionRemarksAndIncomingWithoutCountingRetriesTwice(t *testing.T) {
	oldGh, oldPush := backend.Gh, backend.PushEntry
	t.Cleanup(func() { backend.Gh, backend.PushEntry = oldGh, oldPush })
	body, remarks, minutes := reviewFirst, reviewSecond, 30
	backend.Gh = func(args []string, headers ...string) (string, error) {
		if args[0] == "api" {
			return reviewSnapshot(body, remarks, minutes), nil
		}
		if len(args) == 5 && args[0] == "issue" && args[1] == "edit" && args[3] == "--body" {
			body = args[4]
			return "", nil
		}
		t.Fatalf("unexpected GitHub call: %v", args)
		return "", nil
	}
	writes := 0
	backend.PushEntry = func(_ backend.Config, ref, date, owner string, mins int, rem, desc, mode, resume, title string, progress ...func(backend.FetchProgress)) (backend.PushResult, error) {
		writes++
		if mins != 60 || rem != reviewFirst+"\n"+reviewSecond || desc != rem || mode != "issue" {
			t.Fatalf("combined push = %dm, remarks %q, description %q, mode %q", mins, rem, desc, mode)
		}
		// The first attempt writes the description but fails on the board.
		if writes == 1 {
			return backend.PushResult{URL: "https://github.com/exampleorg/tasks/issues/1", Problems: []string{"board unavailable"}}, nil
		}
		remarks, minutes = rem, mins
		return backend.PushResult{URL: "https://github.com/exampleorg/tasks/issues/1", ItemID: "item"}, nil
	}
	row := reviewBatch()
	for attempt := 0; attempt < 3; attempt++ {
		plan, err := backend.PrepareDailyReviewPush(row)
		if err != nil || plan.Minutes != 60 || !plan.NeedsCombine {
			t.Fatalf("attempt %d plan = %+v, err=%v", attempt, plan, err)
		}
		res, err := backend.PushDailyReview(backend.Config{}, plan)
		if err != nil || res.Complete() != (attempt != 0) {
			t.Fatalf("attempt %d result = %+v, err=%v", attempt, res, err)
		}
	}
	if row.Minutes() != 30 || row["remarks"] != reviewSecond {
		t.Fatal("combining must leave the saved batch intact, rather than turning it into a duplicate daily total")
	}
}

func TestDailyReviewRefusesChangedRemoteStateBeforeWriting(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	snapshot := reviewSnapshot(reviewFirst, reviewFirst, 30)
	backend.Gh = func(args []string, headers ...string) (string, error) {
		if args[0] != "api" {
			t.Fatalf("changed review must not be written: %v", args)
		}
		return snapshot, nil
	}
	plan, err := backend.PrepareDailyReviewPush(reviewBatch())
	if err != nil {
		t.Fatal(err)
	}
	snapshot = reviewSnapshot(reviewFirst+"\nhttps://github.com/exampleorg/other/pull/3 (10m)", reviewFirst, 40)
	if _, err := backend.PushDailyReview(backend.Config{}, plan); err == nil || !strings.Contains(err.Error(), "changed on GitHub") {
		t.Fatalf("expected changed-state refusal, got %v", err)
	}
}

func TestDailyReviewBlocksAmbiguousOrUnexplainedTime(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	for _, c := range []struct {
		body, remarks string
		mins          int
	}{
		{reviewFirst, reviewFirst, 90},
		{"Review notes without a PR list", reviewFirst, 30},
		{reviewFirst, strings.Replace(reviewFirst, "(20m)", "(25m)", 1), 35},
	} {
		backend.Gh = func(args []string, headers ...string) (string, error) {
			return reviewSnapshot(c.body, c.remarks, c.mins), nil
		}
		if _, err := backend.PrepareDailyReviewPush(reviewBatch()); err == nil {
			t.Fatalf("unsafe state should be refused: %+v", c)
		}
	}
}

func TestDailyReviewPushRequiresCombineChoiceAndCancelDoesNotWrite(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	oldGh, oldPush, oldInline := backend.Gh, backend.PushEntry, shell.RunAsyncInline
	shell.RunAsyncInline = true
	t.Cleanup(func() { backend.Gh, backend.PushEntry, shell.RunAsyncInline = oldGh, oldPush, oldInline })
	writes := 0
	backend.Gh = func(args []string, headers ...string) (string, error) {
		if args[0] == "api" {
			return reviewSnapshot(reviewFirst, reviewFirst, 30), nil
		}
		writes++
		return "", errors.New("unexpected write")
	}
	backend.PushEntry = func(backend.Config, string, string, string, int, string, string, string, string, string, ...func(backend.FetchProgress)) (backend.PushResult, error) {
		writes++
		return backend.PushResult{}, nil
	}
	if _, err := ui.Store.AppendRows([]backend.Row{reviewBatch()}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	push := buttonNamed(ui.RecentBox, "Push")
	if push == nil {
		push = buttonNamed(ui.WeekBox, "Push")
	}
	if push == nil {
		t.Fatal("saved review needs a push button")
	}
	test.Tap(push)
	waitForButton(t, ui.Win, "Combine & push")
	if writes != 0 {
		t.Fatal("must wait for the combine choice before writing")
	}
	test.Tap(waitForButton(t, ui.Win, "Cancel"))
	rows, err := ui.Store.ReadRows()
	if err != nil || len(rows) != 1 || rows[0]["pushed_at"] != "" || writes != 0 {
		t.Fatalf("cancel must retain an unpushed draft and leave GitHub unchanged: rows=%v writes=%d err=%v", rows, writes, err)
	}
}

func TestDailyReviewCombineChoicePushesBothListsAndKeepsLocalBatch(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	oldGh, oldPush, oldInline := backend.Gh, backend.PushEntry, shell.RunAsyncInline
	shell.RunAsyncInline = true
	t.Cleanup(func() { backend.Gh, backend.PushEntry, shell.RunAsyncInline = oldGh, oldPush, oldInline })
	writes, pushes := 0, 0
	backend.Gh = func(args []string, headers ...string) (string, error) {
		if args[0] == "api" {
			return reviewSnapshot(reviewFirst, reviewFirst, 30), nil
		}
		if args[0] != "issue" || args[1] != "edit" || args[4] != reviewFirst+"\n"+reviewSecond {
			t.Fatalf("description must contain both review batches: %v", args)
		}
		writes++
		return "", nil
	}
	backend.PushEntry = func(_ backend.Config, ref, date, owner string, mins int, rem, desc, mode, resume, title string, progress ...func(backend.FetchProgress)) (backend.PushResult, error) {
		pushes++
		if mins != 60 || rem != reviewFirst+"\n"+reviewSecond || desc != rem {
			t.Fatalf("combined board payload = %dm, %q, %q", mins, rem, desc)
		}
		return backend.PushResult{URL: "https://github.com/exampleorg/tasks/issues/1", ItemID: "item"}, nil
	}
	if _, err := ui.Store.AppendRows([]backend.Row{reviewBatch()}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	push := buttonNamed(ui.RecentBox, "Push")
	if push == nil {
		push = buttonNamed(ui.WeekBox, "Push")
	}
	if push == nil {
		t.Fatal("saved review needs a push button")
	}
	test.Tap(push)
	test.Tap(waitForButton(t, ui.Win, "Combine & push"))
	rows, err := ui.Store.ReadRows()
	if err != nil || len(rows) != 1 || rows[0]["pushed_at"] == "" || rows[0].Minutes() != 30 || rows[0]["remarks"] != reviewSecond || writes != 1 || pushes != 1 {
		t.Fatalf("combine should mark only this 30m batch pushed: rows=%v writes=%d pushes=%d err=%v", rows, writes, pushes, err)
	}
}

func TestDailyReviewFirstPushDoesNotNeedCombineChoice(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	backend.Gh = func(args []string, headers ...string) (string, error) {
		bodyJSON, _ := json.Marshal(reviewSecond)
		return fmt.Sprintf(`{"data":{"repository":{"issue":{"url":"https://github.com/exampleorg/tasks/issues/1","body":%s,"projectItems":{"nodes":[]}}}}}`, bodyJSON), nil
	}
	plan, err := backend.PrepareDailyReviewPush(reviewBatch())
	if err != nil || plan.NeedsCombine || plan.Minutes != 30 || plan.Body != reviewSecond {
		t.Fatalf("first push should proceed with only its own PRs: plan=%+v err=%v", plan, err)
	}
}
