package main

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The loading strip lives at the top of a tab as two thin rows: a slim
// progress bar on top, and a caption plus percentage on the row below. The
// bar borrows the app's gold primary via radioTheme so it does not read as
// stock Fyne blue.
type loadingIndicator struct {
	bar         *widget.ProgressBarInfinite
	percent     *widget.ProgressBar
	caption     *widget.Label
	percentText *widget.Label
	view        *fyne.Container
	// Animation state for smoothing percent transitions. Each new target
	// starts a fresh animation and cancels the running one so the bar cannot
	// chase two targets at once.
	animCurrent, animTarget, animFrom float64
	animRunning                       *fyne.Animation
}

type loadingTheme struct{ fyne.Theme }

func (t loadingTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameInnerPadding, theme.SizeNamePadding:
		return 0
	case theme.SizeNameText:
		// Progress widgets size themselves by MeasureText("100%") — a small
		// text setting is what makes both bars slim.
		return 6
	case theme.SizeNameInputRadius:
		return 3
	}
	return t.Theme.Size(name)
}

func newLoadingIndicator() *loadingIndicator {
	bar := widget.NewProgressBarInfinite()
	percent := widget.NewProgressBar()
	// Suppress the in-bar "50%" label so the finite bar shrinks to the same
	// slim height as the infinite stripe; the numeric percentage lives in a
	// separate label on the caption row.
	percent.TextFormatter = func() string { return "" }
	percent.Hide()

	// Stack overlays the two bars so switching from infinite to percent does
	// not shift the row height. radioTheme paints gold; loadingTheme shrinks.
	bars := container.NewThemeOverride(
		container.NewStack(bar, percent),
		loadingTheme{Theme: radioTheme{Theme: theme.Current()}},
	)
	// Fyne starts the infinite animation when the renderer is created. The
	// theme override triggers renderer creation as a side effect, so stop the
	// animation after wrapping.
	bar.Stop()

	caption := widget.NewLabel("")
	caption.TextStyle = fyne.TextStyle{Italic: true}
	percentText := widget.NewLabel("")
	percentText.Alignment = fyne.TextAlignTrailing
	// Caption on the left, percent hugged to the right on the same row below
	// the bar. Border gives caption a natural width and percentText a natural
	// width, and the middle stays empty so the row reads as two edges.
	captionRow := container.NewBorder(nil, nil, nil, percentText, caption)

	view := container.NewVBox(bars, captionRow)
	view.Hide()
	return &loadingIndicator{bar: bar, percent: percent, caption: caption, percentText: percentText, view: view}
}

func (p *loadingIndicator) setActive(active bool) bool {
	if p == nil || p.view.Visible() == active {
		return false
	}
	if active {
		p.view.Show()
		p.percent.Hide()
		p.percentText.SetText("")
		p.bar.Show()
		p.bar.Start()
	} else {
		p.bar.Stop()
		if p.animRunning != nil {
			p.animRunning.Stop()
			p.animRunning = nil
		}
		p.percentText.SetText("")
		p.view.Hide()
	}
	return true
}

// animateTo eases the finite bar from its current value toward target over a
// short duration. A running animation is cancelled first so the widget cannot
// end up chasing a stale value.
func (p *loadingIndicator) animateTo(target float64) {
	if target < 0 {
		target = 0
	} else if target > 1 {
		target = 1
	}
	if p.animRunning != nil {
		p.animRunning.Stop()
		p.animRunning = nil
	}
	p.animFrom = p.animCurrent
	p.animTarget = target
	if p.animFrom == p.animTarget {
		p.percent.SetValue(p.animTarget)
		return
	}
	anim := fyne.NewAnimation(250*time.Millisecond, func(f float32) {
		v := p.animFrom + (p.animTarget-p.animFrom)*float64(f)
		p.animCurrent = v
		p.percent.SetValue(v)
	})
	anim.Curve = fyne.AnimationEaseOut
	p.animRunning = anim
	anim.Start()
}

func (p *loadingIndicator) showProgress(active bool, state fetchProgress) bool {
	if p == nil {
		return false
	}
	changed := p.setActive(active)
	if !active {
		return changed
	}
	if state.Total > 0 {
		frac := float64(state.Done) / float64(state.Total)
		if frac > 1 {
			frac = 1
		}
		p.bar.Stop()
		p.bar.Hide()
		p.percent.Show()
		p.animateTo(frac)
		p.caption.SetText(state.Stage)
		p.percentText.SetText(fmt.Sprintf("%d%%", int(frac*100+0.5)))
	} else {
		p.percent.Hide()
		p.bar.Show()
		p.bar.Start()
		p.animCurrent = 0
		p.caption.SetText(state.Stage + "…")
		p.percentText.SetText("")
	}
	p.view.Refresh()
	return changed
}

// progressComposer stitches several sequential fetches into one 0-to-100 bar.
// Each fetch is given a weight (a share of the 100 total slots) via phase();
// the returned callback rescales that fetch's own done/total into the phase's
// slice, and advance() marks the phase complete once the caller knows nothing
// else will emit for it. Weights that undersell or oversell a phase either
// stall or under-fill the bar between phases — pick weights that reflect the
// rough share of wall-clock time each phase costs.
type progressComposer struct {
	out       func(fetchProgress)
	completed float64
}

func newProgressComposer(out func(fetchProgress)) *progressComposer {
	return &progressComposer{out: out}
}

func (c *progressComposer) phase(weight float64) func(fetchProgress) {
	return func(p fetchProgress) {
		frac := 0.0
		if p.Total > 0 {
			frac = float64(p.Done) / float64(p.Total)
		}
		if frac > 1 {
			frac = 1
		}
		done := c.completed + frac*weight
		c.out(fetchProgress{
			Done:  int(done + 0.5),
			Total: 100,
			Stage: p.Stage,
		})
	}
}

func (c *progressComposer) advance(weight float64) {
	c.completed += weight
	c.out(fetchProgress{Done: int(c.completed + 0.5), Total: 100})
}

type fetchJob struct {
	key      string
	progress fetchProgress
}

// Background callbacks only enqueue UI updates. Removed jobs ignore late
// updates, and overlapping title lookups retain their own progress entries.
func (ui *UI) beginFetch(key, stage string) (func(fetchProgress), func()) {
	if ui.fetchJobs == nil {
		ui.fetchJobs = map[*fetchJob]bool{}
	}
	job := &fetchJob{key: key, progress: fetchProgress{Stage: stage}}
	ui.fetchJobs[job] = true
	ui.syncLoading()
	return func(p fetchProgress) {
			fyne.Do(func() {
				if !ui.fetchJobs[job] {
					return
				}
				job.progress = p
				ui.syncLoading()
			})
		}, func() {
			delete(ui.fetchJobs, job)
			ui.syncLoading()
		}
}

func (ui *UI) progressFor(keys []string, fallback bool) (bool, fetchProgress) {
	state := fetchProgress{Stage: "Loading from GitHub"}
	count := 0
	for job := range ui.fetchJobs {
		match := false
		for _, key := range keys {
			if job.key == key {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		count++
		state.Done += job.progress.Done
		state.Total += job.progress.Total
		state.Stage = job.progress.Stage
	}
	if count > 1 {
		state.Stage = "GitHub tasks"
	}
	return count > 0 || fallback, state
}

func (ui *UI) syncLoading() {
	selected := ""
	if ui.tabs != nil && ui.tabs.Selected() != nil {
		selected = ui.tabs.Selected().Text
	}
	week := orDefault(ui.weekStart, weekStartOf(today()))
	logKeys := []string{"pending"}
	logLoading := ui.pendingLoading
	for _, month := range ui.logTabMonths() {
		key := statusCacheKey(month)
		logKeys = append(logKeys, key)
		logLoading = logLoading || ui.projLoading[key]
	}
	pages := []struct {
		name      string
		indicator *loadingIndicator
		body      *fyne.Container
		keys      []string
		fallback  bool
	}{
		{logWorkTabName, ui.logProgress, ui.logBody, logKeys, logLoading},
		{logListTabName, ui.listProgress, ui.listBody, logKeys, logLoading},
		{commitListTabName, ui.commitProgress, ui.commitBody, []string{"commits:" + week}, ui.weekCommitLoad[week]},
		{meetingTabName, ui.meetingProgress, ui.meetingBody, []string{"meeting:" + orDefault(ui.meetingStart, meetingWeekStartOf(today()))}, ui.meetingCommitLoad[orDefault(ui.meetingStart, meetingWeekStartOf(today()))]},
		{statusTabName, ui.statusProgress, ui.calBody, []string{statusCacheKey(ui.calMonth)}, ui.projLoading[statusCacheKey(ui.calMonth)]},
		{reportTabName, ui.reportProgress, ui.repBody, []string{reportCacheKey(ui.repMonth), "rate"}, ui.projLoading[reportCacheKey(ui.repMonth)] || ui.rateLoading},
		{settingsTabName, ui.settingsProgress, nil, []string{"profile", "settings-rate"}, ui.profileLoading},
	}
	for _, page := range pages {
		active, state := ui.progressFor(page.keys, page.fallback)
		if page.indicator.showProgress(selected == page.name && active, state) {
			relayout(page.body)
		}
	}
}
