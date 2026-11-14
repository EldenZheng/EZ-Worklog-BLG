package settings

import (
	_ "embed"
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// ============================ Settings ============================

func BuildSettingsTab(ui *shell.UI) fyne.CanvasObject {
	toast := newSettingsToast()
	var redrawBubbles func()
	user := widget.NewEntry()
	user.SetPlaceHolder("auto-detected from gh — leave blank")
	owner := widget.NewEntry()
	owner.SetPlaceHolder("team-alex")
	// The name a meeting issue is titled with. Left blank it comes off the
	// worklog owner, which is right for a handle that is a name.
	dispName := widget.NewEntry()
	repos := widget.NewMultiLineEntry()
	repos.SetMinRowsVisible(3)
	repos.SetPlaceHolder("exampleorg  (whole org)\nexampleorg/repository\none per line, or comma-separated")
	sal := widget.NewEntry()
	supportPay := widget.NewEntry()
	meetingDest := widget.NewEntry()
	meetingDest.SetPlaceHolder("owner/repository")
	reviewDest := widget.NewEntry()
	reviewDest.SetPlaceHolder("owner/repository")
	boardViews := widget.NewMultiLineEntry()
	boardViews.SetMinRowsVisible(2)
	boardViews.SetPlaceHolder("https://github.com/orgs/exampleorg/projects/1/views/1")
	kanbanFields, readKanbanSources := backend.KanbanSourceEditor(backend.KanbanSources(ui.Cfg))
	savedCurrency := backend.OrDefault(ui.Cfg.Currency, "RM")
	cur := currencyPicker(savedCurrency)
	disp := currencyPicker(backend.OrDefault(ui.Cfg.DisplayCurrency, "USD"))
	// Unticked, the Report drops its converted figures. A blank currency saved
	// earlier meant the same thing, so it opens unticked.
	showDisp := widget.NewCheck("Show", nil)
	showDisp.SetChecked(!ui.Cfg.HideConversion && strings.TrimSpace(ui.Cfg.DisplayCurrency) != "")
	if !showDisp.Checked {
		disp.Disable()
	}
	back := widget.NewEntry()
	cleanup := widget.NewEntry()
	cleanup.SetPlaceHolder("Blank keeps everything")
	mode := shell.NewGoldSelect([]string{"Create Worklog sub-issue", "Write on the issue itself"}, nil)
	// Any number of months, typed in, rather than a few fixed choices.
	reportRange := widget.NewEntry()
	reportRange.SetPlaceHolder("1")
	key := widget.NewPasswordEntry()
	key.SetPlaceHolder("sk-ant-...")
	// One board per org, one per line. Status and Report read the union of
	// them, so work logged against a second org's project still counts.
	projURL := widget.NewMultiLineEntry()
	projURL.SetMinRowsVisible(3)
	projURL.SetPlaceHolder("https://github.com/orgs/exampleorg/projects/9")
	msg := widget.NewLabel("")

	// preload
	c := ui.Cfg
	user.SetText(c.GithubAuthor)
	if strings.TrimSpace(c.GithubAuthor) == "" {
		// Show who gh is logged in as, so the field can stay blank.
		//
		// A placeholder is not worth a crash: gh takes a moment, and if the app
		// is closed inside that moment the answer comes back to a window that is
		// gone. fyne.Do panics there rather than no-opping, so this catches it
		// and drops the hint — which is all that is lost.
		go func() {
			defer func() { _ = recover() }()
			if u, err := backend.GhCurrentUser(); err == nil {
				fyne.Do(func() {
					user.SetPlaceHolder("gh user: " + u + " — leave blank to use it")
					user.Refresh()
				})
			}
		}()
	}
	owner.SetText(c.WorklogOwner)
	dispName.SetText(c.DisplayName)
	dispName.SetPlaceHolder(backend.OrDefault(backend.DisplayName(c), "Your name") + "  — from your worklog owner")
	repos.SetText(strings.Join(c.Repos, "\n"))
	// Left blank when unset rather than pre-filled with a number: a figure
	// already in the box is a figure that gets saved without being read, and
	// nobody else's salary is a sensible starting guess for yours.
	if c.BaseSalary != 0 {
		sal.SetText(strconv.FormatFloat(c.BaseSalary, 'f', -1, 64))
	}
	if c.SupportSetPay != 0 {
		supportPay.SetText(strconv.FormatFloat(c.SupportSetPay, 'f', -1, 64))
	}
	meetingDest.SetText(c.MeetingRepo)
	reviewDest.SetText(c.BulkCodeReviewRepo)
	boardViews.SetText(strings.Join(c.BoardViewURLs, "\n"))
	if c.LookbackDays > 0 {
		back.SetText(strconv.Itoa(c.LookbackDays))
	} else {
		back.SetText("7")
	}
	if c.CleanupDays > 0 {
		cleanup.SetText(strconv.Itoa(c.CleanupDays))
	}
	if c.DefaultMode == "issue" {
		mode.SetSelected("Write on the issue itself")
	} else {
		mode.SetSelected("Create Worklog sub-issue")
	}
	key.SetText(c.AnthropicAPIKey)
	reportRange.SetText(strconv.Itoa(backend.FinishedReportMonths(c)))
	projURL.SetText(strings.Join(backend.ProjectURLs(c), "\n"))
	if !ui.HasCfg {
		msg.SetText("Fill this in once to get started.")
	}

	// Live readout of the last fetched rate — sits next to the buttons so the
	// answer to "did the fetch land" is visible without hunting for the msg
	// label at the bottom. Rendered in gold so it reads as the same accent as
	// the Converted receivable figure on the Report tab.
	rateReadout := canvas.NewText("", widgets.LogoGold)
	rateReadout.TextSize = theme.TextSize()
	rateReadout.TextStyle = fyne.TextStyle{Bold: true}
	// With the conversion unticked there is no rate to read or fetch. This follows
	// the tick rather than the saved setting, so they go as soon as it is unticked.
	var rate *widget.Button
	var actions *fyne.Container
	refreshRateReadout := func() {
		switch {
		case !showDisp.Checked:
			rateReadout.Text = ""
		case ui.Cfg.FxRate > 0:
			rateReadout.Text = fmt.Sprintf("1 %s = %s %s (%s)",
				backend.OrDefault(ui.Cfg.Currency, "RM"), shell.CommaAmount(ui.Cfg.FxRate),
				backend.OrDefault(ui.Cfg.DisplayCurrency, "USD"), ui.Cfg.FxUpdated)
		default:
			rateReadout.Text = "Exchange rate not set — press Fetch exchange rate."
		}
		rateReadout.Refresh()
		shell.ShowIf(rate, showDisp.Checked)
		actions.Refresh()
	}
	showDisp.OnChanged = func(on bool) {
		if on {
			disp.Enable()
		} else {
			disp.Disable()
			msg.SetText("") // it may still be saying what the last fetch found
		}
		refreshRateReadout()
	}

	save := widget.NewButton("Save settings", func() {
		salF, err := backend.ParsePaySetting(sal.Text)
		if err != nil {
			msg.SetText("Base salary: " + err.Error())
			return
		}
		supportF, err := backend.ParsePaySetting(supportPay.Text)
		if err != nil {
			msg.SetText("Support payment: " + err.Error())
			return
		}
		for _, field := range []struct{ name, value string }{
			{"Meeting repository", meetingDest.Text}, {"Bulk review repository", reviewDest.Text},
		} {
			if strings.TrimSpace(field.value) != "" && !backend.ValidRepo(field.value) {
				msg.SetText(field.name + ": enter owner/repo, or leave blank.")
				return
			}
		}
		views := backend.ConfiguredLines(boardViews.Text)
		kanbanBoards, kanbanErr := readKanbanSources()
		if kanbanErr != nil {
			msg.SetText(kanbanErr.Error())
			return
		}
		for _, view := range views {
			if _, err := backend.ParseProjectRef(view); err != nil {
				msg.SetText("Board view URLs: enter a GitHub project or saved view URL on each line.")
				return
			}
		}
		backN, _ := strconv.Atoi(strings.TrimSpace(back.Text))
		if backN < 1 {
			backN = 7
		}
		cleanupN := 0
		if text := strings.TrimSpace(cleanup.Text); text != "" {
			n, err := strconv.Atoi(text)
			if err != nil || n < 1 {
				msg.SetText("Clean up after: enter a number of days, or leave it blank to keep everything.")
				return
			}
			cleanupN = n
		}
		reportN := 0
		if text := strings.TrimSpace(reportRange.Text); text != "" {
			n, err := strconv.Atoi(text)
			if err != nil || n < 1 || n > backend.MaxReportMonths {
				msg.SetText(fmt.Sprintf("Finished report range: enter a number of months from 1 to %d.", backend.MaxReportMonths))
				return
			}
			reportN = n
		}
		m := "subissue"
		if mode.Selected == "Write on the issue itself" {
			m = "issue"
		}
		var repoList []string
		for _, x := range strings.FieldsFunc(repos.Text, func(r rune) bool {
			return r == '\n' || r == '\r' || r == ','
		}) {
			if s := strings.TrimSpace(x); s != "" {
				repoList = append(repoList, s)
			}
		}
		newCfg := ui.Cfg
		newCfg.GithubAuthor = strings.TrimSpace(user.Text)
		newCfg.WorklogOwner = strings.TrimSpace(owner.Text)
		newCfg.DisplayName = strings.TrimSpace(dispName.Text)
		newCfg.Repos = repoList
		newCfg.BaseSalary = salF
		newCfg.SupportSetPay = supportF
		newCfg.MeetingRepo = strings.TrimSpace(meetingDest.Text)
		newCfg.BulkCodeReviewRepo = strings.TrimSpace(reviewDest.Text)
		newCfg.BoardViewURLs = views
		newCfg.KanbanBoards = kanbanBoards
		// The dropdown says MYR where the saved label may say RM. Leaving a label
		// alone unless another currency is picked keeps the Report reading as it did.
		newCfg.Currency = cur.Selected
		if cur.Selected == backend.CurrencyCode(savedCurrency) {
			newCfg.Currency = savedCurrency
		}
		newCfg.DisplayCurrency = disp.Selected
		newCfg.HideConversion = !showDisp.Checked
		// A rate belongs to one pair of currencies. Kept after a switch, it would
		// print the old pair's figure beside the new code as if it were converted.
		pairChanged := backend.CurrencyCode(newCfg.Currency) != backend.CurrencyCode(ui.Cfg.Currency) ||
			newCfg.DisplayCurrency != backend.CurrencyCode(ui.Cfg.DisplayCurrency)
		if pairChanged {
			newCfg.FxRate, newCfg.FxUpdated = 0, ""
		}
		newCfg.LookbackDays = backN
		newCfg.CleanupDays = cleanupN
		newCfg.DefaultMode = m
		// One month is the default, saved as nothing.
		newCfg.ReportMonths = 0
		if reportN > 1 {
			newCfg.ReportMonths = reportN
		}
		newCfg.AnthropicAPIKey = strings.TrimSpace(key.Text)
		// First line stays the main board so an older build of this app, and
		// anything reading project_url, still finds the one it expects.
		var projList []string
		for _, line := range strings.FieldsFunc(projURL.Text, func(r rune) bool {
			return r == '\n' || r == '\r' || r == ',' || r == ' '
		}) {
			if line = strings.TrimSpace(line); line != "" {
				projList = append(projList, line)
			}
		}
		newCfg.ProjectURL, newCfg.ExtraProjectURLs = "", nil
		if len(projList) > 0 {
			newCfg.ProjectURL, newCfg.ExtraProjectURLs = projList[0], projList[1:]
		}
		if newCfg.AnthropicModel == "" {
			newCfg.AnthropicModel = "claude-sonnet-4-6"
		}
		if err := ui.Store.SaveConfig(newCfg); err != nil {
			ui.Errf(err)
			return
		}
		ui.Cfg = newCfg
		if ui.Kanban != nil {
			shell.Hooks.KanbanUISourcesChanged(ui.Kanban)
		}
		ui.HasCfg = true
		ui.ProjItems = nil // reload against the saved project and owner
		ui.EnsureProjectCache()
		refreshRateReadout()
		msg.SetText("")
		if redrawBubbles != nil {
			redrawBubbles()
		}
		toast.show("Settings saved")
		if pairChanged && !newCfg.HideConversion {
			rate.OnTapped()
		}
	})

	rate = widget.NewButton("Fetch exchange rate", func() {
		if !ui.HasCfg {
			msg.SetText("Save settings first.")
			return
		}
		msg.SetText("Fetching rate…")
		_, finish := ui.BeginFetch("settings-rate", "Fetching exchange rate")
		base := backend.CurrencyCode(ui.Cfg.Currency)
		disp := backend.OrDefault(ui.Cfg.DisplayCurrency, "USD")
		var r float64
		var fetchErr error
		ui.Async(func() error {
			v, err := backend.FetchRate(base, disp)
			r = v
			fetchErr = err
			return nil
		}, func() {
			defer finish()
			if fetchErr != nil {
				ui.Errf(fetchErr)
				return
			}
			ui.Cfg.FxRate = r
			ui.Cfg.FxUpdated = shell.Today()
			_ = ui.Store.SaveConfig(ui.Cfg)
			refreshRateReadout()
			msg.SetText(fmt.Sprintf("1 %s = %s %s (%s)", ui.Cfg.Currency, shell.CommaAmount(r), disp, ui.Cfg.FxUpdated))
		})
	})

	ui.SettingsProgress = shell.NewLoadingIndicator()
	profileNote := widget.NewLabel("GitHub profile loads when Settings opens.")
	profileNote.Wrapping = fyne.TextWrapWord
	ui.ProfileBox = container.NewVBox(profileNote)

	// Header row: Worklog / Settings on the left, action buttons on the right
	// stacked over the exchange rate readout. Border pins the buttons to the
	// top-right so they render at their own natural height rather than
	// stretching to match the tall logo block.
	actionRow := container.NewHBox(layout.NewSpacer(), rate, save)
	actions = container.NewVBox(actionRow, container.NewHBox(layout.NewSpacer(), rateReadout))
	refreshRateReadout()
	header := container.NewBorder(nil, nil, nil, actions, settingsHeading())

	// Only the GitHub profile card carries a bubble in the sidebar, and it is
	// tinted gold to match the primary accent. The identity text fields sit
	// beneath the bubble on plain background — the profile card is what needs
	// visual weight, the fields do not.
	sidebarContent := container.NewVBox(
		shell.Bold("Identity"),
		goldPanel(ui.ProfileBox),
		sidebarField("GitHub username (optional — auto from gh)", user),
		sidebarField("Worklog owner", owner),
		sidebarField("Name on meeting issues (optional)", dispName),
	)

	// Color chips share the app's org mapping. Their three-dot controls open
	// the picker; clicking the chip never changes its color accidentally.
	bubbleBox := container.NewVBox()
	redrawBubbles = func() {
		bubbleBox.Objects = nil
		var entries []string
		for _, x := range strings.FieldsFunc(repos.Text, func(r rune) bool {
			return r == '\n' || r == '\r' || r == ','
		}) {
			if s := strings.TrimSpace(x); s != "" {
				entries = append(entries, s)
			}
		}
		tmp := ui.Cfg
		tmp.Repos = entries
		tmp.ExtraProjectURLs = append([]string{}, tmp.ExtraProjectURLs...)
		tmp.ExtraProjectURLs = append(tmp.ExtraProjectURLs, backend.ConfiguredLines(projURL.Text)...)
		if sources, err := readKanbanSources(); err == nil {
			for _, source := range sources {
				tmp.ExtraProjectURLs = append(tmp.ExtraProjectURLs, source.URL)
			}
		}
		orgs := shell.OrgOrder(tmp)
		if len(orgs) == 0 {
			bubbleBox.Add(widget.NewLabelWithStyle(
				"Add a repo or org above; each one will show a colour chip here.",
				fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
		}
		for _, org := range orgs {
			org := org
			bubbleBox.Add(settingsOrgChip(org, shell.OrgColor(tmp, org), func() {
				OrgColorPicker(ui, org, shell.OrgColor(tmp, org), func() { redrawBubbles(); toast.show("Color saved") })
			}))
		}
		bubbleBox.Refresh()
	}
	repos.OnChanged = func(string) { redrawBubbles() }
	projURL.OnChanged = func(string) { redrawBubbles() }
	redrawBubbles()

	// Section-based layout: each section has a gold heading, a divider under
	// it, and its fields below. Spacers between sections distribute vertical
	// slack so the panel fills the full column height rather than stacking at
	// the top with a lot of dead space underneath.
	//
	// Sources merges the repo/org list with the project board URLs. Both are
	// read from the same set of orgs, so one bubble legend on the left names
	// each colour once. The two textareas stack on the right: repos on top,
	// project boards on the bottom.
	fieldsCol := container.NewVBox(
		shell.LabeledHint("Repos and orgs", "One per line, or comma-separated. A bare org name scans every repo it owns.", repos),
		shell.LabeledHint("Project board URLs", "One per line. Status and Report read every board on the list, filtered to the worklog owner above.", projURL),
		shell.LabeledHint("Board view URLs", "Optional saved views for weekly links. Blank uses the project boards above.", boardViews),
		shell.LabeledHint("Kanban boards and filters", "Project URLs are read directly; shared view changes do not affect these filters. Applies to Kanban and Sprint Calendar.", kanbanFields),
		container.NewGridWithColumns(2,
			shell.LabeledHint("Meeting repository", "Destination for daily meeting issues (owner/repo).", meetingDest),
			shell.LabeledHint("Bulk review repository", "Destination for daily bulk review issues (owner/repo).", reviewDest),
		),
	)
	legendCol := container.NewVBox(
		widget.NewLabelWithStyle("Orgs", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		bubbleBox,
	)
	sourcesSection := container.NewVBox(
		settingsTitle("Sources"),
		container.New(widgets.NewFluidRow(0.72, 0.28), fieldsCol, legendCol),
	)
	salarySection := container.NewVBox(
		settingsTitle("Salary"),
		container.NewGridWithColumns(3,
			shell.LabeledHint("Base salary", "Paid per 21 working days.", sal),
			shell.LabeledHint("Currency", "Currency the salary is paid in.", outlined(cur)),
			shell.LabeledHint("Display in", "Also show pay in this currency. Untick to hide the conversion.",
				container.NewBorder(nil, nil, shell.GoldChoice(showDisp), nil, outlined(disp))),
		),
		shell.LabeledHint("Support payment per set", "Paid per seven qualifying support issues, in the salary currency. Blank means unset.", supportPay),
	)
	worklogSection := container.NewVBox(
		settingsTitle("Worklog"),
		container.NewGridWithColumns(3,
			shell.LabeledHint("Look back", "Days back the commit sweep scans for unlogged work.", back),
			shell.LabeledHint("Default push mode", "Sub-issue creates a linked child. Issue writes on the parent itself.", outlinedSelect(mode)),
			shell.LabeledHint("Anthropic API key", "Enables the AI Compact-remarks button. Leave blank to disable.", key),
		),
		shell.LabeledHint("Clean up after (days)", "Forgets ignored commits, and the commit list on pushed entries, after this many days, or the look back if longer. The entries themselves stay. Blank keeps everything.", cleanup),
	)

	projectsSection := container.NewVBox(
		settingsTitle("Projects"),
		container.NewGridWithColumns(3,
			shell.LabeledHint("Finished report range (months)", "How many months the finished report shows at a time. Its month opens every week since the start.", reportRange),
		),
	)

	mainInner := container.NewVBox(
		sourcesSection,
		layout.NewSpacer(),
		salarySection,
		worklogSection,
		projectsSection,
		msg,
	)
	// Chunky theme bumps input padding so entries and the dropdown read as
	// larger controls — filling more of the panel — without needing a per-widget
	// wrapper on each one.
	// The panel stays on screen; only the fields scroll inside it.
	mainBubble, _ := shell.ScrollPanel(container.NewThemeOverride(mainInner,
		chunkySettingsTheme{Theme: theme.Current()}))

	// Both columns are wrapped in Stack so their contents render at natural
	// height while the column itself stretches to whatever height ratioRow
	// hands it. The padding around each column adds a real gap between the
	// two cards on top of ratioRow's own inter-column spacing.
	// The update panel stands at the foot of the sidebar, under the identity
	// fields, wherever the window's height leaves the foot.
	sidebar := container.NewBorder(nil, updatePanel(ui), nil, nil, sidebarContent)
	ui.Update.Frame = sidebar
	body := container.New(widgets.NewFluidRow(0.2, 0.8),
		container.NewPadded(sidebar),
		container.NewPadded(mainBubble),
	)
	top := container.NewVBox(header, ui.SettingsProgress.View)
	// Border pins the header on top and hands every remaining pixel to the
	// body — the Settings tab now fills to the bottom of the window instead
	// of hugging its natural content height.
	return container.NewStack(container.NewBorder(top, nil, nil, nil, body), toast.layer)
}

// settingsTitle is a section's heading with room above it, so each section
// reads as starting fresh rather than running on from the last one's fields.
func settingsTitle(title string) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(theme.Padding()*4, 0, 0, 0), shell.SectionTitle(title))
}

// sidebarField is a field whose label wraps, so the longest label does not set
// how narrow the sidebar can get.
func sidebarField(label string, field fyne.CanvasObject) fyne.CanvasObject {
	caption := widget.NewLabel(label)
	caption.Wrapping = fyne.TextWrapWord
	return container.NewVBox(caption, field)
}

// outlinedSelect wraps a Select in a bordered rectangle so it reads as a form
// control alongside Entries. Fyne's default Select renderer paints no visible
// outline on the app's gold theme, which made the "Default push mode" box
// blend into the panel background beside the neighbouring Entry.
func outlinedSelect(sel *shell.GoldSelect) fyne.CanvasObject {
	return outlined(shell.WideSelect(sel))
}

func outlined(control fyne.CanvasObject) fyne.CanvasObject {
	border := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	border.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	border.StrokeWidth = 1
	border.CornerRadius = theme.InputRadiusSize()
	return container.NewStack(border, control)
}

// goldPanel is a rounded panel with a low-alpha gold background, for content
// that should read as accented rather than as another neutral card.
func goldPanel(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.NRGBA{R: widgets.LogoGold.R, G: widgets.LogoGold.G, B: widgets.LogoGold.B, A: 48})
	bg.CornerRadius = shell.CellCornerRadius
	return container.NewStack(bg, container.NewPadded(content))
}

// chunkySettingsTheme grows inner padding so text fields and the dropdown
// render taller — filling more of the Settings main panel without needing
// a per-widget size wrapper on each one.
type chunkySettingsTheme struct{ fyne.Theme }

func (t chunkySettingsTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameInnerPadding {
		return 10
	}
	return t.Theme.Size(name)
}

// Lines on the main panel are gold, matching its section headings.
func (t chunkySettingsTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameSeparator {
		return widgets.LogoGold
	}
	return t.Theme.Color(name, variant)
}

// sidebarLineTheme makes a line in the sidebar grey: the app's muted text colour,
// so it stays readable in the light and dark themes alike.
type sidebarLineTheme struct{ fyne.Theme }

func (t sidebarLineTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameSeparator {
		return t.Theme.Color(theme.ColorNamePlaceHolder, variant)
	}
	return t.Theme.Color(name, variant)
}

// settingsOrgChip uses an explicit menu control to open the color picker.
func settingsOrgChip(name string, fill color.NRGBA, onPick func()) fyne.CanvasObject {
	bg := canvas.NewRectangle(fill)
	bg.CornerRadius = 12
	label := canvas.NewText(name, color.White)
	label.TextSize = theme.TextSize()
	label.TextStyle = fyne.TextStyle{Bold: true}
	more := widget.NewButton("...", onPick)
	more.Importance = widget.LowImportance
	return container.NewStack(bg, container.NewPadded(container.NewBorder(nil, nil, nil, more, container.NewCenter(label))))
}

// nextOrgPaletteColor advances the current colour to the next slot in the
// palette. An unrecognised colour resets to the first palette entry so the
// cycle stays predictable — clicking a chip always changes it to something.
func nextOrgPaletteColor(current color.NRGBA) color.NRGBA {
	for i, p := range shell.OrgPalette {
		if p == current {
			return shell.OrgPalette[(i+1)%len(shell.OrgPalette)]
		}
	}
	return shell.OrgPalette[0]
}
