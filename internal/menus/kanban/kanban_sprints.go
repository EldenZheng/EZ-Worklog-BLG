package kanban

import (
	"fmt"
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

func sprintSidebar(k *shell.KanbanUI) fyne.CanvasObject {
	side := container.NewVBox(widget.NewLabelWithStyle("Sprints", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	groups := k.CombinedSprints(true)
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
		count := sprintCountBadge(g.Count)
		right := container.NewHBox()
		if current != nil {
			right.Add(current)
		}
		right.Add(container.NewCenter(count))
		var content fyne.CanvasObject = container.NewBorder(nil, nil, container.NewCenter(widget.NewIcon(theme.HistoryIcon())), right, container.NewVBox(name, meta))
		selected := (k.Filter == "current" && g.Sprint.Contains(shell.Today())) || k.Filter == "sprint" && k.Sprint == g.Key
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
		side.Add(shell.NewCardTappable(container.New(sprintRowLayout{}, area), func() { ToggleSprint(k, g.Key); Draw(k) }))
		divider := canvas.NewRectangle(shell.BlendColor(theme.Color(theme.ColorNameBackground), theme.Color(theme.ColorNameForeground), 0.35))
		divider.SetMinSize(fyne.NewSize(1, 1))
		side.Add(divider)
	}
	for _, g := range groups {
		if backend.AddDays(g.Sprint.StartDate, g.Sprint.Duration) > shell.Today() {
			add(g)
		}
	}
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
		if k.Filter == "none" {
			ink = color.NRGBA{R: 24, G: 24, B: 24, A: 255}
		}
		row := container.NewPadded(container.NewBorder(nil, nil, nil, sprintCountBadge(noSprint), container.NewThemeOverride(label, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: ink})))
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
		side.Add(shell.NewCardTappable(area, func() {
			if k.Filter == "none" {
				k.Filter = "all"
			} else {
				k.Filter = "none"
			}
			Draw(k)
		}))
	}
	hasCompleted := false
	for _, g := range groups {
		if backend.AddDays(g.Sprint.StartDate, g.Sprint.Duration) <= shell.Today() {
			hasCompleted = true
		}
	}
	if hasCompleted {
		side.Add(widget.NewLabelWithStyle("Completed", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
		for i := len(groups) - 1; i >= 0; i-- {
			g := groups[i]
			if backend.AddDays(g.Sprint.StartDate, g.Sprint.Duration) <= shell.Today() {
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
