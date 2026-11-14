package shell

import (
	"fmt"
	"image/color"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

// WeekendSlateTab is the Log Work tab the slate form sits on.
const WeekendSlateTab = "Create Weekend Support Slate"

// WeekendSlate is the form that files a week of weekend support: the team's
// epic link, the week, and a mini calendar of its seven days on which a weekday
// can be marked a holiday (covered all day, like the weekend). View is the form;
// Create is its button, kept apart so a popup can line it up with its own.
type WeekendSlate struct {
	View   fyne.CanvasObject
	Create fyne.CanvasObject

	ui         *UI
	link       *widget.Entry
	epic       *widget.Label
	week       string
	holidays   map[string]bool
	rangeLabel *widget.Label
	days       *fyne.Container
	preview    *widget.Label
	result     *fyne.Container
	create     *widget.Button
	version    int // bumped on each edit of the link, so a stale read is dropped

	// The days arrive one after another, as the other calendars' do; motion
	// is this form's own, so the popup and the tab do not share one.
	motion      UI
	arriveAfter time.Duration
}

// SupportAccent is the weekend support blue: the support bonus figures on the
// Report, and the slate's tab while it is not picked.
func SupportAccent() color.NRGBA {
	r, g, b, _ := theme.Color(theme.ColorNameBackground).RGBA()
	if r+g+b < 3*0x8000 {
		return color.NRGBA{R: 0x64, G: 0xb5, B: 0xf6, A: 0xff}
	}
	return color.NRGBA{R: 0x15, G: 0x65, B: 0xc0, A: 0xff}
}

// NewWeekendSlate builds the form on the current week.
func (ui *UI) NewWeekendSlate() *WeekendSlate {
	s := &WeekendSlate{ui: ui, week: backend.WeekStartOf(Today()), holidays: map[string]bool{}}
	s.link = widget.NewEntry()
	s.link.SetPlaceHolder("https://github.com/owner/repo/issues/123")
	s.link.SetText(ui.SlateEpic)
	// The epic is read once typing settles, as Code Review reads its link.
	s.link.OnChanged = func(t string) {
		ui.SlateEpic = strings.TrimSpace(t)
		s.version++
		s.epic.SetText("")
		s.sync()
		if _, err := s.ref(); err != nil {
			return
		}
		version := s.version
		AfterTyping(func() {
			if version == s.version {
				s.check(version)
			}
		})
	}
	s.epic = widget.NewLabel("")
	s.epic.Wrapping = fyne.TextWrapWord

	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() { s.setWeek(ShiftWeek(s.week, -1)) })
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() { s.setWeek(ShiftWeek(s.week, 1)) })
	s.rangeLabel = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	// Gold, arrows and all, like the epic's caption above it.
	weekRow := container.NewThemeOverride(
		container.NewBorder(nil, nil, QuietButton(prev), QuietButton(next), s.rangeLabel),
		goldTextTheme{Theme: theme.Current()})

	s.days = container.NewGridWithColumns(7)
	s.preview = widget.NewLabel("")
	s.preview.Wrapping = fyne.TextWrapWord
	s.result = container.NewVBox()

	s.create = widget.NewButton(WeekendSlateTab, s.run)
	s.Create = GitHubButton(s.create)

	hint := widget.NewLabel("Tap a weekday that is a public holiday to cover it the whole day.")
	hint.Wrapping = fyne.TextWrapWord
	s.View = container.NewVBox(
		container.NewThemeOverride(widget.NewLabelWithStyle("Weekend support epic", fyne.TextAlignLeading,
			fyne.TextStyle{Bold: true}), goldTextTheme{Theme: theme.Current()}),
		s.link,
		s.epic,
		weekRow,
		s.days,
		hint,
		s.preview,
		s.result,
	)
	s.setWeek(s.week)
	if _, err := s.ref(); err == nil {
		s.check(s.version) // the link remembered from last time
	}
	return s
}

// ShowWeekendSlatePopup opens the slate form as a popup.
func (ui *UI) ShowWeekendSlatePopup() {
	s := ui.NewWeekendSlate()
	var d dialog.Dialog
	closeBtn := widget.NewButton("Close", func() { CloseDialog(d, ui.Win) })
	content := container.NewBorder(nil, DialogButtons(closeBtn, s.Create), nil, nil, FitPopup(s.View))
	d = dialog.NewCustomWithoutButtons(ui.PopupTitle("Weekend support slate"), content, ui.Win)
	d.Resize(ui.PopupSizeFor(s.View))
	ShowDialog(d, ui.Win)
	s.Arrive(popupOpenTime) // the days come in once the popup has risen
}

// Arrive brings the days in again after delay, for a tab that has just been
// opened onto the form.
func (s *WeekendSlate) Arrive(delay time.Duration) {
	s.motion.ReplayArrival("slate")
	s.arriveAfter = delay
	s.drawDays()
}

// InlineView is the form as a page: the button under it, at the right.
func (s *WeekendSlate) InlineView() fyne.CanvasObject {
	return container.NewVBox(s.View, container.NewHBox(layout.NewSpacer(), s.Create))
}

func (s *WeekendSlate) ref() (string, error) { return backend.IssueRefFromAny(s.link.Text) }

// check reads the epic: its title, the week its title names, and whether the
// user's own epic for that week is already under it.
func (s *WeekendSlate) check(version int) {
	ref, err := s.ref()
	if err != nil {
		return
	}
	s.epic.SetText("Reading " + ref + "…")
	var node backend.IssueNode
	var readErr error
	s.ui.Async(func() error {
		_, _, _, node, readErr = backend.GetIssueContext(ref)
		return nil
	}, func() {
		if version != s.version {
			return // the link changed while this was being read
		}
		if readErr != nil {
			s.epic.SetText(readErr.Error())
			return
		}
		text := node.Title
		if week := backend.WeekendSupportWeekFromTitle(node.Title, time.Now()); week != "" {
			s.setWeek(week)
		}
		mine := backend.WeekendSupportEpicTitle(s.ui.Cfg, s.week)
		for _, sub := range node.SubIssues.Nodes {
			if strings.EqualFold(strings.TrimSpace(sub.Title), mine) {
				text += fmt.Sprintf("\n#%d is already filed for this week; creating again only adds the days it is missing.", sub.Number)
				break
			}
		}
		s.epic.SetText(text)
	})
}

func (s *WeekendSlate) setWeek(week string) {
	if week != s.week {
		s.holidays = map[string]bool{}
	}
	s.week = week
	s.rangeLabel.SetText(WeekRangeLabel(week))
	s.drawDays()
	s.sync()
}

// drawDays lays out the week, Monday first.
func (s *WeekendSlate) drawDays() {
	var tiles []fyne.CanvasObject
	for _, ds := range WeekDates(s.week) {
		tiles = append(tiles, s.dayTile(ds))
	}
	arrive := s.motion.Arrival("slate", s.week).After(s.arriveAfter)
	s.arriveAfter = 0
	s.days.Objects = arrive.Reveal(tiles, GridDelays(len(tiles), 7, 0), nil)
	s.days.Refresh()
}

// dayTile is one day: its name and date, and the hours it is covered. Weekend
// and holiday tiles are washed gold; tapping a weekday makes it a holiday, and
// the gold and the hours fade across rather than jumping.
func (s *WeekendSlate) dayTile(ds string) fyne.CanvasObject {
	t, _ := time.Parse("2006-01-02", ds)
	weekend := backend.IsWeekend(ds)
	fg := theme.Color(theme.ColorNameForeground)
	bg := theme.Color(theme.ColorNameBackground)
	caption := func(txt string, bold bool) *canvas.Text {
		c := canvas.NewText(txt, fg)
		c.TextSize = theme.CaptionTextSize()
		c.TextStyle = fyne.TextStyle{Bold: bold}
		c.Alignment = fyne.TextAlignCenter
		return c
	}
	if weekend {
		wash := canvas.NewRectangle(BlendColor(bg, widgets.LogoGold, 0.35))
		wash.CornerRadius = CellCornerRadius
		wash.StrokeWidth = 1
		wash.StrokeColor = theme.Color(theme.ColorNameInputBorder)
		return container.NewStack(wash, container.NewPadded(container.NewVBox(
			caption(t.Format("Mon"), true), caption(t.Format("02 Jan"), false),
			caption("Weekend", true), caption("8am - 11pm", false))))
	}

	// A weekday holds both readings, workday and holiday, one over the other,
	// and fades from one to the other.
	restKind, restShift := caption("Workday", false), caption("8-9am, 5-11pm", false)
	fullKind, fullShift := caption("Holiday", true), caption("8am - 11pm", false)
	wash := canvas.NewRectangle(bg)
	wash.CornerRadius = CellCornerRadius
	paint := func(level float32) {
		wash.FillColor = BlendColor(bg, widgets.LogoGold, 0.35*level)
		wash.StrokeColor = MixColor(theme.Color(theme.ColorNameInputBorder), widgets.LogoGold, level)
		wash.StrokeWidth = 1 + level
		wash.Refresh()
		for _, c := range []*canvas.Text{restKind, restShift} {
			c.Color = MixColor(fg, color.Transparent, level)
			c.Refresh()
		}
		for _, c := range []*canvas.Text{fullKind, fullShift} {
			c.Color = MixColor(color.Transparent, fg, level)
			c.Refresh()
		}
	}
	glow, hover := &Glow{}, &Glow{}
	if s.holidays[ds] {
		glow.level = 1
	}
	// Under the pointer the tile lifts a little and its edge warms to gold,
	// fading in and out like the tabs above.
	draw := func(float32) {
		paint(glow.level)
		wash.FillColor = MixColor(wash.FillColor, fg, 0.08*hover.level)
		wash.StrokeColor = MixColor(wash.StrokeColor, widgets.LogoGold, 0.7*hover.level)
		wash.Refresh()
	}
	draw(0)
	ease := func(g *Glow, target float32) {
		if !animationsOn() {
			g.level = target
			draw(target)
			return
		}
		g.To(target, draw)
	}
	body := container.NewVBox(caption(t.Format("Mon"), true), caption(t.Format("02 Jan"), false),
		container.NewStack(restKind, fullKind), container.NewStack(restShift, fullShift))
	tile := NewTappable(container.NewStack(wash, container.NewPadded(body)), func() {
		s.holidays[ds] = !s.holidays[ds]
		target := float32(0)
		if s.holidays[ds] {
			target = 1
		}
		ease(glow, target)
	})
	tile.OnHover = func(on bool) {
		target := float32(0)
		if on {
			target = 1
		}
		ease(hover, target)
	}
	return tile
}

// sync says what will be filed, and enables Create once there is an epic to
// file it under.
func (s *WeekendSlate) sync() {
	if s.preview == nil {
		return
	}
	s.preview.SetText("Creates epic \"" + backend.WeekendSupportEpicTitle(s.ui.Cfg, s.week) +
		"\" with an issue for each of its 7 days.")
	if _, err := s.ref(); err != nil {
		s.create.Disable()
	} else {
		s.create.Enable()
	}
}

// run files the slate, then lists what was filed and anything that went wrong.
func (s *WeekendSlate) run() {
	ref, err := s.ref()
	if err != nil {
		s.showResult(err.Error(), "", nil)
		return
	}
	holidays := map[string]bool{}
	for d, on := range s.holidays {
		holidays[d] = on
	}
	week := s.week
	progress, stop := s.ui.PushSpinner("Filing weekend support…")
	var res backend.SlateResult
	var runErr error
	s.ui.Async(func() error {
		res, runErr = backend.CreateWeekendSupportSlate(s.ui.Cfg, ref, week, holidays, progress)
		return nil
	}, func() {
		stop()
		if runErr != nil {
			s.showResult(runErr.Error(), "", nil)
			return
		}
		msg := fmt.Sprintf("Filed %d of 7 days.", len(res.DayURLs))
		if len(res.Problems) == 0 && len(res.DayURLs) == 7 {
			msg = "Weekend support slate is ready: 7 days filed."
		}
		s.showResult(msg, res.EpicURL, append(res.Notes, res.Problems...))
	})
}

func (s *WeekendSlate) showResult(msg, epicURL string, lines []string) {
	s.result.Objects = nil
	s.result.Add(Bold(msg))
	if u, err := url.Parse(epicURL); err == nil && epicURL != "" {
		s.result.Add(widget.NewHyperlink(epicURL, u))
	}
	for _, l := range lines {
		lbl := widget.NewLabel(l)
		lbl.Wrapping = fyne.TextWrapWord
		s.result.Add(lbl)
	}
	s.result.Refresh()
}
