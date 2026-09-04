package main

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// A commit is not in the repo its issue is in — the issue lives in a tracker
// like BigLedger-Support/tuhu-finance and the commit is in whichever applet
// changed — so the sha has to carry its own repo or the link can only guess.
func TestCommitRefsCarryTheirOwnRepo(t *testing.T) {
	for _, c := range []struct {
		repo, sha, want string
	}{
		{"bigledger/blg-applet-wavelet-doc-item-maintenance-applet", "a8d4121",
			"bigledger/blg-applet-wavelet-doc-item-maintenance-applet@a8d4121"},
		{"", "a8d4121", "a8d4121"}, // nothing to say where it is
		{"  o/r  ", "  abc123  ", "o/r@abc123"},
	} {
		if got := formatCommitRef(c.repo, c.sha); got != c.want {
			t.Fatalf("formatCommitRef(%q, %q) = %q, want %q", c.repo, c.sha, got, c.want)
		}
	}
}

// Rows written before the repo was stored are still on disk, and they still
// have to read back — as a sha with no repo, so the caller can fall back the way
// the editor always did rather than dropping the commit.
func TestParseCommitRefsReadsBothSpellings(t *testing.T) {
	got := parseCommitRefs("o/r@aaa1111, bbb2222 ,bigledger/blg-intranet@ccc3333,, ")
	want := []commitRef{
		{Repo: "o/r", Sha: "aaa1111"},
		{Sha: "bbb2222"},
		{Repo: "bigledger/blg-intranet", Sha: "ccc3333"},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d refs, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ref %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(parseCommitRefs("")) != 0 {
		t.Fatal("an empty refs column is no commits, not one blank one")
	}
}

// Whatever the spelling, what is matched against GitHub is the sha alone: a
// commit already logged must not be offered again because its entry now records
// the repo too.
func TestLoggedShasIgnoresTheRepoHalf(t *testing.T) {
	s := newStore(t.TempDir())
	if _, err := s.AppendRows([]Row{
		{"date": "2026-08-31", "minutes": "300", "refs": "bigledger/applet@aaa1111,bbb2222"},
		{"date": "2026-08-30", "minutes": "60", "refs": "ccc3333"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoggedShas()
	if err != nil {
		t.Fatal(err)
	}
	for _, sha := range []string{"aaa1111", "bbb2222", "ccc3333"} {
		if !got[sha] {
			t.Fatalf("%s is logged and should not be offered again: %v", sha, got)
		}
	}
	if got["bigledger/applet@aaa1111"] {
		t.Fatal("the stored spelling is not a sha; only the sha half is matched")
	}
}

// The editor's sha links point at the commit, not at the issue's repo — that
// mismatch is what made every one of them 404.
func TestRowEditorLinksTheShaToItsOwnRepo(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	form := ui.rowEditor(Row{
		"id": "1", "date": "2026-08-31", "minutes": "300",
		"issue": "BigLedger-Support/tuhu-finance#48",
		"refs":  "bigledger/blg-applet-wavelet-doc-item-maintenance-applet@a8d4121",
	}, func() {}, func() {})

	var links []*widget.Hyperlink
	walk(form.body, func(o fyne.CanvasObject) {
		if h, ok := o.(*widget.Hyperlink); ok {
			links = append(links, h)
		}
	})
	var sha *widget.Hyperlink
	for _, l := range links {
		if l.Text == "a8d4121" {
			sha = l
		}
	}
	if sha == nil {
		t.Fatalf("the sha should be a link: %v", labels(form.body))
	}
	want := "https://github.com/bigledger/blg-applet-wavelet-doc-item-maintenance-applet/commit/a8d4121"
	if got := sha.URL.String(); got != want {
		t.Fatalf("sha links to %q, want %q", got, want)
	}
	if strings.Contains(sha.URL.String(), "tuhu-finance") {
		t.Fatal("the issue's repo is not the commit's repo")
	}

	// An old row with a bare sha keeps the only guess there is, rather than
	// losing its link altogether.
	old := ui.rowEditor(Row{
		"id": "2", "date": "2026-08-31", "minutes": "300",
		"issue": "BigLedger-Support/tuhu-finance#48", "refs": "a8d4121",
	}, func() {}, func() {})
	var fallback *widget.Hyperlink
	walk(old.body, func(o fyne.CanvasObject) {
		if h, ok := o.(*widget.Hyperlink); ok && h.Text == "a8d4121" {
			fallback = h
		}
	})
	if fallback == nil {
		t.Fatal("a bare sha should still link somewhere")
	}
	if !strings.Contains(fallback.URL.String(), "tuhu-finance/commit/a8d4121") {
		t.Fatalf("a repo-less sha falls back to the issue's repo, got %q", fallback.URL)
	}
}
