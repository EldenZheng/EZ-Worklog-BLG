package tests

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"worklog/internal/menus/logwork"
)

// Under the cap the box is one text: it is the remarks, the row stores no
// separate body, and the split is not offered.
func TestCommitTextStaysOneBoxUnderCap(t *testing.T) {
	test.NewApp()
	ct := logwork.CommitTextEditor("- short", "")
	if ct.Separate.Visible() {
		t.Fatal("split offered for text under the cap")
	}
	remarks, body := ct.Values()
	if remarks != "- short" || body != "" {
		t.Fatalf("values = %q, %q", remarks, body)
	}
	if why := ct.Check(); why != "" {
		t.Fatalf("check = %q", why)
	}
}

// Reaching the cap offers the split; ticking it keeps the long text as the
// issue body and copies it into remarks for the user to edit.
func TestCommitTextSplitsLongDescription(t *testing.T) {
	test.NewApp()
	long := strings.Repeat("- a line of real commit work here\n", 60)
	ct := logwork.CommitTextEditor(long, "")
	if !ct.Separate.Visible() {
		t.Fatal("split not offered at the cap")
	}
	if why := ct.Check(); why == "" {
		t.Fatal("over-cap remarks passed check unsplit")
	}
	ct.Separate.SetChecked(true)
	remarks, body := ct.Values()
	if body != strings.TrimSpace(long) {
		t.Fatal("description not kept whole as the issue body")
	}
	if remarks != body {
		t.Fatal("the remarks should start with the complete description")
	}
	if why := ct.Check(); why == "" {
		t.Fatal("over-cap remarks passed check split")
	}
	ct.Remarks.SetText("- a short summary")
	if why := ct.Check(); why != "" {
		t.Fatalf("check after editing remarks = %q", why)
	}
}

// A row saved split reopens split, with each text in its own box.
func TestCommitTextReopensSplit(t *testing.T) {
	test.NewApp()
	ct := logwork.CommitTextEditor("- brief", "the full description")
	if !ct.Split() {
		t.Fatal("saved split row reopened as one box")
	}
	remarks, body := ct.Values()
	if remarks != "- brief" || body != "the full description" {
		t.Fatalf("values = %q, %q", remarks, body)
	}
}
