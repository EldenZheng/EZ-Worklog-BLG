//go:build cursors

package tests

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// A pending commit tile, ignored or not, presses as a button does: it shrinks
// while the pointer is held down on it and springs back once let go, without
// moving the tiles around it.
func TestPendingTileShrinksUnderThePointerAndReturns(t *testing.T) {
	for _, ignored := range []bool{false, true} {
		a, driver := pressTestApp(t)
		w := a.NewWindow("pending tile press")
		ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w}
		g := backend.Group{Date: "2026-11-16", Issue: "exampleorg/shop#1", Ignored: ignored,
			Commits: []backend.Commit{{Sha: "abc1234", Message: "fix the till"}}}
		tile := logwork.GroupBubble(ui, g, false).(*shell.Tappable)
		w.SetContent(container.NewCenter(container.NewGridWrap(fyne.NewSize(340, 152), tile)))
		w.Resize(fyne.NewSize(500, 300))
		size, pos := tile.Size(), tile.Position()
		objects := test.WidgetRenderer(tile).Objects()
		shot := objects[len(objects)-1].(*canvas.Image)
		primary := &desktop.MouseEvent{Button: desktop.MouseButtonPrimary}

		// Another test's delayed card arrival can start on this driver too:
		// pick out the press, which shrinks for 0.15s.
		pressAnim := func(from int) *fyne.Animation {
			for _, a := range driver.anims[from:] {
				if a.Duration == 150*time.Millisecond || a.Duration == 450*time.Millisecond {
					return a
				}
			}
			t.Fatalf("ignored=%v: no press animation started", ignored)
			return nil
		}
		driver.anims = nil
		tile.MouseDown(primary)
		pressAnim(0).Tick(1)
		if tile.Content.Visible() || !shot.Visible() || shot.Size().Width >= size.Width || shot.Size().Height >= size.Height {
			t.Fatalf("ignored=%v: the held tile should be drawn shrunk: card shown %v, picture %v at %v",
				ignored, tile.Content.Visible(), shot.Visible(), shot.Size())
		}
		started := len(driver.anims)
		tile.MouseUp(primary)
		back := pressAnim(started)
		back.Tick(0.5)
		mid := shot.Size().Width
		back.Tick(1)
		if !tile.Content.Visible() || shot.Visible() || mid >= size.Width {
			t.Fatalf("ignored=%v: the tile should spring back and end at rest (mid-return width %v)", ignored, mid)
		}
		if tile.Size() != size || tile.Position() != pos {
			t.Fatalf("ignored=%v: the press moved the tile: %v at %v, was %v at %v", ignored, tile.Size(), tile.Position(), size, pos)
		}
		w.Close()
	}
}
