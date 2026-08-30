package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"image/color"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

// Colours follow configuration order: the first org is blue, the second red.
// The mapping must not shift when a day happens to hold only one of them.
func TestOrgColoursFollowConfigOrder(t *testing.T) {
	cfg := Config{
		Repos:            []string{"bigledger", "BigLedger-Support"},
		ProjectURL:       "https://github.com/orgs/bigledger/projects/9",
		ExtraProjectURLs: []string{"https://github.com/orgs/BigLedger-Support/projects/1"},
	}
	if got := orgColor(cfg, "bigledger"); got != orgPalette[0] {
		t.Fatalf("first org should take the first colour, got %v", got)
	}
	// Case is not part of the identity: refs say "BigLedger-Support", the
	// config may say "bigledger-support".
	if got := orgColor(cfg, "bigledger-support"); got != orgPalette[1] {
		t.Fatalf("second org should take the second colour, got %v", got)
	}
	if got := orgColor(cfg, "someone-else"); got != orgUnknown {
		t.Fatalf("an unconfigured org must not borrow a palette colour, got %v", got)
	}

	// A board-only org still gets a colour, after the ones typed in Repos.
	boardOnly := Config{
		Repos:            []string{"bigledger"},
		ProjectURL:       "https://github.com/orgs/bigledger/projects/9",
		ExtraProjectURLs: []string{"https://github.com/orgs/BigLedger-Support/projects/1"},
	}
	if got := orgColor(boardOnly, "BigLedger-Support"); got != orgPalette[1] {
		t.Fatalf("board-only org should still be coloured, got %v", got)
	}
}

func TestOrgOf(t *testing.T) {
	cases := map[string]string{
		"https://github.com/bigledger/blg-intranet/issues/42":  "bigledger",
		"https://github.com/orgs/BigLedger-Support/projects/1": "BigLedger-Support",
		"bigledger/blg-int-general-task#6511":                  "bigledger",
		"bigledger/repo":                                       "bigledger",
		"":                                                     "",
	}
	for in, want := range cases {
		if got := orgOf(in); got != want {
			t.Fatalf("orgOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// A day's minutes split by org, so the calendar cell can colour its bar.
func TestMinutesByOrgSplitsTheDay(t *testing.T) {
	items := []WorklogItem{
		{URL: "https://github.com/bigledger/repo/issues/1", Minutes: 300},
		{URL: "https://github.com/BigLedger-Support/help/issues/9", Minutes: 180},
		{URL: "https://github.com/bigledger/other/issues/2", Minutes: 60},
		// No URL of its own: fall back to the parent it hangs off.
		{ParentURL: "https://github.com/bigledger/repo/issues/7", Minutes: 30},
	}
	got := minutesByOrg(items)
	if got["bigledger"] != 390 {
		t.Fatalf("bigledger minutes = %d, want 390", got["bigledger"])
	}
	if got["bigledger-support"] != 180 {
		t.Fatalf("support minutes = %d, want 180", got["bigledger-support"])
	}

	cfg := Config{Repos: []string{"bigledger", "BigLedger-Support"}}
	if order := orgsByShare(cfg, got); order[0] != "bigledger" || order[1] != "bigledger-support" {
		t.Fatalf("configured order should hold, got %v", order)
	}
}

// The meter is a picture of the day: segments in org colours over a track, and
// never wider than the track even when the day ran past target.
func TestMeterBarStaysInsideItsTrack(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	cfg := Config{Repos: []string{"bigledger", "BigLedger-Support"}}
	byOrg := map[string]int{"bigledger": 300, "bigledger-support": 180}

	bar := meterBar(cfg, byOrg, target, 8)
	bar.Resize(fyne.NewSize(480, 8))
	filled := meterFilled(bar)
	if filled < 470 || filled > 481 {
		t.Fatalf("480 of 480 minutes should fill the bar, filled %g of 480", filled)
	}

	half := meterBar(cfg, map[string]int{"bigledger": 240}, target, 8)
	half.Resize(fyne.NewSize(480, 8))
	if f := meterFilled(half); f < 230 || f > 250 {
		t.Fatalf("half a day should fill half the bar, filled %g of 480", f)
	}

	over := meterBar(cfg, map[string]int{"bigledger": 600, "bigledger-support": 600}, target, 8)
	over.Resize(fyne.NewSize(480, 8))
	if f := meterFilled(over); f > 481 {
		t.Fatalf("an over-target day must not paint past its track, filled %g", f)
	}
}

// meterFilled totals the width of the coloured segments in a meter, ignoring
// the track they sit on and the transparent remainder beside them.
func meterFilled(o fyne.CanvasObject) float32 {
	isOrgColour := func(c color.Color) bool {
		for _, p := range append(append([]color.NRGBA{}, orgPalette...), orgUnknown) {
			if c == color.Color(p) {
				return true
			}
		}
		return false
	}
	total := float32(0)
	walk(o, func(c fyne.CanvasObject) {
		if r, ok := c.(*canvas.Rectangle); ok && isOrgColour(r.FillColor) {
			total += r.Size().Width
		}
	})
	return total
}

// The calendar cell fills from the bottom: the first org is the base of the
// stack and the empty part at the top is the day still owed.
func TestVMeterFillsFromTheBottom(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	cfg := Config{Repos: []string{"bigledger", "BigLedger-Support"}}
	fill := vMeterFill(cfg, map[string]int{"bigledger": 240, "bigledger-support": 120}, 0, target, cellCornerRadius)
	fill.Resize(fyne.NewSize(80, 100)) // 100px tall cell, 480 target

	segs := []*canvas.Rectangle{}
	walk(fill, func(c fyne.CanvasObject) {
		if r, ok := c.(*canvas.Rectangle); ok {
			segs = append(segs, r)
		}
	})
	if len(segs) != 2 {
		t.Fatalf("two orgs should draw two segments, got %d", len(segs))
	}
	// 240/480 of 100px, plus the overlap it reaches up behind the segment
	// above so the rounded seam between them reads solid.
	if h := segs[0].Size().Height; h < 49 || h > 51+cellCornerRadius {
		t.Fatalf("half a day should fill half the cell, got %g of 100", h)
	}
	if segs[0].CornerRadius != cellCornerRadius {
		t.Fatalf("the fill must be rounded like the cell, radius %g", segs[0].CornerRadius)
	}
	if segs[0].Position().Y <= segs[1].Position().Y {
		t.Fatalf("the first org must be the base of the stack: %v then %v",
			segs[0].Position(), segs[1].Position())
	}
	bottom := segs[0].Position().Y + segs[0].Size().Height
	if bottom < 99 || bottom > 101 {
		t.Fatalf("the fill should sit on the floor of the cell, ends at %g", bottom)
	}
	// Washed out towards the background so the day's number reads on top of
	// it, but opaque: the lower segment reaches up behind the one above, and
	// two half-transparent colours blended there into a band of a third colour
	// — a purple stripe where blue met red.
	base := segs[0].FillColor.(color.NRGBA)
	if base.A != 0xff {
		t.Fatalf("a see-through segment blends with the one it overlaps: %v", base)
	}
	if base == orgColor(cfg, "bigledger") {
		t.Fatal("the fill must be paler than the org's own colour, or the day number is lost in it")
	}
	// The seam is where the blending showed, so both sides of it are checked.
	if top := segs[1].FillColor.(color.NRGBA); top.A != 0xff || top == base {
		t.Fatalf("the second org's segment should be opaque and its own colour: %v", top)
	}
	if overlap := segs[1].Position().Y + segs[1].Size().Height - segs[0].Position().Y; overlap <= 0 {
		t.Fatalf("the segments should still overlap so the seam reads solid, gap of %g", -overlap)
	}

	// A day past target fills the cell and no further.
	over := vMeterFill(cfg, map[string]int{"bigledger": 900}, 0, target, cellCornerRadius)
	over.Resize(fyne.NewSize(80, 100))
	tall := float32(0)
	walk(over, func(c fyne.CanvasObject) {
		if r, ok := c.(*canvas.Rectangle); ok {
			tall += r.Size().Height
		}
	})
	// One org means one segment, so no overlap is added to it.
	if tall > 101 {
		t.Fatalf("an over-target day must not paint past the cell, filled %g of 100", tall)
	}
}
