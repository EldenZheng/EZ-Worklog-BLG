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
	// previous is the sprint that ended last, kept above the current one.
	previous := ""
	add := func(g shell.CombinedSprint) {
		name := widget.NewLabelWithStyle(g.Sprint.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		meta := widget.NewLabel(shell.SprintDateLabel(g.Sprint))
		meta.Wrapping = fyne.TextWrapWord
		var current fyne.CanvasObject
		if g.Sprint.Contains(shell.Today()) {
			current = sprintCapsule("Current", theme.Color(theme.ColorNameSuccess))
		} else if g.Key == previous {
			// The same capsule in gold: last sprint, finished but still in view.
			current = sprintCapsule("Previous", widgets.LogoGold)
		}
		count := sprintBadge(k, g.Count, g.Matches)
		selected := (k.Filter == "current" && g.Sprint.Contains(shell.Today())) || k.Filter == "sprint" && k.Sprint == g.Key
		motion := motionFor(k, g.Key, selected)
		right := container.NewHBox()
		refresh := sprintRefresh(k, g.Key, selected, motion)
		if refresh != nil {
			right.Add(refresh.box)
		} else if selected {
			right.Add(sprintRefreshControl(k, g.Key))
		}
		if current != nil {
			right.Add(current)
		}
		right.Add(container.NewCenter(count))
		icon := widget.NewIcon(theme.HistoryIcon())
		body := container.NewBorder(nil, nil, container.NewCenter(icon), right, container.NewVBox(name, meta))
		key := g.Key
		row := sprintRow(k, key, selected, motion, body, []*fyne.Container{right, body}, refresh, []*widget.Icon{icon}, theme.Color(theme.ColorNameForeground), 2,
			func(spot *fyne.Position) {
				selectRow(k, key, spot, func() { ToggleSprint(k, key) })
				Draw(k)
			})
		hoverRows = append(hoverRows, row)
		into.Add(row)
		divider := canvas.NewRectangle(shell.BlendColor(theme.Color(theme.ColorNameBackground), theme.Color(theme.ColorNameForeground), 0.35))
		divider.SetMinSize(fyne.NewSize(1, 1))
		into.Add(divider)
	}
	// The sprint that ended last stays in view above the current one, so last
	// sprint's leftovers are a click away; once the next sprint ends it folds
	// away under Completed with the rest.
	if g, ok := previousSprint(groups); ok {
		previous = g.Key
		add(g)
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
	noSprint, noSprintMatches := 0, 0
	for _, b := range k.Plan.Boards {
		if k.VisibleBoard(b) {
			noSprint += b.SprintCount("", k.VisibleItemType)
			if m := k.SprintMatches(b, "", k.VisibleItemType); m < 0 || noSprintMatches < 0 {
				noSprintMatches = -1
			} else {
				noSprintMatches += m
			}
		}
	}
	if noSprint > 0 {
		label := widget.NewLabelWithStyle("No Sprint", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		selected := k.Filter == "none"
		motion := motionFor(k, noSprintRow, selected)
		right := container.NewHBox()
		refresh := sprintRefresh(k, "", selected, motion)
		if refresh != nil {
			right.Add(refresh.box)
		} else if selected {
			right.Add(sprintRefreshControl(k, ""))
		}
		right.Add(container.NewCenter(sprintBadge(k, noSprint, noSprintMatches)))
		body := container.NewBorder(nil, nil, nil, right, label)
		row := sprintRow(k, noSprintRow, selected, motion, body, []*fyne.Container{right, body}, refresh, nil, widgets.LogoGold, 1,
			func(spot *fyne.Position) {
				selectRow(k, noSprintRow, spot, func() {
					if k.Filter == "none" {
						k.Filter = "all"
					} else {
						k.Filter = "none"
					}
				})
				Draw(k)
			})
		hoverRows = append(hoverRows, row)
		side.Add(row)
	}
	hasCompleted := false
	for _, g := range groups {
		if sprintEnded(g) && g.Key != previous {
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
			if sprintEnded(g) && g.Key != previous {
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

// sprintBadge counts a sprint's items or, while searching, its matches: "…"
// until GitHub has counted those in a sprint not fetched yet.
func sprintBadge(k *shell.KanbanUI, count, matches int) fyne.CanvasObject {
	switch {
	case k.Search == "":
		return shell.CountBadge(count)
	case matches < 0:
		return shell.TextBadge("…")
	}
	return shell.CountBadge(matches)
}

// sprintCapsule is the small outlined label at the right of a sprint's row.
func sprintCapsule(text string, ink color.Color) fyne.CanvasObject {
	label := canvas.NewText(text, ink)
	label.TextSize = theme.CaptionTextSize()
	frame := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	frame.StrokeColor = ink
	frame.StrokeWidth = 1
	frame.CornerRadius = 12
	return container.NewCenter(container.NewStack(frame, container.NewPadded(label)))
}

// sprintEnded says the sprint's last day is behind us.
func sprintEnded(g shell.CombinedSprint) bool {
	return backend.AddDays(g.Sprint.StartDate, g.Sprint.Duration) <= shell.Today()
}

// previousSprint is the finished sprint that ended last.
func previousSprint(groups []shell.CombinedSprint) (shell.CombinedSprint, bool) {
	var last shell.CombinedSprint
	found := false
	for _, g := range groups {
		end := backend.AddDays(g.Sprint.StartDate, g.Sprint.Duration)
		if sprintEnded(g) && (!found || end > backend.AddDays(last.Sprint.StartDate, last.Sprint.Duration)) {
			last, found = g, true
		}
	}
	return last, found
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

// sprintRefresh is the refresh button of a row moving into or out of its
// selected look, fading with it; nil for a row at rest. A row being let go
// of draws a button of its own, so the selected row keeps the real one.
func sprintRefresh(k *shell.KanbanUI, key string, selected bool, motion *rowMotion) *refreshFade {
	if motion == nil {
		return nil
	}
	if selected {
		return newRefreshFade(sprintRefreshControl(k, key))
	}
	leaving := newSprintRefreshButton(k, key)
	leaving.Disable()
	return newRefreshFade(shell.QuietButtonControl(leaving, &leaving.Button))
}

// sprintRow is a sprint's row in the sidebar (or No Sprint's, key
// noSprintRow): body inked dark on a gold fill while selected, otherwise in
// restInk on nothing, with an edge of stroke when selected. A row on its way
// into that look or out of it draws the way there (see selectLook); relaid is
// what to lay out again as its refresh button opens or closes, and icons the
// icons inked with the words. tapped hears a
// click, and where in the row it landed.
func sprintRow(k *shell.KanbanUI, key string, selected bool, motion *rowMotion, body fyne.CanvasObject, relaid []*fyne.Container, refresh *refreshFade,
	icons []*widget.Icon, restInk color.Color, stroke float32, tapped func(spot *fyne.Position)) *shell.Tappable {
	var ink *container.ThemeOverride
	var content fyne.CanvasObject = body
	switch {
	case motion != nil:
		ink = container.NewThemeOverride(body, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: restInk})
		content = ink
	case selected:
		content = container.NewThemeOverride(body, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: selectedInk})
	case key == noSprintRow:
		content = container.NewThemeOverride(body, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: restInk})
	}
	inner := container.NewPadded(container.NewPadded(content))
	if key == noSprintRow {
		inner = container.NewPadded(content)
	}
	var fill *canvas.Rectangle
	var drawn fyne.CanvasObject = inner
	if motion != nil {
		fill = canvas.NewRectangle(widgets.LogoGold)
		fill.Hide()
		drawn = container.NewStack(container.NewWithoutLayout(fill), inner)
	}
	value := key
	if key == noSprintRow {
		value = ""
	}
	area := dropArea(k, drawn, "", "sprint", value)
	var frame *canvas.Rectangle
	for _, target := range k.Targets {
		if target.Object == area {
			frame = target.Frame
		}
	}
	if frame != nil && selected && motion == nil {
		frame.FillColor = widgets.LogoGold
		frame.StrokeColor = widgets.LogoGold
		frame.StrokeWidth = stroke
	}
	var tappable fyne.CanvasObject = area
	if key != noSprintRow {
		// The whole tall row is both clickable and a drop target.
		tappable = container.New(sprintRowLayout{}, area)
	}
	var spot *fyne.Position
	row := shell.NewCardTappable(tappable, func() { tapped(spot) })
	spot = tapSpot(row)
	if motion != nil && frame != nil {
		look := &selectLook{area: area, frame: frame, fill: fill, stroke: stroke, ink: ink, restInk: restInk, body: relaid, refresh: refresh, icons: icons}
		look.play(motion)
	}
	return row
}
