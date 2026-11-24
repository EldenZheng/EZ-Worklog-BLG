package shell

import (
	"image/png"
	"os"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

type dropdownTestDriver struct {
	fyne.Driver
	anims []*fyne.Animation
}

func (d *dropdownTestDriver) StartAnimation(a *fyne.Animation) { d.anims = append(d.anims, a) }
func (*dropdownTestDriver) StopAnimation(*fyne.Animation)      {}

type dropdownTestApp struct {
	fyne.App
	driver fyne.Driver
}

func (a *dropdownTestApp) Driver() fyne.Driver { return a.driver }

func TestDropdownReferenceLookAndMenuSelectionMotion(t *testing.T) {
	base := test.NewApp()
	base.Settings().SetTheme(GoldTheme{Theme: theme.DefaultTheme()})
	d := &dropdownTestDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&dropdownTestApp{App: base, driver: d})
	defer func() { FinishDropdownClosings(); fyne.SetCurrentApp(base); base.Quit() }()
	chosen := ""
	s := NewGoldSelect([]string{"Reported", "Approved", "Another option"}, nil)
	s.StayOpen = false // the closing path, with its retiring menu
	s.SetSelected("Reported")
	s.OnChanged = func(value string) { chosen = value }
	w := base.NewWindow("dropdown reference")
	w.SetContent(container.NewVBox(s))
	w.Resize(fyne.NewSize(420, 300))
	r := test.WidgetRenderer(s).(*goldSelectRenderer)
	if r.bg.CornerRadius != dropdownCorner || r.bg.StrokeWidth <= 0 || r.bg.StrokeColor != widgets.WithAlpha(widgets.LogoGold, .65) {
		t.Fatal("closed dropdown must have the reference's rounded gold outline")
	}
	s.MouseIn(nil)
	if r.hover.Animation == nil || r.hover.Animation.Duration != widgets.HoverDuration {
		t.Fatal("field hover must use shared gold timing")
	}
	r.hover.Animation.Tick(1)
	s.Tapped(nil)
	d.anims[len(d.anims)-1].Tick(1)
	oldPopup := s.Popup
	full := oldPopup.Size()
	first, next := s.choices[0], s.choices[1]
	if first.mark.share != 1 || !first.mark.short.Visible() || !first.mark.long.Visible() || next.mark.short.Visible() {
		t.Fatal("only the selected row should show the bare right-hand check")
	}
	// Optional off-screen render for checking the common dropdown appearance.
	if path := os.Getenv("WORKLOG_DROPDOWN_RENDER"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, w.Canvas().Capture())
		closeErr := f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	click := fyne.NewPos(12, next.row.Size().Height/2)
	test.TapCanvas(w.Canvas(), d.AbsolutePositionForObject(next.row).Add(click))
	if chosen != "Approved" || s.Selected != "Approved" || s.Popup != nil {
		t.Fatal("choice action must run immediately before its visual confirmation finishes")
	}
	first.row.Tapped(nil)
	if chosen != "Approved" || s.Selected != "Approved" {
		t.Fatal("a retiring menu accepted another action during its confirmation")
	}
	if next.look.anim == nil || first.look.anim == nil || next.look.centre != click || !next.look.hasCentre {
		t.Fatal("selection must spread from the click and drain the previous gold row")
	}
	if s.choices[2].look.anim != nil {
		t.Fatal("unchanged rows must not start background selection animations")
	}
	closing := d.anims[len(d.anims)-1]
	closing.Tick(.25)
	if !oldPopup.Visible() || oldPopup.Size().Height >= full.Height || closing.Duration != dropTime {
		t.Fatal("a selection must start closing immediately, without a confirmation pause")
	}
	first.look.anim.Tick(1)
	next.look.anim.Tick(1)
	if first.mark.short.Visible() || first.mark.long.Visible() || !next.mark.short.Visible() || !next.mark.long.Visible() || next.look.fill.Size() != next.row.Size() {
		t.Fatal("menu ripple did not settle on the new gold row and check")
	}
	closing.Tick(1)
	if oldPopup.Visible() {
		t.Fatal("confirmed selection did not finish closing")
	}
	s.Disable()
	before := len(d.anims)
	s.MouseIn(nil)
	s.Tapped(nil)
	if len(d.anims) != before || s.Popup != nil {
		t.Fatal("disabled dropdown must not animate or open")
	}
	s.Enable()
	if !r.hover.Rect.Visible() {
		t.Fatal("reenabling the field lost its hover surface")
	}
}

func TestWorklogDropdownStaysOpenAndClosesOnExplicitDismissal(t *testing.T) {
	base := test.NewApp()
	d := &dropdownTestDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&dropdownTestApp{App: base, driver: d})
	defer func() { FinishDropdownClosings(); fyne.SetCurrentApp(base); base.Quit() }()
	approved := false
	s := NewWorklogCount(false, func(next bool) { approved = next })
	w := base.NewWindow("Worklog")
	w.SetContent(container.NewVBox(s))
	w.Resize(fyne.NewSize(420, 300))
	open := func() *widget.PopUp {
		s.Tapped(nil)
		d.anims[len(d.anims)-1].Tick(1)
		return s.Popup
	}
	popup := open()
	row := s.choices[1].row
	test.TapCanvas(w.Canvas(), d.AbsolutePositionForObject(row).Add(fyne.NewPos(20, row.Size().Height/2)))
	if !approved || s.Popup != popup || !popup.Visible() || s.active != 1 {
		t.Fatal("Worklog must update its value and keep the same menu open")
	}
	for _, choice := range s.choices {
		if choice.look.anim != nil {
			choice.look.anim.Tick(1)
		}
	}
	if s.choices[0].mark.short.Visible() || !s.choices[1].mark.long.Visible() {
		t.Fatal("the persistent menu lost its animated selection check")
	}
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyUp})
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if approved || s.Popup != popup {
		t.Fatal("keyboard selection must also keep Worklog open")
	}
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	d.anims[len(d.anims)-1].Tick(1)
	if s.Popup != nil || popup.Visible() {
		t.Fatal("Escape did not close Worklog")
	}
	popup = open()
	w.Canvas().Overlays().Top().(fyne.Tappable).Tapped(nil)
	d.anims[len(d.anims)-1].Tick(1)
	if s.Popup != nil || popup.Visible() {
		t.Fatal("outside click did not close Worklog")
	}
	popup = open()
	s.Tapped(nil)
	d.anims[len(d.anims)-1].Tick(1)
	if s.Popup != nil || popup.Visible() {
		t.Fatal("clicking the Worklog field again did not close it")
	}
}
