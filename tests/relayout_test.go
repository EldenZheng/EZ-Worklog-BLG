package tests

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// A refresh that finds work has to make room for it.
//
// Replacing a container's Objects lays that container out again inside the
// space it was already given; nothing tells the column above it that the space
// needed has changed. So the pending list would go from one line of "No
// unlogged commits" to a dozen tiles, keep the one line's worth of height, and
// draw the tiles into it — a Refresh that plainly found commits, showing
// nothing. Resizing the window was the only thing that fixed it.
func TestARefreshThatFindsCommitsMakesRoomForThem(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w,
		CalMonth: shell.ThisMonth(), RepMonth: shell.ThisMonth()}
	ui.Cfg = backend.Config{Repos: []string{"exampleorg"}}
	w.SetContent(logwork.BuildLogTab(ui))
	w.Resize(fyne.NewSize(1200, 900))

	ui.PendingLoaded = true
	ui.Pending = backend.PendingResult{Range: []string{"2026-08-20", "2026-08-22"}}
	logwork.RenderPending(ui)
	empty := ui.PendingBox.Size().Height

	var groups []backend.Group
	for i := 0; i < 4; i++ {
		groups = append(groups, backend.Group{Date: "2026-08-21",
			Issue: "exampleorg/sample-intranet#42",
			Commits: []backend.Commit{{Sha: fmt.Sprintf("aaa111%d", i),
				Repo: "exampleorg/sample-intranet", Message: "work", Full: "work"}}})
	}
	ui.Pending = backend.PendingResult{Count: 4, Range: []string{"2026-08-20", "2026-08-22"},
		Groups: groups}
	logwork.RenderPending(ui)

	grown := ui.PendingBox.Size().Height
	if want := ui.PendingBox.MinSize().Height; grown < want {
		t.Fatalf("the list needs %g of height and was given %g (was %g when empty)",
			want, grown, empty)
	}
	if grown <= empty {
		t.Fatalf("four tiles should take more room than one line: %g then %g", empty, grown)
	}

	// And back down again when the work is logged, so an empty list does not
	// leave a hole where the tiles used to be.
	ui.Pending = backend.PendingResult{Range: []string{"2026-08-20", "2026-08-22"}}
	logwork.RenderPending(ui)
	if shrunk := ui.PendingBox.Size().Height; shrunk >= grown {
		t.Fatalf("an emptied list should give the room back: %g then %g", grown, shrunk)
	}
}
