package widgets

import (
	"time"

	"fyne.io/fyne/v2"
)

const (
	// steadyStall is how long a frame may take before it counts as late.
	steadyStall = 50 * time.Millisecond
	// steadyFrame is how far a late frame moves the animation on: one ordinary
	// frame, whatever it really took.
	steadyFrame = time.Second / 60
	// steadyHolds is how many late frames one animation waits for. Past that it
	// follows the clock, so a page that stays slow cannot drag it out.
	steadyHolds = 4
)

// Steady is an animation that waits for the frames it is drawn on. Fyne times
// an animation by the clock from its Start, so when the click that starts one
// also rebuilds the page, the frames that would have shown its opening are
// never drawn and it turns up part-way through. Steady counts a late frame as
// an ordinary one and plays the rest from there.
//
// It can also stop part-way and be played on later: a press shrinks while the
// pointer is held down, and returns once it is let go.
type Steady struct {
	duration time.Duration
	tick     func(share float32)
	share    float32
	target   float32
	holds    int
	anim     *fyne.Animation
	first    *fyne.Animation
}

// NewSteady is an animation that hands tick its share, 0 to 1, evenly over
// duration. The caller applies whatever curve it wants. Keep the result before
// calling Start: a driver that does not animate runs tick to the end inside it.
func NewSteady(duration time.Duration, tick func(share float32)) *Steady {
	return &Steady{duration: duration, tick: tick}
}

// Start plays it from the beginning to the end.
func (s *Steady) Start() { s.StartTo(1) }

// StartTo plays it from the beginning as far as target, and leaves it there.
func (s *Steady) StartTo(target float32) {
	s.Stop()
	s.share, s.first = 0, nil
	s.PlayTo(target)
}

// PlayTo plays it on from where it is as far as target, taking that share of
// the duration.
func (s *Steady) PlayTo(target float32) {
	s.Stop()
	s.target, s.holds = target, 0
	if s.share >= target {
		s.tick(s.share)
		return
	}
	s.run()
}

// Animation is the Fyne animation Start set going, for a caller that has one
// to hand back. After a late frame another carries on in its place.
func (s *Steady) Animation() *fyne.Animation { return s.first }

// Stop leaves it where it is. A nil Steady has nothing to stop.
func (s *Steady) Stop() {
	if s != nil && s.anim != nil {
		s.anim.Stop()
		s.anim = nil
	}
}

// run plays from the share it has reached as far as the target.
func (s *Steady) run() {
	from, last := s.share, time.Now()
	span := s.target - from
	left := time.Duration(float64(s.duration) * float64(span))
	var anim *fyne.Animation
	anim = fyne.NewAnimation(left, func(done float32) {
		if s.anim != anim {
			return
		}
		now := time.Now()
		late := now.Sub(last) > steadyStall
		last = now
		if late && s.holds < steadyHolds {
			// The clock ran on while nothing was drawn: move one frame on and
			// time the rest from now.
			s.holds++
			s.share = min(s.target, s.share+float32(steadyFrame)/float32(s.duration))
			anim.Stop()
			s.anim = nil
			if s.share < s.target {
				s.run()
			}
			s.tick(s.share)
			return
		}
		s.share = from + span*done
		if done >= 1 {
			s.share, s.anim = s.target, nil
		}
		s.tick(s.share)
	})
	anim.Curve = fyne.AnimationLinear
	s.anim = anim
	if s.first == nil {
		s.first = anim
	}
	anim.Start()
}
