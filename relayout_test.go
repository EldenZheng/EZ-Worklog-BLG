package main

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
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
	ui := &UI{store: newStore(t.TempDir()), win: w,
		calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	w.SetContent(ui.buildLogTab())
	w.Resize(fyne.NewSize(1200, 900))

	ui.pendingLoaded = true
	ui.pending = PendingResult{Range: []string{"2026-08-20", "2026-08-22"}}
	ui.renderPending()
	empty := ui.pendingBox.Size().Height

	var groups []Group
	for i := 0; i < 4; i++ {
		groups = append(groups, Group{Date: "2026-08-21",
			Issue: "bigledger/blg-intranet#42",
			Commits: []Commit{{Sha: fmt.Sprintf("aaa111%d", i),
				Repo: "bigledger/blg-intranet", Message: "work", Full: "work"}}})
	}
	ui.pending = PendingResult{Count: 4, Range: []string{"2026-08-20", "2026-08-22"},
		Groups: groups}
	ui.renderPending()

	grown := ui.pendingBox.Size().Height
	if want := ui.pendingBox.MinSize().Height; grown < want {
		t.Fatalf("the list needs %g of height and was given %g (was %g when empty)",
			want, grown, empty)
	}
	if grown <= empty {
		t.Fatalf("four tiles should take more room than one line: %g then %g", empty, grown)
	}

	// And back down again when the work is logged, so an empty list does not
	// leave a hole where the tiles used to be.
	ui.pending = PendingResult{Range: []string{"2026-08-20", "2026-08-22"}}
	ui.renderPending()
	if shrunk := ui.pendingBox.Size().Height; shrunk >= grown {
		t.Fatalf("an emptied list should give the room back: %g then %g", grown, shrunk)
	}
}
