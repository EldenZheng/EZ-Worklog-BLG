package animation

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
)

// A frame that comes late moves an animation on by one ordinary frame, not by
// the time lost; after a few late frames it follows the clock again.
func TestWorklogSteadyCountsALateFrameAsOne(t *testing.T) {
	a := newAnim(&fyne.Animation{Duration: time.Second})
	start := a.start
	a.worklogSteady(start.Add(400 * time.Millisecond))
	if got := a.progressFraction(start.Add(400*time.Millisecond), time.Second); got > 0.02 {
		t.Fatalf("a late first frame should count as one frame, got %v of the way", got)
	}
	// An ordinary frame after it moves it on by that frame.
	now := start.Add(416 * time.Millisecond)
	a.worklogSteady(now)
	if got := a.progressFraction(now, time.Second); got < 0.02 || got > 0.05 {
		t.Fatalf("an ordinary frame should carry on from there, got %v", got)
	}
	for i := 0; i < worklogHolds; i++ {
		now = now.Add(time.Second)
		a.worklogSteady(now)
	}
	before := a.start
	a.worklogSteady(now.Add(time.Second))
	if a.start != before {
		t.Fatal("past its late frames an animation follows the clock")
	}
}
