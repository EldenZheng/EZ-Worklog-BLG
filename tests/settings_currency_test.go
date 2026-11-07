package tests

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/report"
	"worklog/internal/menus/settings"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

type currencyForm struct {
	ui          *shell.UI
	view        fyne.CanvasObject
	cur, disp   *shell.GoldSelect
	show        *widget.Check
	save, fetch *widget.Button
}

func openCurrencySettings(t *testing.T, cfg backend.Config) currencyForm {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	test.ApplyTheme(t, theme.DefaultTheme())
	cfg.GithubAuthor = "sample-user"
	f := currencyForm{ui: &shell.UI{Store: backend.NewStore(tempDir(t)), Win: a.NewWindow("settings"), Cfg: cfg, HasCfg: true}}
	f.view = settings.BuildSettingsTab(f.ui)
	var selects []*shell.GoldSelect
	walkRendered(f.view, func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *shell.GoldSelect:
			if slices.Contains(v.Options, "USD") {
				selects = append(selects, v)
			}
		case *widget.Button:
			switch v.Text {
			case "Save settings":
				f.save = v
			case "Fetch exchange rate":
				f.fetch = v
			}
		case *widget.Check:
			if v.Text == "Show" {
				f.show = v
			}
		}
	})
	if len(selects) != 2 || f.save == nil || f.fetch == nil || f.show == nil {
		t.Fatalf("missing currency controls: %d dropdowns, save %v, fetch %v, show %v",
			len(selects), f.save != nil, f.fetch != nil, f.show != nil)
	}
	f.cur, f.disp = selects[0], selects[1]
	return f
}

func stubRate(t *testing.T, rate float64) *int {
	t.Helper()
	previous := backend.FetchRate
	t.Cleanup(func() { backend.FetchRate = previous })
	calls := 0
	backend.FetchRate = func(string, string) (float64, error) {
		calls++
		return rate, nil
	}
	return &calls
}

// Both fields are dropdowns with the saved values picked. RM shows as its code
// and is saved back as RM, so the Report does not change until another is picked.
func TestCurrencyDropdownsKeepTheSavedLabel(t *testing.T) {
	calls := stubRate(t, 3.8)
	f := openCurrencySettings(t, backend.Config{Currency: "RM", DisplayCurrency: "IDR", FxRate: 3.8, FxUpdated: "2026-11-06"})
	if f.cur.Selected != "MYR" || f.disp.Selected != "IDR" || !f.show.Checked {
		t.Fatalf("opened on %q, %q, ticked %v", f.cur.Selected, f.disp.Selected, f.show.Checked)
	}
	f.save.OnTapped()
	got, _ := f.ui.Store.LoadConfig()
	if got.Currency != "RM" || got.DisplayCurrency != "IDR" || got.FxRate != 3.8 || *calls != 0 {
		t.Fatalf("saved %+v after %d fetches", got, *calls)
	}
}

// A saved code the list lacks stays on offer instead of being lost on save.
func TestCurrencyDropdownKeepsAnUnlistedCode(t *testing.T) {
	stubRate(t, 1)
	f := openCurrencySettings(t, backend.Config{Currency: "ABC", DisplayCurrency: "USD"})
	if f.cur.Selected != "ABC" || slices.Contains(settings.CurrencyCodes, "ABC") {
		t.Fatalf("selected %q", f.cur.Selected)
	}
}

// A rate belongs to a pair of currencies: switching either refetches it, rather
// than printing the old pair's figure beside the new code.
func TestChangingACurrencyFetchesItsRate(t *testing.T) {
	calls := stubRate(t, 0.21)
	f := openCurrencySettings(t, backend.Config{Currency: "RM", DisplayCurrency: "IDR", FxRate: 3800, FxUpdated: "2026-11-06"})
	f.disp.SetSelected("USD")
	f.save.OnTapped()
	if *calls != 1 || f.ui.Cfg.DisplayCurrency != "USD" || f.ui.Cfg.FxRate != 0.21 {
		t.Fatalf("after %d fetches: %+v", *calls, f.ui.Cfg)
	}
}

// Unticking Display in saves the choice, keeps the currency for later, and
// takes the rate controls away. No rate is fetched for a hidden conversion.
func TestUntickingDisplayInHidesTheConversion(t *testing.T) {
	calls := stubRate(t, 1)
	f := openCurrencySettings(t, backend.Config{Currency: "RM", DisplayCurrency: "IDR", FxRate: 3.8, FxUpdated: "2026-11-06"})
	if !f.fetch.Visible() || f.disp.Disabled() {
		t.Fatal("ticked, the rate controls and dropdown should be live")
	}
	f.show.SetChecked(false)
	if !f.disp.Disabled() {
		t.Fatal("unticked, the dropdown should be disabled")
	}
	if f.fetch.Visible() {
		t.Fatal("Fetch exchange rate should go as soon as it is unticked, before any save")
	}
	f.save.OnTapped()
	got, _ := f.ui.Store.LoadConfig()
	if !got.HideConversion || got.DisplayCurrency != "IDR" || *calls != 0 || f.fetch.Visible() {
		t.Fatalf("saved %+v, %d fetches, fetch visible %v", got, *calls, f.fetch.Visible())
	}
	f.ui.RefreshRate(true)
	if *calls != 0 {
		t.Fatal("a hidden conversion should not fetch a rate")
	}

	// Ticking it again brings the controls back.
	f.show.SetChecked(true)
	if !f.fetch.Visible() || f.disp.Disabled() {
		t.Fatal("ticked again, the rate controls and dropdown should be back")
	}
	f.save.OnTapped()
	if f.ui.Cfg.HideConversion || !f.fetch.Visible() {
		t.Fatalf("hide %v, fetch visible %v", f.ui.Cfg.HideConversion, f.fetch.Visible())
	}
}

// A config from before the option has no such key and keeps its conversion. A
// blank Display in already meant no conversion, so it opens unticked.
func TestDisplayInOpensFromTheSavedConfig(t *testing.T) {
	stubRate(t, 1)
	if f := openCurrencySettings(t, backend.Config{Currency: "RM", DisplayCurrency: "USD"}); !f.show.Checked {
		t.Fatal("older config should stay ticked")
	}
	if f := openCurrencySettings(t, backend.Config{Currency: "RM"}); f.show.Checked || !f.disp.Disabled() {
		t.Fatal("blank Display in should open unticked")
	}
}

func choiceTexts(s *shell.GoldSelect) []string {
	var texts []string
	walkRendered(s.Rows, func(o fyne.CanvasObject) {
		if label, ok := o.(*widget.Label); ok {
			texts = append(texts, label.Text)
		}
	})
	return texts
}

// The popup opens with its search field focused. Typing narrows the rows, in
// any case, and the keys work the narrowed list.
func TestCurrencyDropdownSearches(t *testing.T) {
	stubRate(t, 1)
	f := openCurrencySettings(t, backend.Config{Currency: "RM", DisplayCurrency: "USD", FxRate: 1, FxUpdated: "2026-11-06"})
	f.ui.Win.SetContent(f.view)
	f.ui.Win.Resize(fyne.NewSize(1400, 1000))
	press := func(name fyne.KeyName) { f.disp.Find.TypedKey(&fyne.KeyEvent{Name: name}) }

	f.disp.Tapped(nil)
	if f.disp.Find == nil {
		t.Fatal("popup has no search field")
	}
	if f.ui.Win.Canvas().Focused() != fyne.Focusable(f.disp.Find) {
		t.Fatalf("search field not focused: %T", f.ui.Win.Canvas().Focused())
	}
	if got := choiceTexts(f.disp); len(got) != len(settings.CurrencyCodes) {
		t.Fatalf("unfiltered popup lists %d of %d", len(got), len(settings.CurrencyCodes))
	}

	f.disp.Find.SetText("sd")
	if got := choiceTexts(f.disp); !slices.Equal(got, []string{"BSD", "RSD", "SDG", "USD"}) {
		t.Fatalf("search for sd lists %v", got)
	}
	press(fyne.KeyDown)
	press(fyne.KeyReturn)
	if f.disp.Selected != "RSD" || f.disp.Popup != nil {
		t.Fatalf("selected %q, popup open %v", f.disp.Selected, f.disp.Popup != nil)
	}

	// Nothing to pick: Enter does nothing, and Escape leaves the choice alone.
	f.disp.Tapped(nil)
	f.disp.Find.SetText("zzz")
	if got := choiceTexts(f.disp); !slices.Equal(got, []string{"No matches"}) {
		t.Fatalf("empty search lists %v", got)
	}
	press(fyne.KeyReturn)
	if f.disp.Selected != "RSD" {
		t.Fatalf("Enter on no matches picked %q", f.disp.Selected)
	}
	press(fyne.KeyEscape)
	if f.disp.Popup != nil || f.disp.Selected != "RSD" {
		t.Fatal("Escape should close the popup and keep the choice")
	}

	// Tapping a narrowed row picks that row, not the one at its old place.
	f.disp.Tapped(nil)
	f.disp.Find.SetText("eur")
	test.Tap(f.disp.Rows.Objects[0].(fyne.Tappable))
	if f.disp.Selected != "EUR" {
		t.Fatalf("tapped row picked %q", f.disp.Selected)
	}

	// Reopening starts over with the whole list.
	f.disp.Tapped(nil)
	if got := choiceTexts(f.disp); len(got) != len(settings.CurrencyCodes) || f.disp.Find.Text != "" {
		t.Fatalf("reopened on %q with %d rows", f.disp.Find.Text, len(got))
	}

	// Clearing the search brings the whole list back and the focus to the field.
	f.disp.Find.SetText("eur")
	f.ui.Win.Canvas().Unfocus()
	f.disp.Find.SetText("")
	if got := choiceTexts(f.disp); len(got) != len(settings.CurrencyCodes) {
		t.Fatalf("cleared search lists %d rows", len(got))
	}
	if f.ui.Win.Canvas().Focused() != fyne.Focusable(f.disp.Find) {
		t.Fatalf("clearing left the focus on %T", f.ui.Win.Canvas().Focused())
	}
	f.disp.CloseChoices()
}

// A disabled dropdown stays shut.
func TestUntickedCurrencyDropdownDoesNotOpen(t *testing.T) {
	stubRate(t, 1)
	f := openCurrencySettings(t, backend.Config{Currency: "RM", DisplayCurrency: "USD", HideConversion: true})
	f.ui.Win.SetContent(f.view)
	f.disp.Tapped(nil)
	if f.disp.Popup != nil {
		t.Fatal("disabled dropdown opened")
	}
}

// The sidebar's line is grey and the main panel's lines are gold.
func TestSettingsLineColours(t *testing.T) {
	stubRate(t, 1)
	f := openCurrencySettings(t, backend.Config{Currency: "RM", DisplayCurrency: "USD"})
	grey := theme.Color(theme.ColorNamePlaceHolder)
	greys, golds := 0, 0
	walkRendered(f.view, func(o fyne.CanvasObject) {
		sep, ok := o.(*widget.Separator)
		if !ok {
			return
		}
		for _, part := range test.WidgetRenderer(sep).Objects() {
			rect, ok := part.(*canvas.Rectangle)
			if !ok {
				continue
			}
			var fill color.Color = rect.FillColor
			switch {
			case sameColor(fill, grey):
				greys++
			case sameColor(fill, widgets.LogoGold):
				golds++
			default:
				t.Fatalf("a line in neither colour: %v", fill)
			}
		}
	})
	if greys != 1 || golds < 3 {
		t.Fatalf("greys = %d, golds = %d", greys, golds)
	}
}

// Hidden, no converted figure is left anywhere on the Report tab.
func TestReportHidesTheConversion(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, HasCfg: true, RepMonth: "2026-08",
		Cfg: backend.Config{Repos: []string{"exampleorg"}, WorklogOwner: "sample-owner",
			BaseSalary: 2793, Currency: "RM", DisplayCurrency: "IDR", FxRate: 3.8, FxUpdated: "2026-08-20"}}
	w.SetContent(report.BuildReportTab(ui))
	key := shell.ReportCacheKey("2026-08")
	ui.EnsureProjectCache()
	ui.ProjItems[key] = []backend.WorklogItem{{Date: "2026-08-11", Minutes: 480, Owner: "sample-owner",
		URL: "https://github.com/exampleorg/work/issues/1"}}
	ui.ProjLoaded[key] = true

	shown := func() []string {
		report.DrawReport(ui)
		var found []string
		check := func(o fyne.CanvasObject) {
			for _, text := range labels(o) {
				if strings.Contains(text, "IDR") || strings.Contains(text, "Converted") {
					found = append(found, text)
				}
			}
		}
		check(ui.RepBox)
		walk(ui.RepBox, func(o fyne.CanvasObject) {
			if s, ok := o.(*container.Scroll); ok {
				check(s.Content)
			}
		})
		return found
	}
	if len(shown()) == 0 {
		t.Fatal("the conversion should show by default")
	}
	ui.Cfg.HideConversion = true
	if got := shown(); len(got) != 0 {
		t.Fatalf("hidden conversion still on the tab: %q", got)
	}
}
