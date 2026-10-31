package main

import (
	"fmt"
	"image/color"
	"sort"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type combinedSprint struct {
	Key    string
	Sprint sprintIteration
	Count  int
}

func sprintKey(s sprintIteration) string { return fmt.Sprintf("%s/%d", s.StartDate, s.Duration) }
func (b sprintBoard) sprintByKey(key string) sprintIteration {
	for _, s := range b.iterations() {
		if sprintKey(s) == key {
			return s
		}
	}
	return sprintIteration{}
}
func (k *kanbanUI) visibleBoard(b sprintBoard) bool { return !k.projectOff[b.ID] }

func (k *kanbanUI) combinedSprints(kanban ...bool) []combinedSprint {
	groups := map[string]*combinedSprint{}
	for _, b := range k.plan.Boards {
		if !k.visibleBoard(b) {
			continue
		}
		for _, s := range b.iterations() {
			count := 0
			for _, it := range b.Items {
				if it.SprintID == s.ID && (len(kanban) == 0 || !kanban[0] || k.visibleItemType(it)) {
					count++
				}
			}
			if count == 0 {
				continue
			}
			key := sprintKey(s)
			if groups[key] == nil {
				groups[key] = &combinedSprint{Key: key, Sprint: s}
			}
			groups[key].Count += count
		}
	}
	var result []combinedSprint
	for _, g := range groups {
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Sprint.StartDate < result[j].Sprint.StartDate })
	return result
}

func (k *kanbanUI) projectLegend(calendar ...bool) fyne.CanvasObject {
	row := container.NewHBox(widget.NewLabelWithStyle("Projects", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	for _, b := range k.plan.Boards {
		b := b
		fill := k.accent(b, sprintItem{})
		var ink = theme.Color(theme.ColorNameForeground)
		if !k.visibleBoard(b) {
			fill = dimmed(fill)
			ink = theme.Color(theme.ColorNamePlaceHolder)
		}
		count, minutes := 0, 0
		hasPR := false
		for _, it := range b.Items {
			include := k.matchesIssueFilter(b, it)
			if len(calendar) > 0 && calendar[0] {
				selected := b.current(today())
				if k.calendarSprint != "" && k.calendarSprint != "current" {
					selected = b.sprintByKey(k.calendarSprint)
				}
				include = selected.ID != "" && it.SprintID == selected.ID
			}
			if include {
				count++
				hasPR = hasPR || it.itemType() == kanbanPullRequests
				minutes += sprintEstimate(it, sprintTask{})
			}
		}
		quantity := "issues"
		if count == 1 {
			quantity = "issue"
		}
		if hasPR {
			quantity = "items"
			if count == 1 {
				quantity = "item"
			}
		}
		name := canvas.NewText(fmt.Sprintf("%s (%d %s | %s estimated)", b.Title, count, quantity, hoursMins(minutes)), ink)
		name.TextSize = theme.TextSize()
		row.Add(newCardTappable(container.NewPadded(container.NewHBox(orgDotIn(fill), container.NewCenter(name))), func() {
			if k.projectOff == nil {
				k.projectOff = map[string]bool{}
			}
			k.projectOff[b.ID] = !k.projectOff[b.ID]
			k.draw()
		}))
	}
	return container.NewHScroll(row)
}

func sprintDateLabel(s sprintIteration) string {
	start, e := time.Parse("2006-01-02", s.StartDate)
	if e != nil {
		return s.StartDate
	}
	end := start.AddDate(0, 0, s.Duration-1)
	if start.Year() == end.Year() && start.Year() == time.Now().Year() {
		return start.Format("Jan 02") + " – " + end.Format("Jan 02")
	}
	return start.Format("Jan 02, 2006") + " – " + end.Format("Jan 02, 2006")
}

func (k *kanbanUI) sprintSidebar() fyne.CanvasObject {
	side := container.NewVBox(widget.NewLabelWithStyle("Sprints", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	groups := k.combinedSprints(true)
	add := func(g combinedSprint) {
		name := widget.NewLabelWithStyle(g.Sprint.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		meta := widget.NewLabel(sprintDateLabel(g.Sprint))
		meta.Wrapping = fyne.TextWrapWord
		var current fyne.CanvasObject
		if g.Sprint.contains(today()) {
			label := canvas.NewText("Current", logoGold)
			label.TextSize = theme.CaptionTextSize()
			frame := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
			frame.StrokeColor = logoGold
			frame.StrokeWidth = 1
			frame.CornerRadius = 12
			current = container.NewCenter(container.NewStack(frame, container.NewPadded(label)))
		}
		count := sprintCountBadge(g.Count)
		right := container.NewHBox()
		if current != nil {
			right.Add(current)
		}
		right.Add(container.NewCenter(count))
		var content fyne.CanvasObject = container.NewBorder(nil, nil, container.NewCenter(widget.NewIcon(theme.HistoryIcon())), right, container.NewVBox(name, meta))
		selected := (k.filter == "current" && g.Sprint.contains(today())) || k.filter == "sprint" && k.sprint == g.Key
		if selected {
			content = container.NewThemeOverride(content, statusHeaderTheme{Theme: theme.Current(), ink: color.NRGBA{R: 24, G: 24, B: 24, A: 255}})
		}
		area := k.dropArea(container.NewPadded(container.NewPadded(content)), "", "sprint", g.Key)
		if selected {
			for _, target := range k.targets {
				if target.object == area {
					target.frame.FillColor = logoGold
					target.frame.StrokeColor = logoGold
					target.frame.StrokeWidth = 2
				}
			}
		}
		// The whole tall row is both clickable and a drop target.
		side.Add(newCardTappable(container.New(sprintRowLayout{}, area), func() { k.toggleSprint(g.Key); k.draw() }))
		divider := canvas.NewRectangle(blendColor(theme.Color(theme.ColorNameBackground), theme.Color(theme.ColorNameForeground), 0.35))
		divider.SetMinSize(fyne.NewSize(1, 1))
		side.Add(divider)
	}
	for _, g := range groups {
		if addDays(g.Sprint.StartDate, g.Sprint.Duration) > today() {
			add(g)
		}
	}
	noSprint := 0
	for _, b := range k.plan.Boards {
		if k.visibleBoard(b) {
			for _, it := range b.Items {
				if it.SprintID == "" && k.visibleItemType(it) {
					noSprint++
				}
			}
		}
	}
	if noSprint > 0 {
		label := widget.NewLabelWithStyle("No Sprint", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		ink := color.Color(logoGold)
		if k.filter == "none" {
			ink = color.NRGBA{R: 24, G: 24, B: 24, A: 255}
		}
		row := container.NewPadded(container.NewBorder(nil, nil, nil, sprintCountBadge(noSprint), container.NewThemeOverride(label, statusHeaderTheme{Theme: theme.Current(), ink: ink})))
		area := k.dropArea(row, "", "sprint", "")
		for _, target := range k.targets {
			if target.object == area {
				target.frame.FillColor = color.Transparent
				if k.filter == "none" {
					target.frame.FillColor = logoGold
					target.frame.StrokeWidth = 1
					target.frame.StrokeColor = logoGold
				}
			}
		}
		side.Add(newCardTappable(area, func() {
			if k.filter == "none" {
				k.filter = "all"
			} else {
				k.filter = "none"
			}
			k.draw()
		}))
	}
	hasCompleted := false
	for _, g := range groups {
		if addDays(g.Sprint.StartDate, g.Sprint.Duration) <= today() {
			hasCompleted = true
		}
	}
	if hasCompleted {
		side.Add(widget.NewLabelWithStyle("Completed", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
		for i := len(groups) - 1; i >= 0; i-- {
			g := groups[i]
			if addDays(g.Sprint.StartDate, g.Sprint.Duration) <= today() {
				add(g)
			}
		}
	}
	return side
}

type sprintRowLayout struct{}

func sprintCountBadge(count int) fyne.CanvasObject {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameOverlayBackground))
	bg.CornerRadius = 12
	bg.SetMinSize(fyne.NewSize(26, 26))
	label := canvas.NewText(fmt.Sprint(count), theme.Color(theme.ColorNameForeground))
	label.TextSize = theme.CaptionTextSize()
	label.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewStack(bg, container.NewCenter(label))
}

func (sprintRowLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(290, 96) }
func (sprintRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

func (k *kanbanUI) calendarItems() []sprintCandidate {
	var out []sprintCandidate
	for _, b := range k.plan.Boards {
		if !k.visibleBoard(b) {
			continue
		}
		s := b.current(today())
		if k.calendarSprint != "" && k.calendarSprint != "current" {
			s = b.sprintByKey(k.calendarSprint)
		}
		if s.ID == "" {
			continue
		}
		for _, it := range b.Items {
			if it.SprintID == s.ID {
				out = append(out, sprintCandidate{it.key(b.ID), it, s})
			}
		}
	}
	return out
}

func (k *kanbanUI) sourcesChanged() {
	k.sourceVersion++
	k.loaded = false
	var keep []sprintBoard
	for _, b := range k.plan.Boards {
		for _, s := range kanbanSources(k.ui.cfg) {
			if sameSprintProject(b.URL, s.URL) {
				keep = append(keep, b)
				break
			}
		}
	}
	k.plan.Boards = keep
	k.draw()
	k.load(true)
}

func (k *kanbanUI) toggleSprint(key string) {
	selected := k.filter == "sprint" && k.sprint == key
	if k.filter == "current" {
		for _, g := range k.combinedSprints() {
			if g.Key == key && g.Sprint.contains(today()) {
				selected = true
			}
		}
	}
	if selected {
		k.filter = "all"
		k.sprint = ""
	} else {
		k.filter = "sprint"
		k.sprint = key
	}
}
