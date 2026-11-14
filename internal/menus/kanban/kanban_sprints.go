package kanban

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// sprintsShown is how many upcoming sprints the sidebar lists before
// "Show More…".
const sprintsShown = 9

// goldHeading is a sidebar heading in the logo gold.
func goldHeading(text string) fyne.CanvasObject {
	t := canvas.NewText(text, widgets.LogoGold)
	t.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewCenter(t)
}

func sprintSidebar(k *shell.KanbanUI) fyne.CanvasObject {
	side := container.NewVBox(container.NewHBox(goldHeading("Sprints")), shell.VGap(theme.Padding()))
	// A slide re-lays only the sidebar's geometry each frame, from its scroll
	// inwards, so the view rides along as the list grows or shrinks.
	reflow := func(final bool) {
		if scroll, ok := k.SideClip.(*container.Scroll); ok {
			shell.Reflow(final, scroll, scroll.Content, side)
		}
	}
	groups := k.CombinedSprints(true)
	var hoverRows []*shell.Tappable
	// into is where add puts a sprint's row: the sidebar, or the folding list
	// of finished sprints.
	into := side
	add := func(g shell.CombinedSprint) {
		name := widget.NewLabelWithStyle(g.Sprint.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		meta := widget.NewLabel(shell.SprintDateLabel(g.Sprint))
		meta.Wrapping = fyne.TextWrapWord
		var current fyne.CanvasObject
		if g.Sprint.Contains(shell.Today()) {
			label := canvas.NewText("Current", widgets.LogoGold)
			label.TextSize = theme.CaptionTextSize()
			frame := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
			frame.StrokeColor = widgets.LogoGold
			frame.StrokeWidth = 1
			frame.CornerRadius = 12
			current = container.NewCenter(container.NewStack(frame, container.NewPadded(label)))
		}
		count := shell.CountBadge(g.Count)
		selected := (k.Filter == "current" && g.Sprint.Contains(shell.Today())) || k.Filter == "sprint" && k.Sprint == g.Key
		right := container.NewHBox()
		if selected {
			right.Add(sprintRefreshControl(k, g.Key))
		}
		if current != nil {
			right.Add(current)
		}
		right.Add(container.NewCenter(count))
		var content fyne.CanvasObject = container.NewBorder(nil, nil, container.NewCenter(widget.NewIcon(theme.HistoryIcon())), right, container.NewVBox(name, meta))
		if selected {
			content = container.NewThemeOverride(content, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: color.NRGBA{R: 24, G: 24, B: 24, A: 255}})
		}
		area := dropArea(k, container.NewPadded(container.NewPadded(content)), "", "sprint", g.Key)
		if selected {
			for _, target := range k.Targets {
				if target.Object == area {
					target.Frame.FillColor = widgets.LogoGold
					target.Frame.StrokeColor = widgets.LogoGold
					target.Frame.StrokeWidth = 2
				}
			}
		}
		// The whole tall row is both clickable and a drop target.
		row := rippleRow(k, shell.NewCardTappable(container.New(sprintRowLayout{}, area), func() { ToggleSprint(k, g.Key); Draw(k) }))
		hoverRows = append(hoverRows, row)
		into.Add(row)
		divider := canvas.NewRectangle(shell.BlendColor(theme.Color(theme.ColorNameBackground), theme.Color(theme.ColorNameForeground), 0.35))
		divider.SetMinSize(fyne.NewSize(1, 1))
		into.Add(divider)
	}
	// The next few sprints are listed; the rest slide out under Show More.
	var upcoming []shell.CombinedSprint
	for _, g := range groups {
		if backend.AddDays(g.Sprint.StartDate, g.Sprint.Duration) > shell.Today() {
			upcoming = append(upcoming, g)
		}
	}
	for i, g := range upcoming {
		if i == sprintsShown {
			more := container.NewVBox()
			into = more
			fold := shell.NewFold(more, k.SprintsShowAll)
			// Gold words at rest; under the pointer a gold bar fills the
			// sidebar's width.
			var toggle *shell.HoverTag
			toggle = shell.NewHoverTag(showMoreText(k.SprintsShowAll), color.Transparent, widgets.LogoGold,
				widgets.LogoGold, shell.TagHoverInk, func() {
					k.SprintsShowAll = !fold.Open()
					toggle.Text = showMoreText(k.SprintsShowAll)
					toggle.Refresh()
					fold.Slide(k.SprintsShowAll, reflow)
				})
			toggle.Wide = true
			side.Add(fold)
			side.Add(toggle)
		}
		add(g)
	}
	into = side
	noSprint := 0
	for _, b := range k.Plan.Boards {
		if k.VisibleBoard(b) {
			for _, it := range b.Items {
				if it.SprintID == "" && k.VisibleItemType(it) {
					noSprint++
				}
			}
		}
	}
	if noSprint > 0 {
		label := widget.NewLabelWithStyle("No Sprint", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		ink := color.Color(widgets.LogoGold)
		right := container.NewHBox()
		if k.Filter == "none" {
			ink = color.NRGBA{R: 24, G: 24, B: 24, A: 255}
			right.Add(sprintRefreshControl(k, ""))
		}
		right.Add(container.NewCenter(shell.CountBadge(noSprint)))
		row := container.NewPadded(container.NewThemeOverride(container.NewBorder(nil, nil, nil, right, label), widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: ink}))
		area := dropArea(k, row, "", "sprint", "")
		for _, target := range k.Targets {
			if target.Object == area {
				target.Frame.FillColor = color.Transparent
				if k.Filter == "none" {
					target.Frame.FillColor = widgets.LogoGold
					target.Frame.StrokeWidth = 1
					target.Frame.StrokeColor = widgets.LogoGold
				}
			}
		}
		noSprintRow := rippleRow(k, shell.NewCardTappable(area, func() {
			if k.Filter == "none" {
				k.Filter = "all"
			} else {
				k.Filter = "none"
			}
			Draw(k)
		}))
		hoverRows = append(hoverRows, noSprintRow)
		side.Add(noSprintRow)
	}
	hasCompleted := false
	for _, g := range groups {
		if backend.AddDays(g.Sprint.StartDate, g.Sprint.Duration) <= shell.Today() {
			hasCompleted = true
		}
	}
	if hasCompleted {
		// Finished sprints fold away under their heading, folded by default;
		// the choice holds across redraws.
		completed := container.NewVBox()
		into = completed
		finished := 0
		for i := len(groups) - 1; i >= 0; i-- {
			g := groups[i]
			if backend.AddDays(g.Sprint.StartDate, g.Sprint.Duration) <= shell.Today() {
				add(g)
				finished++
			}
		}
		// Gold, with the count in the same badge the sprint rows carry, so the
		// folded heading still says how much is under it.
		label := container.NewHBox(goldHeading("Completed"), container.NewCenter(shell.CountBadge(finished)))
		head, area := shell.CollapseToggle(label, completed, k.CompletedOpen,
			func(on bool) { k.CompletedOpen = on }, reflow)
		side.Add(head)
		side.Add(area)
	}
	return shell.HoverRows(side, hoverRows)
}

type sprintRowLayout struct{}

func (sprintRowLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(290, 96) }

func (sprintRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

func SourcesChanged(k *shell.KanbanUI) {
	k.SourceVersion++
	k.Loaded = false
	var keep []backend.SprintBoard
	for _, b := range k.Plan.Boards {
		for _, s := range backend.KanbanSources(k.Ui.Cfg) {
			if backend.SameSprintProject(b.URL, s.URL) {
				keep = append(keep, b)
				break
			}
		}
	}
	k.Plan.Boards = keep
	Draw(k)
	k.Load(true)
}

func ToggleSprint(k *shell.KanbanUI, key string) {
	selected := k.Filter == "sprint" && k.Sprint == key
	if k.Filter == "current" {
		for _, g := range k.CombinedSprints() {
			if g.Key == key && g.Sprint.Contains(shell.Today()) {
				selected = true
			}
		}
	}
	if selected {
		k.Filter = "all"
		k.Sprint = ""
	} else {
		k.Filter = "sprint"
		k.Sprint = key
	}
}

func showMoreText(all bool) string {
	if all {
		return "Show Less"
	}
	return "Show More…"
}

// rippleRow makes a sidebar row ripple gold from where it is tapped. The
// ripple plays in the board's overlay, so the redraw the tap sets off does not
// cut it short.
func rippleRow(k *shell.KanbanUI, row *shell.Tappable) *shell.Tappable {
	row.OnTapAt = func(at fyne.Position) { shell.Ripple(k.GhostLayer, row, at) }
	return row
}
