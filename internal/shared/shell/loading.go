package shell

import (
	"fmt"
	"time"
	"worklog/internal/shared/backend"

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
	Bar         *widget.ProgressBarInfinite
	percent     *widget.ProgressBar
	bars        *container.ThemeOverride
	Caption     *widget.Label
	percentText *widget.Label
	View        *fyne.Container
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

func NewLoadingIndicator() *loadingIndicator {
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
		loadingTheme{Theme: RadioTheme{Theme: theme.Current()}},
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
	return &loadingIndicator{Bar: bar, percent: percent, bars: bars, Caption: caption, percentText: percentText, View: view}
}

func (p *loadingIndicator) SetActive(active bool) bool {
	if p == nil || p.View.Visible() == active {
		return false
	}
	if active {
		p.View.Show()
		p.percent.Hide()
		p.percentText.SetText("")
		// While hidden, Fyne's cache expires the bars' renderers and drops the
		// theme override with them. Showing the bar first would rebuild it in
		// stock blue, and a running infinite bar never repaints its colour, so
		// re-apply the override while the bar is still stopped.
		p.bars.Refresh()
		p.Bar.Show()
		p.Bar.Start()
	} else {
		p.Bar.Stop()
		if p.animRunning != nil {
			p.animRunning.Stop()
			p.animRunning = nil
		}
		p.percentText.SetText("")
		p.View.Hide()
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

func (p *loadingIndicator) ShowProgress(active bool, state backend.FetchProgress) bool {
	if p == nil {
		return false
	}
	changed := p.SetActive(active)
	if !active {
		return changed
	}
	if state.Total > 0 {
		frac := float64(state.Done) / float64(state.Total)
		if frac > 1 {
			frac = 1
		}
		p.Bar.Stop()
		p.Bar.Hide()
		p.percent.Show()
		p.animateTo(frac)
		p.Caption.SetText(state.Stage)
		p.percentText.SetText(fmt.Sprintf("%d%%", int(frac*100+0.5)))
	} else {
		p.percent.Hide()
		if !p.Bar.Visible() {
			p.bars.Refresh() // same expired-override repaint as setActive
		}
		p.Bar.Show()
		p.Bar.Start()
		p.animCurrent = 0
		p.Caption.SetText(state.Stage + "…")
		p.percentText.SetText("")
	}
	p.View.Refresh()
	return changed
}

// ProgressComposer stitches several sequential fetches into one 0-to-100 bar.
// Each fetch is given a weight (a share of the 100 total slots) via phase();
// the returned callback rescales that fetch's own done/total into the phase's
// slice, and advance() marks the phase complete once the caller knows nothing
// else will emit for it. Weights that undersell or oversell a phase either
// stall or under-fill the bar between phases — pick weights that reflect the
// rough share of wall-clock time each phase costs.
type ProgressComposer struct {
	out       func(backend.FetchProgress)
	completed float64
}

func NewProgressComposer(out func(backend.FetchProgress)) *ProgressComposer {
	return &ProgressComposer{out: out}
}

func (c *ProgressComposer) Phase(weight float64) func(backend.FetchProgress) {
	return func(p backend.FetchProgress) {
		frac := 0.0
		if p.Total > 0 {
			frac = float64(p.Done) / float64(p.Total)
		}
		if frac > 1 {
			frac = 1
		}
		done := c.completed + frac*weight
		c.out(backend.FetchProgress{
			Done:  int(done + 0.5),
			Total: 100,
			Stage: p.Stage,
		})
	}
}

func (c *ProgressComposer) Advance(weight float64) {
	c.completed += weight
	c.out(backend.FetchProgress{Done: int(c.completed + 0.5), Total: 100})
}

type fetchJob struct {
	key      string
	progress backend.FetchProgress
}

// Background callbacks only enqueue UI updates. Removed jobs ignore late
// updates, and overlapping title lookups retain their own progress entries.
func (ui *UI) BeginFetch(key, stage string) (func(backend.FetchProgress), func()) {
	if ui.fetchJobs == nil {
		ui.fetchJobs = map[*fetchJob]bool{}
	}
	job := &fetchJob{key: key, progress: backend.FetchProgress{Stage: stage}}
	ui.fetchJobs[job] = true
	ui.SyncLoading()
	return func(p backend.FetchProgress) {
			fyne.Do(func() {
				if !ui.fetchJobs[job] {
					return
				}
				job.progress = p
				ui.SyncLoading()
			})
		}, func() {
			delete(ui.fetchJobs, job)
			ui.SyncLoading()
		}
}

func (ui *UI) progressFor(keys []string, fallback bool) (bool, backend.FetchProgress) {
	state := backend.FetchProgress{Stage: "Loading from GitHub"}
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

func (ui *UI) SyncLoading() {
	selected := ""
	if ui.Tabs != nil && ui.Tabs.Selected() != nil {
		selected = ui.Tabs.Selected().Text
	}
	week := backend.OrDefault(ui.WeekStart, backend.WeekStartOf(Today()))
	// Log Work's top bar only tracks the pending commit sweep — the calendar
	// below it has its own weekStripProgress that lights up while boards load,
	// and reporting the same fetch on both bars had users chasing the top one
	// when the answer was under their eyes on the strip. Log List has no
	// strip of its own, so its top bar still covers the board load.
	logWorkKeys := []string{"pending"}
	logWorkLoading := ui.PendingLoading
	logListKeys := []string{"pending"}
	logListLoading := ui.PendingLoading
	var calendarKeys []string
	calendarLoading := false
	for _, month := range ui.LogTabMonths() {
		key := StatusCacheKey(month)
		logListKeys = append(logListKeys, key)
		logListLoading = logListLoading || ui.ProjLoading[key]
		calendarKeys = append(calendarKeys, key)
		calendarLoading = calendarLoading || ui.ProjLoading[key]
	}
	pages := []struct {
		name      string
		indicator *loadingIndicator
		body      *fyne.Container
		keys      []string
		fallback  bool
	}{
		{LogWorkTabName, ui.LogProgress, ui.LogBody, logWorkKeys, logWorkLoading},
		// Second entry for the same tab: the strip's own indicator, driven by
		// the calendar keys only. Both bars can light up together — one at the
		// top for the pending sweep, one over the strip for the boards — but
		// each shows only what its own fetch is doing.
		{LogWorkTabName, ui.WeekStripProgress, ui.WeekBox, calendarKeys, calendarLoading},
		{LogListTabName, ui.ListProgress, ui.ListBody, logListKeys, logListLoading},
		{CommitListTabName, ui.CommitProgress, ui.CommitBody, []string{"commits:" + week}, ui.WeekCommitLoad[week]},
		{MeetingTabName, ui.MeetingProgress, ui.MeetingBody, []string{"meeting:" + backend.OrDefault(ui.MeetingStart, MeetingWeekStartOf(Today()))}, ui.MeetingCommitLoad[backend.OrDefault(ui.MeetingStart, MeetingWeekStartOf(Today()))]},
		{StatusTabName, ui.StatusProgress, ui.CalBody, []string{StatusCacheKey(ui.CalMonth)}, ui.ProjLoading[StatusCacheKey(ui.CalMonth)]},
		{ReportTabName, ui.ReportProgress, ui.RepBody, []string{ReportCacheKey(ui.RepMonth), "rate"}, ui.ProjLoading[ReportCacheKey(ui.RepMonth)] || ui.RateLoading},
		{SettingsTabName, ui.SettingsProgress, nil, []string{"profile", "settings-rate"}, ui.ProfileLoading},
	}
	for _, page := range pages {
		active, state := ui.progressFor(page.keys, page.fallback)
		if page.indicator.ShowProgress(selected == page.name && active, state) {
			Relayout(page.body)
		}
	}
	if ui.Kanban != nil {
		ui.Kanban.SyncActivity()
	}
}
