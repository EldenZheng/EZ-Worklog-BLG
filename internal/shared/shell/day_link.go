package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// OpenDayOnStatus jumps to a day on the Status tab, loading its month if the
// tab was looking at a different one.
func (ui *UI) OpenDayOnStatus(date string) {
	if len(date) < 7 {
		return
	}
	if month := date[:7]; ui.CalMonth != month {
		ui.CalMonth = month
		ui.LoadStatus(false)
	}
	ui.SelDay = date
	Hooks.DrawCalendar(ui)
	Hooks.DrawDayPanel(ui)
	if ui.Tabs != nil {
		ui.Tabs.SelectIndex(StatusTabIndex)
	}
}

// DayLink lays one invisible block over a calendar day's heading that opens the
// day on Status. label is the heading's text, and sets how wide the block is, so
// the rest of the row stays free; heading is what is drawn. hover, if set, hears
// the pointer on the block, for a day that has to stay lit under it.
//
// Under the pointer the heading's text fades to gold, so it reads as a link;
// today's, already gold at rest, fades to white instead.
func (ui *UI) DayLink(date string, label *widget.Label, heading fyne.CanvasObject, hover func(owner fyne.CanvasObject, on bool)) fyne.CanvasObject {
	link := NewTappable(canvas.NewRectangle(color.Transparent), func() { ui.OpenDayOnStatus(date) })
	// A heading already tinted (today's gold) is retinted in place: an outer
	// override would lose to the inner one.
	ink, ok := heading.(*container.ThemeOverride)
	if !ok {
		ink = container.NewThemeOverride(heading, theme.Current())
		heading = ink
	}
	rest := ink.Theme
	var lit color.Color = widgets.LogoGold
	if date == Today() {
		lit = color.White
	}
	var fade *fyne.Animation
	var level float32
	link.OnHover = func(on bool) {
		if fade != nil {
			fade.Stop()
			fade = nil
		}
		from, target := level, float32(0)
		if on {
			target = 1
		}
		paint := func(done float32) {
			level = from + (target-from)*done
			ink.Theme = rest
			if level > 0 {
				base := rest.Color(theme.ColorNameForeground, fyne.CurrentApp().Settings().ThemeVariant())
				ink.Theme = widgets.StatusHeaderTheme{Theme: rest, Ink: MixColor(base, lit, level)}
			}
			ink.Refresh()
		}
		if fyne.CurrentApp().Settings().ShowAnimations() {
			var anim *fyne.Animation
			anim = fyne.NewAnimation(widgets.HoverDuration, func(done float32) {
				if fade == anim {
					paint(done)
				}
			})
			anim.Curve = fyne.AnimationEaseOut
			fade = anim
			anim.Start()
		} else {
			paint(1)
		}
		if hover != nil {
			hover(link, on)
		}
	}
	width := fyne.MeasureText(label.Text, theme.TextSize(), label.TextStyle).Width + 2*theme.InnerPadding()
	return container.New(dayLinkLayout{width: width}, heading, link)
}

// dayLinkLayout gives the heading the whole row and holds the block over it to
// the text's width, or to the row if that is narrower. The block is a layer of
// its own so that its width can never cut the heading's text short.
type dayLinkLayout struct{ width float32 }

func (l dayLinkLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return objects[0].MinSize()
}

func (l dayLinkLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.Position{})
	objects[0].Resize(size)
	objects[1].Move(fyne.Position{})
	objects[1].Resize(fyne.NewSize(min(l.width, size.Width), size.Height))
}
