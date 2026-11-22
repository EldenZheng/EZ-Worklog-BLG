package animation

import "time"

// A click that opens a heavy page (the Log Work week, the Kanban board) holds
// up the frames after it, and every animation timed by the clock lost that
// time: a ripple or a fade was over, or nearly, before its first frame was
// drawn. Here an animation is timed by the frames actually drawn instead. A
// frame that comes later than worklogStall moves it on by one ordinary frame,
// worklogFrame, not by the time lost. After worklogHolds late frames it follows
// the clock again, so a page that stays slow cannot drag a motion out.
const (
	worklogStall = 50 * time.Millisecond
	worklogFrame = time.Second / 60
	worklogHolds = 4
)

// worklogSteady moves the animation's clock on past the time lost since its
// last frame, if that frame came late.
func (a *anim) worklogSteady(now time.Time) {
	if !a.worklogLast.IsZero() && a.worklogHolds < worklogHolds {
		if gap := now.Sub(a.worklogLast); gap > worklogStall {
			lost := gap - worklogFrame
			a.start = a.start.Add(lost)
			a.pinTime = a.pinTime.Add(lost)
			a.worklogHolds++
		}
	}
	a.worklogLast = now
}
