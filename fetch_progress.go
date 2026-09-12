package main

import "sync"

type fetchProgress struct {
	Done, Total int
	Stage       string
}

// fetchCounter aggregates progress for a fetch and forwards it to zero or more
// listeners. A single counter carries a fetch through every phase — done keeps
// climbing, total grows as later phases add their work — so the aggregated
// bar reads as one journey from 0% to 100% rather than resetting at each stage.
type fetchCounter struct {
	mu          sync.Mutex
	done, total int
	stage       string
	changed     []func(fetchProgress)
}

func newFetchCounter(stage string, changed []func(fetchProgress)) *fetchCounter {
	return &fetchCounter{stage: stage, changed: changed}
}

func (c *fetchCounter) add() {
	c.mu.Lock()
	c.total++
	c.emitLocked()
	c.mu.Unlock()
}

func (c *fetchCounter) finish() {
	c.mu.Lock()
	c.done++
	c.emitLocked()
	c.mu.Unlock()
}

// setStage renames the current work without touching the counts. Done stays
// where it is, and the caption below the bar picks up the new stage on the
// next emit.
func (c *fetchCounter) setStage(stage string) {
	c.mu.Lock()
	c.stage = stage
	c.emitLocked()
	c.mu.Unlock()
}

// addPhase widens the total by delta and renames the stage. Done is left alone
// so the percentage does not fall back to zero when a new phase begins — the
// bar just carries on toward the new, larger total.
func (c *fetchCounter) addPhase(stage string, delta int) {
	c.mu.Lock()
	c.stage = stage
	c.total += delta
	c.emitLocked()
	c.mu.Unlock()
}

// seal is kept for API compatibility. The counter emits its current total on
// every event now, so sealing is redundant.
func (c *fetchCounter) seal() {
	c.mu.Lock()
	c.emitLocked()
	c.mu.Unlock()
}

func (c *fetchCounter) emitLocked() {
	p := fetchProgress{Done: c.done, Total: c.total, Stage: c.stage}
	for _, f := range c.changed {
		if f != nil {
			f(p)
		}
	}
}
