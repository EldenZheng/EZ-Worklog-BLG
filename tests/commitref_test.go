package tests

import (
	"net/url"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// A commit is not in the repo its issue is in — the issue lives in a tracker
// like Example-Support/sample-finance and the commit is in whichever applet
// changed — so the sha has to carry its own repo or the link can only guess.
func TestCommitRefsCarryTheirOwnRepo(t *testing.T) {
	for _, c := range []struct {
		repo, sha, want string
	}{
		{"exampleorg/sample-document-applet", "a8d4121",
			"exampleorg/sample-document-applet@a8d4121"},
		{"", "a8d4121", "a8d4121"}, // nothing to say where it is
		{"  o/r  ", "  abc123  ", "o/r@abc123"},
	} {
		if got := backend.FormatCommitRef(c.repo, c.sha); got != c.want {
			t.Fatalf("formatCommitRef(%q, %q) = %q, want %q", c.repo, c.sha, got, c.want)
		}
	}
}

type savedLinkApp struct {
	fyne.App
	opened string
}

func (a *savedLinkApp) OpenURL(u *url.URL) error {
	a.opened = u.String()
	return nil
}

func TestSavedBubblesLinkTheIssueAndItsCommits(t *testing.T) {
	ui := editorUI(t)
	base := fyne.CurrentApp()
	app := &savedLinkApp{App: base}
	fyne.SetCurrentApp(app)
	defer func() { fyne.SetCurrentApp(base); base.Quit() }()

	const sha = "a8d412131d9acf87b6c2dc0989ed6a1f39e15179"
	row := backend.Row{
		"type": backend.KindCommit, "issue": "tracker/tasks#48",
		"refs": "dev/applet@" + sha + ",dev/service@bbbc222,ccc3333",
	}
	for _, bubble := range []fyne.CanvasObject{
		logwork.RowTile(ui, row, func() {}), logwork.DayRowCard(ui, row, func() {}),
	} {
		links := map[string]string{}
		walk(bubble, func(o fyne.CanvasObject) {
			switch l := o.(type) {
			case *shell.HoverLink:
				links[l.Text] = l.URL.String()
			case *shell.HoverTag:
				if l.Text == "tasks#48" {
					t.Error("the issue should be a plain link, not a bubble")
				}
			}
		})
		for text, url := range map[string]string{
			"tasks#48": "https://github.com/tracker/tasks/issues/48",
			"a8d4121":  "https://github.com/dev/applet/commit/" + sha,
			"bbbc222":  "https://github.com/dev/service/commit/bbbc222",
		} {
			if links[text] != url {
				t.Errorf("%q links to %q, want %q", text, links[text], url)
			}
		}
		if !contains(labels(bubble), "ccc3333") || links["ccc3333"] != "" {
			t.Error("a legacy SHA with no recorded repo should be plain text")
		}
	}
}

func TestClickBesideSavedIssueOpensEditor(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	for _, pushed := range []string{"", "2026-11-21T08:30:00"} {
		row := backend.Row{"date": "2026-11-21", "issue": "tracker/tasks#48", "pushed_at": pushed}
		card := logwork.RowTile(ui, row, func() {})
		ui.Win.SetContent(card)
		ui.Win.Resize(fyne.NewSize(420, 220))
		var issue *shell.HoverLink
		walk(card, func(o fyne.CanvasObject) {
			if link, ok := o.(*shell.HoverLink); ok && link.Text == "tasks#48" {
				issue = link
			}
		})
		if issue == nil {
			t.Fatal("missing issue block")
		}
		p := fyne.CurrentApp().Driver().AbsolutePositionForObject(issue)
		middle := fyne.NewPos(card.Size().Width/2, p.Y+issue.Size().Height/2)
		if p.X+issue.Size().Width >= middle.X {
			t.Fatalf("issue block covers the middle of the card: %v", issue.Size())
		}
		test.TapCanvas(ui.Win.Canvas(), middle)
		overlay := ui.Win.Canvas().Overlays().Top()
		if overlay == nil {
			t.Fatalf("click beside issue did not open the editor (pushed_at=%q)", pushed)
		}
		ui.Win.Canvas().Overlays().Remove(overlay)
	}
}

// Rows written before the repo was stored are still on disk, and they still
// have to read back — as a sha with no repo, so the caller can fall back the way
// the editor always did rather than dropping the commit.
func TestParseCommitRefsReadsBothSpellings(t *testing.T) {
	got := backend.ParseCommitRefs("o/r@aaa1111, bbb2222 ,exampleorg/sample-intranet@ccc3333,, ")
	want := []backend.CommitRef{
		{Repo: "o/r", Sha: "aaa1111"},
		{Sha: "bbb2222"},
		{Repo: "exampleorg/sample-intranet", Sha: "ccc3333"},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d refs, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ref %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(backend.ParseCommitRefs("")) != 0 {
		t.Fatal("an empty refs column is no commits, not one blank one")
	}
}

// Whatever the spelling, what is matched against GitHub is the sha alone: a
// commit already logged must not be offered again because its entry now records
// the repo too.
func TestLoggedShasIgnoresTheRepoHalf(t *testing.T) {
	s := backend.NewStore(tempDir(t))
	if _, err := s.AppendRows([]backend.Row{
		{"date": "2026-08-31", "minutes": "300", "refs": "exampleorg/applet@aaa1111,bbb2222"},
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
	if got["exampleorg/applet@aaa1111"] {
		t.Fatal("the stored spelling is not a sha; only the sha half is matched")
	}
}

// The editor's sha links point at the commit, not at the issue's repo — that
// mismatch is what made every one of them 404.
func TestRowEditorLinksTheShaToItsOwnRepo(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	form := logwork.RowEditor(ui, backend.Row{
		"id": "1", "date": "2026-08-31", "minutes": "300",
		"issue": "Example-Support/sample-finance#48",
		"refs":  "exampleorg/sample-document-applet@a8d4121",
	}, func() {}, func() {})

	var links []*widget.Hyperlink
	walk(form.Body, func(o fyne.CanvasObject) {
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
		t.Fatalf("the sha should be a link: %v", labels(form.Body))
	}
	want := "https://github.com/exampleorg/sample-document-applet/commit/a8d4121"
	if got := sha.URL.String(); got != want {
		t.Fatalf("sha links to %q, want %q", got, want)
	}
	if strings.Contains(sha.URL.String(), "sample-finance") {
		t.Fatal("the issue's repo is not the commit's repo")
	}

	// An old row with a bare sha keeps the only guess there is, rather than
	// losing its link altogether.
	old := logwork.RowEditor(ui, backend.Row{
		"id": "2", "date": "2026-08-31", "minutes": "300",
		"issue": "Example-Support/sample-finance#48", "refs": "a8d4121",
	}, func() {}, func() {})
	var fallback *widget.Hyperlink
	walk(old.Body, func(o fyne.CanvasObject) {
		if h, ok := o.(*widget.Hyperlink); ok && h.Text == "a8d4121" {
			fallback = h
		}
	})
	if fallback == nil {
		t.Fatal("a bare sha should still link somewhere")
	}
	if !strings.Contains(fallback.URL.String(), "sample-finance/commit/a8d4121") {
		t.Fatalf("a repo-less sha falls back to the issue's repo, got %q", fallback.URL)
	}
}
