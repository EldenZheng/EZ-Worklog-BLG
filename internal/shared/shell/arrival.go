package shell

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// Views arrive the way Kanban's cards do: when a view shows something new (a
// week, a month, a period), its pieces are uncovered from the top down one
// after another, left to right, and its figures count up to their values.
// One animation drives the lot, so Fyne's test driver, which runs an animation
// straight to its end, sees everything in place.

const (
	// arriveEach is how long one piece takes to uncover; arriveStep how far
	// behind its left neighbour it starts.
	arriveEach = 460 * time.Millisecond
	arriveStep = 65 * time.Millisecond
	// arriveSpan caps the whole cascade, however many pieces there are.
	arriveSpan = 650 * time.Millisecond
	countTime  = 700 * time.Millisecond
)

// arrivalLimit is how long after it started a redraw can still join an
// arrival; past it the arrival is over, whatever became of its animations.
const arrivalLimit = 2 * time.Second

// arrivalState is one view's arrival: what it is showing, since when, and the
// animations playing it.
type arrivalState struct {
	identity string
	started  time.Time
	running  []*fyne.Animation
	live     int
	done     bool
}

func (s *arrivalState) stop() {
	for _, anim := range s.running {
		anim.Stop()
	}
	s.running, s.live = nil, 0
}

// Arrival plays a view's arrival. A nil Arrival plays nothing, and its methods
// leave the view as it is.
type Arrival struct {
	state   *arrivalState
	elapsed time.Duration // how far into the arrival this draw comes in
}

// Arrival is slot's arrival for identity: a new one when identity is new to
// slot, the one already playing when a redraw comes in before it is over (it
// picks up where it had got to), or nil when it has finished. Call it only once
// the view has its data: a view drawn while still loading arrives when the
// data lands.
func (ui *UI) Arrival(slot, identity string) *Arrival {
	if !animationsOn() {
		return nil
	}
	if ui.arrivals == nil {
		ui.arrivals = map[string]*arrivalState{}
	}
	s := ui.arrivals[slot]
	if s == nil || s.identity != identity {
		if s != nil {
			s.stop()
		}
		s = &arrivalState{identity: identity, started: time.Now()}
		ui.arrivals[slot] = s
		return &Arrival{state: s}
	}
	since := time.Since(s.started)
	if s.done || since >= arrivalLimit {
		s.done = true
		return nil
	}
	// The views the earlier draw built are gone; this draw takes over.
	s.stop()
	return &Arrival{state: s, elapsed: since}
}

// Fetching reports whether project data for any of keys is being fetched now.
// A view showing data that is still being refreshed holds its arrival back for
// the draw that comes when the fetch lands, so the fresh data is what arrives.
func (ui *UI) Fetching(keys ...string) bool {
	for _, key := range keys {
		if ui.ProjLoading[key] {
			return true
		}
	}
	return false
}

// WeekFetching is Fetching for the months the given days fall in.
func (ui *UI) WeekFetching(days []string) bool {
	var keys []string
	for _, d := range days {
		if len(d) >= 7 {
			keys = append(keys, StatusCacheKey(d[:7]))
		}
	}
	return ui.Fetching(keys...)
}

// ReplayArrival makes slot's next draw arrive again, as when its tab is opened.
func (ui *UI) ReplayArrival(slot string) {
	if s := ui.arrivals[slot]; s != nil {
		s.stop()
	}
	delete(ui.arrivals, slot)
}

// play starts anim as part of the arrival, marking it done once the last of
// its animations ends.
func (a *Arrival) play(anim *fyne.Animation, ended *func()) {
	s := a.state
	*ended = func() {
		s.live--
		if s.live <= 0 {
			s.done = true
		}
	}
	s.live++
	s.running = append(s.running, anim)
	anim.Start()
}

// easeOutQuart settles more softly than a cubic: most of the move early, then a
// long gentle approach, so pieces glide into place rather than snap.
func easeOutQuart(p float32) float32 {
	q := 1 - p
	return 1 - q*q*q*q
}

// After holds the arrival back by d, for a view that has to finish some other
// move first (a form sliding in). Its pieces stay covered until then.
func (a *Arrival) After(d time.Duration) *Arrival {
	if a != nil {
		a.elapsed -= d
	}
	return a
}

// ArriveDelays spaces n pieces arriveStep apart, closer when there are many, so
// the cascade never runs past arriveSpan.
func ArriveDelays(n int) []time.Duration {
	step := arriveStep
	if n > 1 && time.Duration(n-1)*step > arriveSpan {
		step = arriveSpan / time.Duration(n-1)
	}
	delays := make([]time.Duration, n)
	for i := range delays {
		delays[i] = time.Duration(i) * step
	}
	return delays
}

// Cascade plays a cascade as part of the arrival: piece i starts delays[i] in
// and takes arriveEach, and set gets each piece's share, 0 to 1, on every
// frame. A redraw that joins the arrival late starts where it had got to.
func (a *Arrival) Cascade(delays []time.Duration, set func(i int, share float32)) {
	if a == nil || len(delays) == 0 {
		return
	}
	last := time.Duration(0)
	for _, d := range delays {
		last = max(last, d)
	}
	total := last + arriveEach
	at := func(elapsed time.Duration) {
		for i, d := range delays {
			share := min(1, max(0, float32(elapsed-d)/float32(arriveEach)))
			// Every piece receives every frame: charts flush the entire frame
			// from their final callback, even before that bar starts moving.
			set(i, easeOutQuart(share))
		}
	}
	if a.elapsed >= total {
		at(total)
		return
	}
	at(a.elapsed)
	from, left := a.elapsed, total-a.elapsed
	var ended func()
	anim := fyne.NewAnimation(left, func(done float32) {
		if done >= 1 {
			at(total)
			ended()
			return
		}
		at(from + time.Duration(float64(left)*float64(done)))
	})
	anim.Curve = fyne.AnimationLinear
	a.play(anim, &ended)
}

// Reveal wraps each piece in a Reveal and plays a cascade over them with the
// given delays, also calling then (if set) with each piece's share. A nil
// Arrival hands the pieces back as they are.
func (a *Arrival) Reveal(pieces []fyne.CanvasObject, delays []time.Duration, then func(i int, share float32)) []fyne.CanvasObject {
	if a == nil {
		return pieces
	}
	reveals := make([]*Reveal, len(pieces))
	wrapped := make([]fyne.CanvasObject, len(pieces))
	for i, p := range pieces {
		reveals[i] = NewReveal(p)
		wrapped[i] = reveals[i]
	}
	a.Cascade(delays, func(i int, share float32) {
		reveals[i].SetShare(share)
		if then != nil {
			then(i, share)
		}
	})
	return wrapped
}

// RevealIn is Reveal over a container's own objects, in place, one after
// another in their order.
func (a *Arrival) RevealIn(box *fyne.Container) {
	if a == nil || box == nil {
		return
	}
	box.Objects = a.Reveal(box.Objects, ArriveDelays(len(box.Objects)), nil)
	box.Refresh()
}

// CountUp runs each text's first figure up from zero to what it says now, as
// part of the arrival.
func (a *Arrival) CountUp(texts []*canvas.Text) {
	if a == nil || len(texts) == 0 {
		return
	}
	finals := make([]string, len(texts))
	for i, t := range texts {
		finals[i] = t.Text
	}
	set := func(share float32) {
		for i, t := range texts {
			t.Text = ScaleFirstNumber(finals[i], share)
			if share >= 1 {
				t.Text = finals[i]
			}
			t.Refresh()
		}
	}
	if a.elapsed >= countTime {
		set(1)
		return
	}
	from := float32(a.elapsed) / float32(countTime)
	set(easeOutCubic(from))
	var ended func()
	anim := fyne.NewAnimation(countTime-a.elapsed, func(done float32) {
		set(easeOutCubic(from + (1-from)*done))
		if done >= 1 {
			ended()
		}
	})
	anim.Curve = fyne.AnimationLinear
	a.play(anim, &ended)
}

// Reveal shows its content uncovered from the top down to its share. Once fully
// uncovered it stops clipping, so nothing that reaches past its edge, such as a
// card's shadow or a dragged tile, is cut off afterwards.
type Reveal struct {
	widget.BaseWidget
	Content   fyne.CanvasObject
	OnDismiss func()
	// Fold makes the reveal ask for only its share of the content's height,
	// so whatever holds it grows and shrinks with it (a drop-down).
	Fold  bool
	clip  *clipBox
	share float32
}

// DismissPopup is used by the private Fyne overlay for outside clicks.
func (r *Reveal) DismissPopup() bool {
	if r.OnDismiss == nil {
		return false
	}
	r.OnDismiss()
	return true
}

// NewReveal wraps content, fully uncovered until SetShare says otherwise.
func NewReveal(content fyne.CanvasObject) *Reveal {
	r := &Reveal{Content: content, clip: newClipBox(), share: 1}
	r.ExtendBaseWidget(r)
	return r
}

// Share is how much of the content shows, 0 to 1.
func (r *Reveal) Share() float32 { return r.share }

// SetShare uncovers that share of the content, from the top.
func (r *Reveal) SetShare(share float32) {
	share = min(1, max(0, share))
	if share == r.share {
		return
	}
	r.share = share
	r.Refresh()
}

func (r *Reveal) CreateRenderer() fyne.WidgetRenderer { return &revealRenderer{r: r} }

type revealRenderer struct {
	r       *Reveal
	clipped bool
}

func (rr *revealRenderer) Layout(size fyne.Size) {
	r := rr.r
	// A reveal that was never placed is one a redraw replaced before it was
	// shown. Its content may be shown elsewhere now (a meter is kept from draw to
	// draw), so it must not be resized to nothing from here.
	if size.IsZero() {
		return
	}
	full := size
	if r.Fold {
		full.Height = max(size.Height, r.Content.MinSize().Height)
	}
	if r.Content.Position() != fyne.NewPos(0, 0) {
		r.Content.Move(fyne.NewPos(0, 0))
	}
	if r.Content.Size() != full {
		r.Content.Resize(full)
	}
	r.clip.Resize(fyne.NewSize(size.Width, full.Height*r.share))
}

func (rr *revealRenderer) MinSize() fyne.Size {
	m := rr.r.Content.MinSize()
	if rr.r.Fold {
		m.Height *= rr.r.share
	}
	return m
}

func (rr *revealRenderer) Refresh() {
	// The content lives inside the clip while it is being uncovered, and on its
	// own once it is not.
	clip := rr.r.share < 1
	if clip != rr.clipped {
		rr.clipped = clip
		if clip {
			rr.r.clip.objects.Objects = []fyne.CanvasObject{rr.r.Content}
		} else {
			rr.r.clip.objects.Objects = nil
		}
	}
	rr.Layout(rr.r.Size())
	canvas.Refresh(rr.r)
}

func (rr *revealRenderer) Objects() []fyne.CanvasObject {
	if rr.clipped {
		return []fyne.CanvasObject{rr.r.clip}
	}
	return []fyne.CanvasObject{rr.r.Content}
}

func (rr *revealRenderer) Destroy() {}

// MeterShare fills slot's bar to share of its reading, for a bar that fills in
// as its view arrives. At 1 the bar shows its reading exactly.
func (ui *UI) MeterShare(slot string, share float32) {
	m := ui.Meters[slot]
	if m == nil {
		return
	}
	if m.Animation != nil {
		m.Animation.Stop()
		m.Animation = nil
	}
	if share >= 1 {
		if sameMeterParts(m.Parts, m.Target) {
			return
		}
		m.setParts(m.Target)
		return
	}
	parts := append([]MeterPart(nil), m.Target...)
	for i := range parts {
		parts[i].Weight *= share
	}
	if len(m.Fill.Objects) != len(parts) {
		m.setParts(parts)
		return
	}
	m.Parts = parts
	m.Fill.Refresh()
}

// firstNumber is the first figure in a line of text: digits with any thousands
// commas and decimals.
var firstNumber = regexp.MustCompile(`\d{1,3}(?:,\d{3})+(?:\.\d+)?|\d+(?:\.\d+)?`)

// ScaleFirstNumber is text with its first figure multiplied by share, written
// the way the figure was: the same decimals, with thousands commas if it had
// them. "RM 1,234.50" at a half reads "RM 617.25".
func ScaleFirstNumber(text string, share float32) string {
	loc := firstNumber.FindStringIndex(text)
	if loc == nil {
		return text
	}
	raw := text[loc[0]:loc[1]]
	commas := strings.Contains(raw, ",")
	value, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64)
	if err != nil {
		return text
	}
	decimals := 0
	if dot := strings.IndexByte(raw, '.'); dot >= 0 {
		decimals = len(raw) - dot - 1
	}
	scaled := strconv.FormatFloat(value*float64(share), 'f', decimals, 64)
	if commas {
		scaled = groupThousands(scaled)
	}
	return text[:loc[0]] + scaled + text[loc[1]:]
}

func groupThousands(s string) string {
	whole, frac := s, ""
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		whole, frac = s[:dot], s[dot:]
	}
	var b strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	return b.String() + frac
}

// GridDelays orders a calendar's cells left to right, each column a step
// behind the one before and each row a half step behind the one above. offset
// is how many blank cells come before the first.
func GridDelays(cells, columns, offset int) []time.Duration {
	delays := make([]time.Duration, cells)
	for i := range delays {
		at := i + offset
		col, row := at%columns, at/columns
		delays[i] = time.Duration(col)*arriveStep + time.Duration(row)*arriveStep/2
	}
	return delays
}
