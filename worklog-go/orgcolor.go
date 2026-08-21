package main

import (
	"fmt"
	"image/color"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// orgPalette hands each organisation a colour in configuration order: the first
// org configured is blue, the second red, and so on. Work from two orgs looks
// alike on a board and in a list, so the colour is the only thing that says at
// a glance which employer a day's minutes belong to.
var orgPalette = []color.NRGBA{
	{R: 0x3B, G: 0x7D, B: 0xD8, A: 0xff}, // blue
	{R: 0xD1, G: 0x3B, B: 0x3B, A: 0xff}, // red
	{R: 0x2E, G: 0x8B, B: 0x57, A: 0xff}, // green
	{R: 0x8A, G: 0x4F, B: 0xC4, A: 0xff}, // purple
	{R: 0xC9, G: 0x7A, B: 0x14, A: 0xff}, // amber
	{R: 0x1F, G: 0x8A, B: 0x8A, A: 0xff}, // teal
}

// orgUnknown is for work whose org is not configured — a repo scanned by an
// explicit owner/repo entry that was later removed, say. Grey reads as "not one
// of the ones you told me about" rather than as a seventh employer.
var orgUnknown = color.NRGBA{R: 0x8A, G: 0x8A, B: 0x8A, A: 0xff}

// orgOrder lists the configured organisations, lowercased, in the order they
// take colours. Repos comes first because that is the list the user types in
// the order they think of them; a board-only org is appended after.
func orgOrder(cfg Config) []string {
	var out []string
	seen := map[string]bool{}
	add := func(o string) {
		o = strings.ToLower(strings.TrimSpace(o))
		if o == "" || seen[o] {
			return
		}
		seen[o] = true
		out = append(out, o)
	}
	for _, entry := range cfg.Repos {
		entry = strings.TrimSpace(entry)
		if i := strings.IndexByte(entry, '/'); i >= 0 {
			add(entry[:i]) // explicit owner/repo — the owner is the org
			continue
		}
		add(entry)
	}
	for _, u := range projectURLs(cfg) {
		if ref, err := parseProjectRef(u); err == nil {
			add(ref.login)
		}
	}
	return out
}

// orgColor is the colour for an organisation, matched case-insensitively.
func orgColor(cfg Config, org string) color.NRGBA {
	org = strings.ToLower(strings.TrimSpace(org))
	if org == "" {
		return orgUnknown
	}
	for i, known := range orgOrder(cfg) {
		if known == org {
			return orgPalette[i%len(orgPalette)]
		}
	}
	return orgUnknown
}

// orgOf pulls the organisation out of anything that names one: a github URL, an
// "owner/repo#123" issue ref, or a bare "owner/repo".
func orgOf(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "github.com/"); i >= 0 {
		s = s[i+len("github.com/"):]
		// Project URLs read /orgs/<login>/projects/N; issue URLs read
		// /<org>/<repo>/issues/N.
		s = strings.TrimPrefix(s, "orgs/")
		s = strings.TrimPrefix(s, "users/")
	}
	if i := strings.IndexAny(s, "/#"); i >= 0 {
		s = s[:i]
	}
	return s
}

// minutesByOrg splits worklog minutes by the org that owns the issue. The key
// is the lowercased org, so the same org spelled two ways still sums to one.
func minutesByOrg(items []WorklogItem) map[string]int {
	out := map[string]int{}
	for _, it := range items {
		org := strings.ToLower(orgOf(it.URL))
		if org == "" {
			org = strings.ToLower(orgOf(it.ParentURL))
		}
		out[org] += it.Minutes
	}
	return out
}

// orgsByShare orders the orgs in a split: configured order first so a colour
// keeps its place between redraws, with anything unconfigured last.
func orgsByShare(cfg Config, byOrg map[string]int) []string {
	rank := map[string]int{}
	for i, o := range orgOrder(cfg) {
		rank[o] = i
	}
	var out []string
	for org, mins := range byOrg {
		if mins > 0 {
			out = append(out, org)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ri, oki := rank[out[i]]
		rj, okj := rank[out[j]]
		if oki != okj {
			return oki // configured orgs before unknown ones
		}
		if oki && ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}

// meterBar draws minutes against a goal as one horizontal bar, split into a
// segment per organisation in that org's colour. Over-goal work fills the bar
// and is called out by the caller's own text — a bar that could exceed its
// track would just paint over whatever sits beside it.
func meterBar(cfg Config, byOrg map[string]int, goal int, height float32) fyne.CanvasObject {
	return meterBarWithDraft(cfg, byOrg, nil, goal, height)
}

// draftTint is how much of the org's colour a not-yet-pushed segment keeps. Pale
// enough to read as "this is not banked", coloured enough to stay the same org.
const draftTint = 0.45

// meterBarWithDraft is meterBar plus the minutes that are only saved locally,
// drawn after the banked ones in a washed-out shade of the same org colour. The
// two are scaled together, so the bar shows what the day would come to if
// everything waiting were pushed without ever claiming it already has been.
func meterBarWithDraft(cfg Config, byOrg, draft map[string]int, goal int, height float32) fyne.CanvasObject {
	track := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	track.CornerRadius = height / 2
	track.SetMinSize(fyne.NewSize(0, height))
	if goal <= 0 {
		return track
	}

	banked := orgsByShare(cfg, byOrg)
	pending := orgsByShare(cfg, draft)
	total := 0
	for _, o := range banked {
		total += byOrg[o]
	}
	for _, o := range pending {
		total += draft[o]
	}
	if total <= 0 {
		return track
	}

	var weights []float32
	var segs []fyne.CanvasObject
	scale := 1.0
	if total > goal {
		scale = float64(goal) / float64(total) // full bar, shares preserved
	}
	add := func(mins int, fill color.Color) {
		w := float32(float64(mins) / float64(goal) * scale)
		if w <= 0 {
			return
		}
		weights = append(weights, w)
		seg := canvas.NewRectangle(fill)
		seg.CornerRadius = height / 2
		seg.SetMinSize(fyne.NewSize(0, height))
		segs = append(segs, seg)
	}
	for _, o := range banked {
		add(byOrg[o], orgColor(cfg, o))
	}
	for _, o := range pending {
		add(draft[o], blendColor(
			theme.Color(theme.ColorNameInputBackground), orgColor(cfg, o), draftTint))
	}
	if rest := 1 - sumOf(weights); rest > 0.001 {
		weights = append(weights, rest)
		segs = append(segs, canvas.NewRectangle(color.Transparent))
	}
	if len(segs) == 0 {
		return track
	}
	// Tight row: a meter's segments meet, they do not sit apart.
	fill := container.New(newTightRow(weights...), segs...)
	return container.NewStack(track, fill)
}

// meterTint is how much of the org's colour goes into a calendar cell's fill.
// The rest is the background behind it, which is what keeps the day's number
// readable on top. 0.4 is the old 0x66 alpha, mixed rather than layered.
const meterTint = 0.4

// vMeterFill is the same reading as meterBar, drawn as the cell filling from
// the bottom instead of as a bar along it. Colours are washed out towards the
// background because the day's number sits on top and has to stay readable.
func vMeterFill(cfg Config, byOrg map[string]int, goal int, radius float32) fyne.CanvasObject {
	if goal <= 0 {
		return canvas.NewRectangle(color.Transparent)
	}
	orgs := orgsByShare(cfg, byOrg)
	total := 0
	for _, o := range orgs {
		total += byOrg[o]
	}
	if total <= 0 {
		return canvas.NewRectangle(color.Transparent)
	}
	scale := 1.0
	if total > goal {
		scale = float64(goal) / float64(total) // a full cell, shares kept
	}

	var weights []float32
	var segs []fyne.CanvasObject
	// vMeter fills from the bottom in the order it is given, so configured
	// order puts the first org at the base and the colours never swap places
	// between one day and the next.
	for _, o := range orgs {
		w := float32(float64(byOrg[o]) / float64(goal) * scale)
		if w <= 0 {
			continue
		}
		// Washed out so the day's number still reads through, but *opaque*:
		// the segments deliberately reach up behind one another so the rounded
		// joins look solid, and two half-transparent colours over each other
		// blended into a third — a purple band across the seam where blue met
		// red. Mixing the same fraction into the background instead gives the
		// same pale colour with nothing left to blend, so the upper segment
		// simply covers the overlap.
		seg := canvas.NewRectangle(blendColor(
			theme.Color(theme.ColorNameBackground), orgColor(cfg, o), meterTint))
		// The fill lives inside a rounded cell, so it has to be rounded too —
		// square corners poking out of the frame were the fill looking like a
		// separate box laid over the day.
		seg.CornerRadius = radius
		weights = append(weights, w)
		segs = append(segs, seg)
	}
	if len(segs) == 0 {
		return canvas.NewRectangle(color.Transparent)
	}
	return container.New(newVMeter(radius, weights...), segs...)
}

func sumOf(fs []float32) float32 {
	t := float32(0)
	for _, f := range fs {
		t += f
	}
	return t
}

// orgDot is the colour key that goes beside a name or a number.
func orgDot(cfg Config, org string) fyne.CanvasObject {
	d := canvas.NewRectangle(orgColor(cfg, org))
	d.CornerRadius = 5
	d.SetMinSize(fyne.NewSize(10, 10))
	return container.NewCenter(d)
}

// orgLegend names the colours in play, and doubles as the filter: tapping an
// org drops its work out of the view, tapping it again brings it back. Without
// the key the palette is a guess; without the filter, two employers' work can
// only ever be read on top of each other.
//
// Every org in the data is listed whether it is showing or not — a filter you
// cannot see is a filter you cannot undo. A hidden one keeps its place, greyed,
// with its dot washed out to the background.
func orgLegend(cfg Config, byOrg map[string]int, hidden map[string]bool,
	toggle func(org string)) fyne.CanvasObject {
	if len(orgsByShare(cfg, byOrg)) < 2 {
		// A month of one org is one colour, and a key naming the only colour on
		// screen is a row of furniture above the calendar.
		return nil
	}
	return orgLegendWith(cfg, byOrg, hidden, hoursMins, toggle)
}

// orgLegendWith is orgLegend with the amount beside each name spelled out by
// the caller: minutes on the calendar and the report, a commit count on the
// pending list, where "3h 20m of commits" would mean nothing.
//
// It draws for a single org too. The pending list turns over constantly — one
// org today, two tomorrow — and a key that comes and goes is a key you cannot
// look for; the colour on the tiles should always have somewhere that names it.
func orgLegendWith(cfg Config, byOrg map[string]int, hidden map[string]bool,
	amount func(int) string, toggle func(org string)) fyne.CanvasObject {

	orgs := orgsByShare(cfg, byOrg)
	if len(orgs) == 0 {
		return nil // nothing on screen to key
	}
	items := []fyne.CanvasObject{
		widget.NewLabelWithStyle("Filter", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
	}
	for _, o := range orgs {
		o := o
		items = append(items, orgChip(cfg, o, amount(byOrg[o]), hidden[o], toggle))
	}
	return container.NewHBox(items...)
}

// orgChip is one entry in the key: the colour, the name, and what it is worth.
func orgChip(cfg Config, org, amount string, off bool, toggle func(org string)) fyne.CanvasObject {
	fill := orgColor(cfg, org)
	text := theme.Color(theme.ColorNameForeground)
	if off {
		// Off has to read as off at a glance, with no checkbox to spell it out:
		// the dot fades most of the way into the background and the name goes
		// the same grey as any other disabled thing in the app.
		fill = blendColor(theme.Color(theme.ColorNameBackground), fill, 0.25)
		text = theme.Color(theme.ColorNamePlaceHolder)
	}
	dot := canvas.NewRectangle(fill)
	dot.CornerRadius = 5
	dot.SetMinSize(fyne.NewSize(10, 10))

	name := canvas.NewText(fmt.Sprintf("%s (%s)", orDefault(org, "unknown org"), amount), text)
	name.TextSize = theme.TextSize()

	chip := container.NewHBox(container.NewCenter(dot), container.NewCenter(name))
	return newTappable(chip, func() { toggle(org) })
}

// hoursMins renders minutes the way a timesheet reads them.
func hoursMins(m int) string {
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	if m%60 == 0 {
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh %dm", m/60, m%60)
}
