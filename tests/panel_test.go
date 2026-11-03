package tests

import (
	"image/color"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
)

// A folding heading starts as asked and slides its body open and shut on every
// tap of the heading itself, reporting the new state. (The test driver runs
// an animation straight to its end.)
func TestCollapseToggleFoldsItsBody(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	body := widget.NewLabel("body")
	var states []bool
	relayouts := 0
	head, area := shell.CollapseToggle(widget.NewLabel("Saved locally"), body, false,
		func(open bool) { states = append(states, open) }, func(bool) { relayouts++ })
	fold, ok := area.(*shell.Fold)
	if !ok {
		t.Fatalf("area is %T, want *shell.Fold", area)
	}
	if fold.Visible() || fold.MinSize().Height != 0 {
		t.Fatal("body should start folded away")
	}
	tap, ok := head.(*fyne.Container).Objects[0].(*shell.Tappable)
	if !ok {
		t.Fatalf("heading is not tappable: %T", head.(*fyne.Container).Objects[0])
	}
	test.Tap(tap)
	if !fold.Visible() || fold.MinSize().Height != body.MinSize().Height {
		t.Fatal("first tap should slide the body fully open")
	}
	test.Tap(tap)
	if fold.Visible() {
		t.Fatal("second tap should fold the body away again")
	}
	if len(states) != 2 || !states[0] || states[1] {
		t.Fatalf("onToggle states = %v, want [true false]", states)
	}
	if relayouts == 0 {
		t.Fatal("the page should be asked to relayout as the fold moves")
	}
}

// When a section above shuts and the page gets shorter, the view rides up
// with it rather than being left over blank space below the end.
func TestReflowKeepsTheViewInsideAShrunkPage(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	tall := canvas.NewRectangle(color.Transparent)
	tall.SetMinSize(fyne.NewSize(100, 1000))
	page := container.NewVBox(tall)
	scroll := container.NewVScroll(page)
	w := a.NewWindow("t")
	w.SetContent(scroll)
	w.Resize(fyne.NewSize(200, 300))
	scroll.ScrollToBottom()
	if scroll.Offset.Y == 0 {
		t.Fatal("setup: the page should start scrolled down")
	}
	tall.SetMinSize(fyne.NewSize(100, 400))
	shell.Reflow(false, scroll, page)
	if max := page.Size().Height - scroll.Size().Height; scroll.Offset.Y > max {
		t.Fatalf("view left at %g past the end of a page that now stops at %g", scroll.Offset.Y, max)
	}
}

// Switching what a Morph shows ends at the new content's own height, with
// the content back in place.
func TestMorphSettlesOnTheNewHeight(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	short := widget.NewLabel("short")
	long := container.NewVBox(widget.NewLabel("one"), widget.NewLabel("two"), widget.NewLabel("three"))
	long.Hide()
	m := shell.NewMorph(container.NewVBox(short, long))
	w := a.NewWindow("t")
	w.SetContent(m)
	before := m.MinSize().Height
	short.Hide()
	long.Show()
	finals := 0
	m.Change(1, func(final bool) {
		if final {
			finals++
		}
	})
	if got := m.MinSize().Height; got <= before || got != long.MinSize().Height {
		t.Fatalf("morph height = %g, want the new content's %g", got, long.MinSize().Height)
	}
	if finals != 1 {
		t.Fatalf("the page should get one final relayout, got %d", finals)
	}
}

// A page built with NewPageScroll keeps the view on the page when a refresh
// leaves it shorter, instead of over blank space past the new end.
func TestRelayoutRidesTheViewUpAShrunkPage(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	tall := canvas.NewRectangle(color.Transparent)
	tall.SetMinSize(fyne.NewSize(100, 1000))
	page := container.NewVBox(tall)
	scroll := shell.NewPageScroll(page, page, page)
	w := a.NewWindow("t")
	w.SetContent(scroll)
	w.Resize(fyne.NewSize(200, 300))
	scroll.ScrollToBottom()
	tall.SetMinSize(fyne.NewSize(100, 400))
	shell.Relayout(page)
	if end := page.MinSize().Height - scroll.Size().Height; scroll.Offset.Y > end+0.5 {
		t.Fatalf("view left at %g past the page's new end at %g", scroll.Offset.Y, end)
	}
}

// The gold tag hugs its text, never runs past the width it is given, and
// says so with an ellipsis.
func TestGoldTagHugsItsTextAndElides(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	tag := shell.NewGoldTag("A parent issue with a long title that will not fit", func() {})
	row := shell.HugTag(tag)
	w := a.NewWindow("t")
	w.SetContent(row)
	row.Resize(fyne.NewSize(120, 30))
	if tag.Size().Width > 120 {
		t.Fatalf("tag is %g wide in a 120 row", tag.Size().Width)
	}
	if shown := tag.Shown(); shown == tag.Text || !strings.HasSuffix(shown, "…") {
		t.Fatalf("a long title should end in an ellipsis, shows %q", shown)
	}
	row.Resize(fyne.NewSize(1000, 30))
	if tag.Size().Width >= 1000 || tag.Shown() != tag.Text {
		t.Fatalf("with room the tag should hug its whole text, is %g wide showing %q", tag.Size().Width, tag.Shown())
	}
}

// The pointer moving from a bubble onto a link or tag inside it still counts
// as hovering the bubble; leaving the tag for outside the bubble does not.
func TestBubbleStaysLitOverATagInsideIt(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	shell.ReleaseBubble() // drag tests can leave a card held
	tag := shell.NewGoldTag("Parent issue", func() {})
	area := shell.HoverBubble(color.White, container.NewVBox(widget.NewLabel("card"), shell.HugTag(tag)))
	bubble := shell.BubbleIn(area).(*shell.StripeBubble)

	area.MouseIn(nil)
	area.MouseOut() // Fyne says "out" as the pointer moves onto the tag
	tag.MouseIn(nil)
	if !bubble.Lit() {
		t.Fatal("the bubble should stay lit while the pointer is on its tag")
	}
	tag.MouseOut()
	if bubble.Lit() {
		t.Fatal("leaving the tag for outside should let the bubble go dark")
	}
}

// A column hears the pointer on the cards inside it as on itself, so its
// title stays lit as the pointer moves from the column onto a card.
func TestHoverRegionCountsTheCardsInside(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	shell.ReleaseBubble() // drag tests can leave a card held
	card := shell.HoverBubble(color.White, widget.NewLabel("card"))
	lit := false
	column := shell.NewHoverRegion(container.NewVBox(card), func(on bool) { lit = on })

	column.MouseIn(nil)
	column.MouseOut() // Fyne says "out" as the pointer moves onto the card
	card.MouseIn(nil)
	if !lit {
		t.Fatal("the column should stay lit while the pointer is on a card in it")
	}
	card.MouseOut()
	if lit {
		t.Fatal("leaving the card for outside the column should let it go dark")
	}
}
