package kanban

import (
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// ---- reordering the status columns, by dragging their headings ----
//
// The order is local, like the choice of columns: it is kept in
// sprint-planner.json as KanbanColumnOrder and changes nothing on GitHub.

// OrderColumns puts names in the saved order. A name the order does not know
// keeps its usual place, after those it does.
func OrderColumns(saved, names []string) []string {
	rank := map[string]int{}
	for i, name := range saved {
		rank[name] = i
	}
	out := slices.Clone(names)
	slices.SortStableFunc(out, func(a, b string) int {
		ra, okA := rank[a]
		rb, okB := rank[b]
		switch {
		case okA && okB:
			return ra - rb
		case okA:
			return -1
		case okB:
			return 1
		}
		return 0
	})
	return out
}

// MovedColumns is the saved order after the shown columns were dragged into
// shown: the hidden ones keep their places among them as far as they can,
// coming after the shown column they followed before.
func MovedColumns(all, shown []string) []string {
	visible := map[string]bool{}
	for _, name := range shown {
		visible[name] = true
	}
	out := slices.Clone(shown)
	for i, name := range all {
		if visible[name] {
			continue
		}
		// After the nearest shown column before it, or at the front.
		at := 0
		for back := i - 1; back >= 0; back-- {
			if j := slices.Index(out, all[back]); j >= 0 {
				at = j + 1
				break
			}
		}
		out = slices.Insert(out, at, name)
	}
	return out
}

// columnGrip is a column's heading, which can be dragged along the board.
type columnGrip struct {
	widget.BaseWidget
	k       *shell.KanbanUI
	status  string
	content fyne.CanvasObject
	grips   *[]*columnGrip
	lifted  bool
	ghost   fyne.CanvasObject
	line    *canvas.Rectangle
	order   []string // the order a drop here makes; nil when it changes nothing
}

func newColumnGrip(k *shell.KanbanUI, status string, grips *[]*columnGrip, content fyne.CanvasObject) *columnGrip {
	g := &columnGrip{k: k, status: status, content: content, grips: grips}
	g.ExtendBaseWidget(g)
	*grips = append(*grips, g)
	return g
}

func (g *columnGrip) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(g.content) }

func (g *columnGrip) Cursor() desktop.Cursor { return widgets.GrabCursor{} }

func (g *columnGrip) Dragged(e *fyne.DragEvent) {
	k, layer := g.k, g.k.GhostLayer
	if layer == nil {
		return
	}
	if !g.lifted {
		g.lifted = true
		k.Dragging = true
		g.ghost = shell.DragGhost(g.status, 0, KanbanStatusColor(g.status))
		g.line = canvas.NewRectangle(widgets.LogoGold)
		g.line.CornerRadius = 1.5
		g.line.Hide()
		layer.Objects = []fyne.CanvasObject{g.ghost, g.line}
	}
	driver := fyne.CurrentApp().Driver()
	origin := driver.AbsolutePositionForObject(layer)
	g.ghost.Move(e.AbsolutePosition.Subtract(origin).Add(fyne.NewPos(12, 12)))
	var spans []shell.ChipSpan
	var current []string
	for _, grip := range *g.grips {
		left := driver.AbsolutePositionForObject(grip).X
		spans = append(spans, shell.ChipSpan{ID: grip.status, Left: left, Right: left + grip.Size().Width})
		current = append(current, grip.status)
	}
	order, lineX := shell.LegendSlot(spans, g.status, e.AbsolutePosition.X, theme.Padding()*2)
	g.order = nil
	g.line.Hide()
	if !slices.Equal(order, current) {
		g.order = order
		top := driver.AbsolutePositionForObject(g).Y
		g.line.Move(fyne.NewPos(lineX-origin.X-1.5, top-origin.Y))
		g.line.Resize(fyne.NewSize(3, g.Size().Height))
		g.line.Show()
	}
	layer.Refresh()
}

func (g *columnGrip) DragEnd() {
	if !g.lifted {
		return
	}
	g.lifted = false
	k := g.k
	k.Dragging = false
	if k.GhostLayer != nil {
		k.GhostLayer.Objects = nil
		k.GhostLayer.Refresh()
	}
	order := g.order
	g.order = nil
	if order != nil {
		all, _ := statusOptions(k)
		if MoveColumns(k, MovedColumns(all, order)) {
			return
		}
	}
	// A save that finished mid-drag held its redraw back.
	if k.RedrawPending {
		Draw(k)
	}
}

// MoveColumns saves a new column order and redraws the board, the columns
// sliding from where they stood to their new places.
func MoveColumns(k *shell.KanbanUI, order []string) bool {
	from := map[string]float32{}
	for status, column := range boardColumns[k] {
		from[status] = column.Position().X
	}
	p := k.Plan
	p.KanbanColumnOrder = order
	if !k.Save(p) {
		return false
	}
	Draw(k)
	slideColumns(k, from)
	return true
}

// boardColumns is each drawn status column of a board, by status.
var boardColumns = map[*shell.KanbanUI]map[string]fyne.CanvasObject{}

const columnSlideTime = 320 * time.Millisecond

// slideColumns moves each column from where it stood (from) to where the new
// order put it, so a swap is seen rather than jumped.
func slideColumns(k *shell.KanbanUI, from map[string]float32) {
	columns := boardColumns[k]
	if len(from) == 0 || len(columns) == 0 || !fyne.CurrentApp().Settings().ShowAnimations() {
		return
	}
	to := map[string]float32{}
	anim := fyne.NewAnimation(columnSlideTime, func(p float32) {
		for status, column := range columns {
			start, ok := from[status]
			if !ok {
				continue
			}
			// Where the layout put it, read once before the first move.
			end, ok := to[status]
			if !ok {
				end = column.Position().X
				to[status] = end
			}
			column.Move(fyne.NewPos(start+(end-start)*p, column.Position().Y))
		}
	})
	anim.Curve = shell.EaseInOutCubic
	anim.Start()
}

// statusOptions is every column the board could show, in their current order.
func statusOptions(k *shell.KanbanUI) ([]string, []string) {
	shown, extra := StatusColumns(k)
	seen := map[string]bool{}
	var all []string
	for _, name := range append(append([]string{}, PrimaryKanbanStatuses...), extra...) {
		if !seen[name] {
			seen[name] = true
			all = append(all, name)
		}
	}
	return OrderColumns(k.Plan.KanbanColumnOrder, all), shown
}

// ShowColumnOrder opens the plain way to reorder the columns: every column
// the board can show, each with a button to move it up or down the order.
// Each move applies at once, the board's columns sliding to their places.
func ShowColumnOrder(k *shell.KanbanUI) {
	win := k.Ui.Win
	if win == nil {
		return
	}
	rows := container.NewVBox()
	var fill func()
	fill = func() {
		all, shown := statusOptions(k)
		visible := map[string]bool{}
		for _, name := range shown {
			visible[name] = true
		}
		rows.Objects = nil
		for i, name := range all {
			move := func(by int) func() {
				return func() {
					order := slices.Clone(all)
					j := i + by
					order[i], order[j] = order[j], order[i]
					MoveColumns(k, order)
					fill()
				}
			}
			up := widget.NewButtonWithIcon("", theme.MoveUpIcon(), move(-1))
			down := widget.NewButtonWithIcon("", theme.MoveDownIcon(), move(1))
			if i == 0 {
				up.Disable()
			}
			if i == len(all)-1 {
				down.Disable()
			}
			label := widget.NewLabel(name)
			if !visible[name] {
				label.Importance = widget.LowImportance // hidden by the filter
			}
			swatch := canvas.NewRectangle(KanbanStatusColor(name))
			swatch.CornerRadius = 3
			swatch.SetMinSize(fyne.NewSquareSize(10))
			rows.Add(container.NewBorder(nil, nil,
				container.NewCenter(swatch),
				container.NewHBox(shell.QuietButton(up), shell.QuietButton(down)), label))
		}
		rows.Refresh()
	}
	fill()
	note := widget.NewLabel("Greyed columns are hidden by the filter; they keep their place in the order.")
	note.Wrapping = fyne.TextWrapWord
	var d dialog.Dialog
	done := widget.NewButton("Close", func() { shell.CloseDialog(d, win) })
	d = dialog.NewCustomWithoutButtons("Column order",
		container.NewBorder(note, shell.DialogButtons(done), nil, nil, container.NewVScroll(rows)), win)
	size := k.Ui.PopupSize()
	d.Resize(fyne.NewSize(min(size.Width, 460), size.Height))
	shell.ShowDialog(d, win)
}
