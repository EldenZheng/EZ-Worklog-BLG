package tests

import (
	"strings"
	"testing"
	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// Under the cap the box is one text: it is the remarks, the row stores no
// separate body, and the split is not offered.
func TestCommitTextStaysOneBoxUnderCap(t *testing.T) {
	test.NewApp()
	ct := logwork.CommitTextEditor((&shell.UI{}), "- short", "", widget.NewLabel(""))
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
// issue body, seeds remarks that fit, and lets the entry save.
func TestCommitTextSplitsLongDescription(t *testing.T) {
	test.NewApp()
	long := strings.Repeat("- a line of real commit work here\n", 60)
	ct := logwork.CommitTextEditor((&shell.UI{}), long, "", widget.NewLabel(""))
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
	if remarks == "" || len(remarks) > backend.RemarkCap {
		t.Fatalf("seeded remarks are %d chars", len(remarks))
	}
	if why := ct.Check(); why != "" {
		t.Fatalf("check = %q", why)
	}
	ct.Remarks.SetText(strings.Repeat("x", backend.RemarkCap+1))
	if why := ct.Check(); why == "" {
		t.Fatal("over-cap remarks passed check split")
	}
}

// A row saved split reopens split, with each text in its own box.
func TestCommitTextReopensSplit(t *testing.T) {
	test.NewApp()
	ct := logwork.CommitTextEditor((&shell.UI{}), "- brief", "the full description", widget.NewLabel(""))
	if !ct.Split() {
		t.Fatal("saved split row reopened as one box")
	}
	remarks, body := ct.Values()
	if remarks != "- brief" || body != "the full description" {
		t.Fatalf("values = %q, %q", remarks, body)
	}
}
