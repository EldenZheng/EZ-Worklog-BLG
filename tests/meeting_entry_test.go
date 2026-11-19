package tests

import (
	"errors"
	"strings"
	"testing"

	"fyne.io/fyne/v2"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestMissingMeetingRecordingUsesMaroonBadgeAndAllowsBlankLink(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	for _, tc := range []struct {
		name, kind, link, notes string
		missing                 bool
	}{
		{"missing", backend.KindMeeting, "", "- meeting notes", true},
		{"whitespace", backend.KindMeeting, "  ", "- meeting notes", true},
		{"recorded", backend.KindMeeting, "https://example.com/recording", "- notes", false},
		{"legacy", backend.KindMeeting, "", "notes\nMeeting Recording Link: https://example.com/recording", false},
		{"ordinary worklog", backend.KindOther, "", "- notes", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := backend.Row{"type": tc.kind, "date": shell.Today(), "minutes": "30", "meeting_title": "Planning", "meeting_recording": tc.link, "remarks": tc.notes}
			if got := backend.MissingMeetingRecording(r); got != tc.missing {
				t.Fatalf("missing recording=%v, want %v", got, tc.missing)
			}
			if _, err := backend.MeetingEntryBody(r); err != nil {
				t.Fatalf("optional recording rejected: %v", err)
			}
			for _, card := range []fyne.CanvasObject{logwork.RowTile(ui, r, func() {}), logwork.DayRowCard(ui, r, func() {})} {
				if got := contains(labels(card), "Missing Meeting Recording"); got != tc.missing {
					t.Fatalf("incorrect meeting badge: %v", labels(card))
				}
				if tc.missing {
					assertOutlineTag(t, card, "Missing Meeting Recording", shell.MissingMeetingRecordingMaroon)
				}
			}
		})
	}
}

func TestMeetingRecordingSitsBelowRemarksAndWarnsOnlyOnSavedBubble(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	var tabs *shell.UnderlineTabs
	walk(ui.Win.Content(), func(o fyne.CanvasObject) {
		if x, ok := o.(*shell.UnderlineTabs); ok {
			tabs = x
		}
	})
	tabs.SetSelected("Meeting")
	view := ui.Win.Content()
	entryByPlaceholder(view, "Meeting title (e.g. Sprint planning)").SetText("Planning")
	entryByPlaceholder(view, "minutes").SetText("30")
	notes := entryByPlaceholder(view, "- one bullet per thing done; this becomes the worklog remarks")
	notes.SetText("- meeting notes")
	assertBelow := func(form fyne.CanvasObject, notes fyne.CanvasObject) {
		t.Helper()
		if contains(labels(form), "Missing Meeting Recording") {
			t.Fatal("the reminder belongs on the saved bubble, not the input field")
		}
		recording := entryByPlaceholder(form, "https://… (meeting recording)")
		driver := fyne.CurrentApp().Driver()
		if driver.AbsolutePositionForObject(recording).Y < driver.AbsolutePositionForObject(notes).Y+notes.Size().Height {
			t.Fatal("recording field must sit below remarks")
		}
	}
	ui.Win.Resize(fyne.NewSize(1400, 1000))
	assertBelow(view, notes)
	tapButton(t, view, "Save draft")
	rows, err := ui.Store.ReadRows()
	if err != nil || len(rows) != 1 || rows[0]["meeting_recording"] != "" {
		t.Fatalf("blank recording blocked the draft: %v %v", rows, err)
	}
	if !contains(labels(logwork.RowTile(ui, rows[0], func() {})), "Missing Meeting Recording") {
		t.Fatal("saved locally bubble needs the recording reminder")
	}
	form := logwork.RowEditor(ui, rows[0], func() {}, func() {}).All()
	ui.Win.SetContent(form)
	assertBelow(form, multiLineEntry(form))
	entryByPlaceholder(form, "https://… (meeting recording)").SetText("https://example.com/recording")
	tapButton(t, form, "Save")
	rows, err = ui.Store.ReadRows()
	if err != nil || len(rows) != 1 || contains(labels(logwork.RowTile(ui, rows[0], func() {})), "Missing Meeting Recording") {
		t.Fatal("saving a recording must clear the saved bubble's reminder", err)
	}
}

func TestMeetingRecordingUsesFullDescriptionAndSeparateRemarks(t *testing.T) {
	r := backend.Row{"id": "meeting1", "meeting_title": "PC Image discussion", "remarks": "- brief worklog summary", "issue_body": strings.Repeat("Discussion notes\n", 90), "meeting_recording": "https://drive.google.com/file/d/recording/view"}
	body, err := backend.MeetingEntryBody(r)
	if err != nil || !strings.Contains(body, r["issue_body"]) || !strings.Contains(body, "Meeting Recording Link: "+r["meeting_recording"]) || r["remarks"] != "- brief worklog summary" {
		t.Fatalf("body lost meeting content: %q %v", body, err)
	}
	for _, link := range []string{"not a link", "file:///recording", "javascript:alert(1)", "https://example.com\nextra"} {
		r["meeting_recording"] = link
		if _, err := backend.MeetingEntryBody(r); err == nil {
			t.Errorf("invalid recording accepted: %q", link)
		}
	}
}

func TestCustomMeetingCreationAndRetryKeepChosenTitle(t *testing.T) {
	old := backend.Gh
	defer func() { backend.Gh = old }()
	r := backend.Row{"id": "meeting1", "date": "2026-11-19", "type": backend.KindMeeting, "meeting_title": "PC Image discussion", "remarks": "Discuss issue allocation", "meeting_recording": "https://example.com/recording"}
	cfg := backend.Config{MeetingRepo: "exampleorg/meetings", WorklogOwner: "team-alex"}
	stopped := errors.New("stop before real creation")
	backend.Gh = func(args []string, _ ...string) (string, error) {
		if len(args) > 1 && args[0] == "issue" && args[1] == "list" {
			return "[]", nil
		}
		if len(args) > 1 && args[0] == "issue" && args[1] == "create" {
			var title, body string
			for i, arg := range args {
				if arg == "--title" {
					title = args[i+1]
				}
				if arg == "--body" {
					body = args[i+1]
				}
			}
			if title != r["meeting_title"] || !strings.Contains(body, "Meeting Recording Link: https://example.com/recording") {
				t.Fatalf("chosen meeting title/body lost: %v", args)
			}
			return "", stopped
		}
		t.Fatalf("unexpected call: %v", args)
		return "", stopped
	}
	if _, err := backend.EnsureIssueRef(cfg, r); !errors.Is(err, stopped) {
		t.Fatal(err)
	}
	backend.Gh = func(args []string, _ ...string) (string, error) {
		if len(args) > 1 && args[0] == "issue" && args[1] == "list" {
			return `[{"number":42,"body":"<!-- ez-worklog-meeting:meeting1 -->"}]`, nil
		}
		t.Fatalf("retry created another issue: %v", args)
		return "", stopped
	}
	patch, err := backend.EnsureIssueRef(cfg, r)
	if err != nil || patch["issue"] != "exampleorg/meetings#42" {
		t.Fatalf("retry did not reuse created meeting: %v %v", patch, err)
	}
}

func TestMeetingFormSavesAndReopensCustomTitleAndRecording(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	var tabs *shell.UnderlineTabs
	walk(ui.Win.Content(), func(o fyne.CanvasObject) {
		if x, ok := o.(*shell.UnderlineTabs); ok {
			tabs = x
		}
	})
	if tabs == nil {
		t.Fatal("missing logwork kinds")
	}
	tabs.SetSelected("Meeting")
	form := ui.Win.Content()
	entryByPlaceholder(form, "Meeting title (e.g. Sprint planning)").SetText("PC Image discussion")
	entryByPlaceholder(form, "https://… (meeting recording)").SetText("https://example.com/recording")
	entryByPlaceholder(form, "minutes").SetText("90")
	entryByPlaceholder(form, "- one bullet per thing done; this becomes the worklog remarks").SetText("- Discuss issue allocation")
	tapButton(t, form, "Save draft")
	rows, err := ui.Store.ReadRows()
	if err != nil || len(rows) != 1 {
		t.Fatalf("meeting draft missing: %v %v", rows, err)
	}
	r := rows[0]
	if r["meeting_title"] != "PC Image discussion" || r["meeting_recording"] != "https://example.com/recording" || r["issue"] != "" || logwork.RowTitle(ui, r) != "PC Image discussion" {
		t.Fatalf("meeting fields lost: %v", r)
	}
	editor := logwork.RowEditor(ui, r, func() {}, func() {}).All()
	title := entryByPlaceholder(editor, "Meeting title (e.g. Sprint planning)")
	link := entryByPlaceholder(editor, "https://… (meeting recording)")
	if title == nil || link == nil || title.Text != "PC Image discussion" || link.Text != "https://example.com/recording" {
		t.Fatal("meeting did not reopen with editable fields")
	}
	title.SetText("Updated meeting title")
	link.SetText("https://example.com/updated")
	tapButton(t, editor, "Save")
	rows, _ = ui.Store.ReadRows()
	if rows[0]["meeting_title"] != "Updated meeting title" || rows[0]["meeting_recording"] != "https://example.com/updated" {
		t.Fatal("meeting edits were not persisted")
	}
}

func TestWorklogsAlwaysUseSubIssuesAndKeepDailyIssueKinds(t *testing.T) {
	for _, kind := range []string{backend.KindCommit, backend.KindOther, backend.KindCodeReview, backend.KindIndependent} {
		if got := backend.RowPushMode(backend.Row{"type": kind, "mode": "issue"}); got != "subissue" {
			t.Fatalf("%s mode = %s", kind, got)
		}
	}
	for _, kind := range []string{backend.KindMeeting, backend.KindBulkReview} {
		if got := backend.RowPushMode(backend.Row{"type": kind}); got != "issue" {
			t.Fatalf("%s mode = %s", kind, got)
		}
	}
	if got := backend.RowPushMode(backend.Row{"type": backend.KindOther, "mode": "issue", "pushed_at": "yesterday"}); got != "issue" {
		t.Fatal("old pushed direct entry lost its original target")
	}
}
