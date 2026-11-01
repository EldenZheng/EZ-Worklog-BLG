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
	cur := widget.NewEntry()
	cur.SetPlaceHolder("RM")
	disp := widget.NewEntry()
	disp.SetPlaceHolder("USD")
	back := widget.NewEntry()
	mode := widget.NewSelect([]string{"Create Worklog sub-issue", "Write on the issue itself"}, nil)
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
	cur.SetText(backend.OrDefault(c.Currency, "RM"))
	disp.SetText(backend.OrDefault(c.DisplayCurrency, "USD"))
	if c.LookbackDays > 0 {
		back.SetText(strconv.Itoa(c.LookbackDays))
	} else {
		back.SetText("7")
	}
	if c.DefaultMode == "issue" {
		mode.SetSelected("Write on the issue itself")
	} else {
		mode.SetSelected("Create Worklog sub-issue")
	}
	key.SetText(c.AnthropicAPIKey)
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
	refreshRateReadout := func() {
		if ui.Cfg.FxRate > 0 {
			rateReadout.Text = fmt.Sprintf("1 %s = %s %s (%s)",
				backend.OrDefault(ui.Cfg.Currency, "RM"), shell.CommaAmount(ui.Cfg.FxRate),
				backend.OrDefault(ui.Cfg.DisplayCurrency, "USD"), ui.Cfg.FxUpdated)
		} else {
			rateReadout.Text = "Exchange rate not set — press Fetch exchange rate."
		}
		rateReadout.Refresh()
	}
	refreshRateReadout()

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
		newCfg.Currency = backend.OrDefault(strings.TrimSpace(cur.Text), "RM")
		newCfg.DisplayCurrency = strings.ToUpper(strings.TrimSpace(disp.Text))
		newCfg.LookbackDays = backN
		newCfg.DefaultMode = m
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
	})
	save.Importance = widget.HighImportance

	rate := widget.NewButton("Fetch exchange rate", func() {
		if !ui.HasCfg {
			msg.SetText("Save settings first.")
			return
		}
		msg.SetText("Fetching rate…")
		_, finish := ui.BeginFetch("settings-rate", "Fetching exchange rate")
		base := strings.ToUpper(strings.TrimSpace(ui.Cfg.Currency))
		if base == "RM" {
			base = "MYR"
		}
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
	ui.ProfileBox = container.NewVBox(widget.NewLabel("GitHub profile loads when Settings opens."))

	// Header row: Worklog / Settings on the left, action buttons on the right
	// stacked over the exchange rate readout. Border pins the buttons to the
	// top-right so they render at their own natural height rather than
	// stretching to match the tall logo block.
	actions := container.NewVBox(
		container.NewHBox(layout.NewSpacer(), rate, shell.OvalPush(save)),
		container.NewHBox(layout.NewSpacer(), rateReadout),
	)
	header := container.NewBorder(nil, nil, nil, actions, settingsHeading())

	// Only the GitHub profile card carries a bubble in the sidebar, and it is
	// tinted gold to match the primary accent. The identity text fields sit
	// beneath the bubble on plain background — the profile card is what needs
	// visual weight, the fields do not.
	sidebarContent := container.NewVBox(
		shell.Bold("Identity"),
		goldPanel(ui.ProfileBox),
		shell.Labeled("GitHub username (optional — auto from gh)", user),
		shell.Labeled("Worklog owner", owner),
		shell.Labeled("Name on meeting issues (optional)", dispName),
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
		sectionTitle("Sources"),
		container.New(widgets.NewRatioRow(0.72, 0.28), fieldsCol, legendCol),
	)
	salarySection := container.NewVBox(
		sectionTitle("Salary"),
		container.NewGridWithColumns(3,
			shell.LabeledHint("Base salary", "Paid per 21 working days.", sal),
			shell.LabeledHint("Currency", "Currency the salary is paid in.", cur),
			shell.LabeledHint("Display in", "Also show pay in this currency.", disp),
		),
		shell.LabeledHint("Support payment per set", "Paid per seven qualifying support issues, in the salary currency. Blank means unset.", supportPay),
	)
	worklogSection := container.NewVBox(
		sectionTitle("Worklog"),
		container.NewGridWithColumns(3,
			shell.LabeledHint("Look back", "Days back the commit sweep scans for unlogged work.", back),
			shell.LabeledHint("Default push mode", "Sub-issue creates a linked child. Issue writes on the parent itself.", outlinedSelect(mode)),
			shell.LabeledHint("Anthropic API key", "Enables the AI Compact-remarks button. Leave blank to disable.", key),
		),
		msg,
	)

	mainInner := container.NewVBox(
		sourcesSection,
		layout.NewSpacer(),
		salarySection,
		worklogSection,
	)
	// Chunky theme bumps input padding so entries and the dropdown read as
	// larger controls — filling more of the panel — without needing a per-widget
	// wrapper on each one.
	mainBubble := roundedPanel(container.NewThemeOverride(mainInner,
		chunkySettingsTheme{Theme: theme.Current()}))

	// Both columns are wrapped in Stack so their contents render at natural
	// height while the column itself stretches to whatever height ratioRow
	// hands it. The padding around each column adds a real gap between the
	// two cards on top of ratioRow's own inter-column spacing.
	body := container.New(widgets.NewRatioRow(0.2, 0.8),
		container.NewPadded(sidebarContent),
		container.NewVScroll(container.NewPadded(mainBubble)),
	)
	top := container.NewVBox(header, ui.SettingsProgress.View)
	// Border pins the header on top and hands every remaining pixel to the
	// body — the Settings tab now fills to the bottom of the window instead
	// of hugging its natural content height.
	return container.NewStack(container.NewBorder(top, nil, nil, nil, body), toast.layer)
}

// roundedPanel draws content inside a rounded rectangle so a group of fields
// reads as its own card. The bg is a subtle blend toward foreground so it
// reads as a distinct surface from the text fields and dropdowns inside — a
// panel painted with ColorNameInputBackground would swallow them.
func roundedPanel(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(shell.BlendColor(
		theme.Color(theme.ColorNameBackground),
		theme.Color(theme.ColorNameForeground),
		0.08,
	))
	bg.CornerRadius = shell.CellCornerRadius
	return container.NewStack(bg, container.NewPadded(content))
}

// outlinedSelect wraps a Select in a bordered rectangle so it reads as a form
// control alongside Entries. Fyne's default Select renderer paints no visible
// outline on the app's gold theme, which made the "Default push mode" box
// blend into the panel background beside the neighbouring Entry.
func outlinedSelect(sel *widget.Select) fyne.CanvasObject {
	border := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	border.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	border.StrokeWidth = 1
	border.CornerRadius = theme.InputRadiusSize()
	return container.NewStack(border, shell.WideSelect(sel))
}

// goldPanel is roundedPanel with a low-alpha gold background, for content
// that should read as accented rather than as another neutral card.
func goldPanel(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.NRGBA{R: widgets.LogoGold.R, G: widgets.LogoGold.G, B: widgets.LogoGold.B, A: 48})
	bg.CornerRadius = shell.CellCornerRadius
	return container.NewStack(bg, container.NewPadded(content))
}

// sectionTitle is a gold heading with a separator under it, used to split the
// Settings main pane into named groups (Repos and Orgs, Project boards,
// Salary, Worklog).
func sectionTitle(title string) fyne.CanvasObject {
	t := canvas.NewText(title, widgets.LogoGold)
	t.TextSize = theme.TextSize() + 4
	t.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewVBox(t, widget.NewSeparator())
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
