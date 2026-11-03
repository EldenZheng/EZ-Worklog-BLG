package shell

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

type KanbanTarget struct {
	Object             fyne.CanvasObject
	Clip               fyne.CanvasObject
	Frame              *canvas.Rectangle
	Board, Kind, Value string
	StrokeColor        color.Color
	StrokeWidth        float32
}

type KanbanUI struct {
	Ui                     *UI
	Plan                   backend.SprintPlan
	Root, Body, GhostLayer *fyne.Container
	SideClip, BoardClip    fyne.CanvasObject
	// CompletedOpen unfolds the finished sprints in the sidebar; folded by default.
	CompletedOpen bool
	// CardsShown is which cards the board last showed (filter, sprint,
	// search); when it changes the cards arrive top to bottom.
	CardsShown string
	// SprintsShowAll lists every upcoming sprint, not only the first few.
	SprintsShowAll                      bool
	Message                             *widget.Label
	Targets                             []*KanbanTarget
	Hover                               *KanbanTarget
	Loading, Busy                       bool
	Loaded                              bool
	StorageErr                          error
	Filter, board, Sprint, Search, Week string
	CalendarBody, CalendarGhost         *fyne.Container
	CalendarMessage                     *widget.Label
	CalendarMode, Month                 string
	Dates                               []*PlannerDateTarget
	CalendarClip                        fyne.CanvasObject
	CalendarIssueClip                   fyne.CanvasObject
	CalendarDrawnMode                   string
	CalendarOffsets                     map[string]fyne.Position
	AutoStarted                         map[string]bool // issues already auto-started this session
	AutoStartPending                    bool            // next board fetch should start today's planned issues
	Pending                             map[string]KanbanPending
	// Queue holds GitHub writes waiting their turn; Running is the one in
	// flight. Writes go one at a time, in the order they were made.
	Queue                      []*KanbanJob
	Running                    *KanbanJob
	ReloadAfterQueue           bool                         // a board refresh was asked for while writes were queued
	Dragging                   bool                         // a Kanban card is being dragged
	RedrawPending              bool                         // a redraw was held back until the drag ends
	ColumnCards                map[string][]*SprintDragCard // status value: cards as drawn
	HoverSlot                  KanbanSlot
	DropLine                   *canvas.Rectangle
	Activity, CalendarActivity *loadingIndicator
	CalendarPage               *fyne.Container
	RefreshButtons             []*widget.Button
	CalendarDrag               *PlannerDragCard
	CalendarResizing           bool
	CalendarActive             bool
	ProjectOff                 map[string]bool
	SourceVersion              int
	CalendarSprint             string
	ExtraStatuses              map[string]bool
	StatusChoices              *widget.CheckGroup
	StatusChoiceView           fyne.CanvasObject
	TypeChoiceView             fyne.CanvasObject
}

func (k *KanbanUI) Save(p backend.SprintPlan) bool {
	if k.StorageErr != nil {
		k.Ui.Errf(fmt.Errorf("local planner could not be read; preserving its file: %w", k.StorageErr))
		return false
	}
	if err := k.Ui.Store.SaveSprintPlan(p); err != nil {
		k.Ui.Errf(err)
		return false
	}
	k.Plan = p
	// Log Work's day bars show the planned time; keep them in step.
	if k.Ui.WeekBox != nil {
		Hooks.DrawWeekStrip(k.Ui)
	}
	return true
}

func (k *KanbanUI) Load(force bool) {
	if k.Busy || len(k.Queue) > 0 {
		// Never refresh under queued writes: the fresh boards would race
		// them. Refresh once the queue is empty instead.
		if force || !k.Loaded {
			k.ReloadAfterQueue = true
		}
		return
	}
	if k.Loading || (k.Loaded && !force) {
		return
	}
	k.Loading = true
	k.SyncActivity()
	k.SetMessage("Loading configured project boards… You can use the saved local calendar while loading.")
	previousBoards := append([]backend.SprintBoard{}, k.Plan.Boards...)
	sources := append([]backend.KanbanSource{}, backend.KanbanSources(k.Ui.Cfg)...)
	version := k.SourceVersion
	boards := make([]backend.SprintBoard, 0, 2)
	var errors []string
	// Async rather than a bare goroutine, so the tests can run it in step:
	// the save at the end writes the planner file, and one landing after its
	// test had finished wrote into a folder already being deleted.
	k.Ui.Async(func() error {
		for _, source := range sources {
			u := source.URL
			b, err := backend.FetchSprintBoardWithFilter(u, source.Filter)
			if err != nil {
				errors = append(errors, u+": "+err.Error())
				for _, old := range previousBoards {
					if backend.SameSprintProject(old.URL, u) {
						boards = append(boards, old)
					}
				}
			} else {
				boards = append(boards, b)
			}
		}
		return nil
	}, func() {
		k.Loading = false
		if version != k.SourceVersion {
			k.Load(true)
			return
		}
		k.Loaded = len(errors) == 0
		p := k.Plan
		p.Boards = boards
		p.Fetched = time.Now().Format(time.RFC3339)
		if k.Save(p) {
			k.SetMessage("")
			if len(errors) > 0 {
				k.SetMessage("Refresh incomplete; saved data retained for unavailable boards. " + strings.Join(errors, "; "))
			}
			// Fresh statuses: drop plans for issues that have left the
			// boards. Today's planned issues are started only when the
			// fetch came from the Sprint Calendar or app start, never from
			// Kanban, where status changes are made by dragging.
			k.PrunePlan()
			if k.AutoStartPending {
				k.AutoStartPending = false
				Hooks.KanbanUIStartPlannedToday(k)
			}
		}
		Hooks.KanbanUIDraw(k)
		// Writes made while the boards loaded can go now.
		Hooks.KanbanKick(k)
	})
}

func (k *KanbanUI) SetMessage(text string) {
	if k.Message != nil {
		k.Message.SetText(text)
		ShowIf(k.Message, strings.TrimSpace(text) != "")
	}
	if k.CalendarMessage != nil {
		k.CalendarMessage.SetText(text)
		ShowIf(k.CalendarMessage, strings.TrimSpace(text) != "")
	}
}

func (k *KanbanUI) Accent(b backend.SprintBoard, it backend.SprintItem) color.NRGBA {
	// The meeting bubble takes the planned-time colour of the day bars: it
	// belongs to no org, and what it plans is time set aside.
	if b.ID == plannerMeetingBoard.ID {
		return PlannerPlanColor
	}
	org := OrgOf(it.URL)
	if org == "" {
		ref, err := backend.ParseProjectRef(b.URL)
		if err == nil {
			org = ref.Login
		}
	}
	cfg := k.ColorConfig()
	return OrgColor(cfg, org)
}

func (k *KanbanUI) Bubble(b backend.SprintBoard, it backend.SprintItem, caption string, compact bool) fyne.CanvasObject {
	title := widget.NewLabelWithStyle(it.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord
	if caption == "" {
		body := container.NewVBox(title)
		if it.URL != "" {
			body.Add(GithubHyperlink("Open issue", it.URL))
		}
		return Hooks.KanbanUIBubbleSurface(k, b, it, body)
	}
	foot := widget.NewLabel(caption)
	foot.Wrapping = fyne.TextWrapWord
	return Hooks.KanbanUIBubbleSurface(k, b, it, Hooks.KanbanUIBubbleContents(k, title, foot, it.URL))
}

func SprintSummary(s backend.SprintIteration) string {
	end := backend.AddDays(s.StartDate, s.Duration-1)
	days := 0
	for date := Today(); s.Contains(date); date = backend.AddDays(date, 1) {
		days++
	}
	label := fmt.Sprintf("%s · %s – %s · %d days", s.Title, s.StartDate, end, s.Duration)
	if s.Contains(Today()) {
		label += fmt.Sprintf(" · %d days left", days)
	}
	return label
}

func pointIn(obj fyne.CanvasObject, p fyne.Position) bool {
	if obj == nil || !obj.Visible() {
		return false
	}
	o := fyne.CurrentApp().Driver().AbsolutePositionForObject(obj)
	s := obj.Size()
	return p.X >= o.X && p.Y >= o.Y && p.X < o.X+s.Width && p.Y < o.Y+s.Height
}

type SprintDragCard struct {
	widget.BaseWidget
	K       *KanbanUI
	Board   backend.SprintBoard
	Item    backend.SprintItem
	Content fyne.CanvasObject
	lifted  bool
}

func (c *SprintDragCard) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.Content)
}

func (c *SprintDragCard) Cursor() desktop.Cursor { return widgets.GrabCursor{} }

func (c *SprintDragCard) Tapped(*fyne.PointEvent) { Hooks.KanbanUIEditor(c.K, c.Board, c.Item) }

func (c *SprintDragCard) Dragged(e *fyne.DragEvent) {
	k := c.K
	// Saves in flight do not block a drag: the drop joins the queue. Only a
	// board refresh does, because it replaces every card.
	if k.Loading && !c.lifted {
		return
	}
	if !c.lifted {
		c.lifted = true
		k.Dragging = true
		ghost := DragGhost(c.Item.Title, backend.SprintEstimate(c.Item, k.Plan.Tasks[c.Item.Key(c.Board.ID)]), k.Accent(c.Board, c.Item))
		HoldBubble(c.Content)
		k.DropLine = newDropLine()
		k.GhostLayer.Objects = []fyne.CanvasObject{ghost, k.DropLine}
	}
	if len(k.GhostLayer.Objects) > 0 {
		k.GhostLayer.Objects[0].Move(e.AbsolutePosition.Subtract(fyne.CurrentApp().Driver().AbsolutePositionForObject(k.GhostLayer)).Add(fyne.NewPos(12, 12)))
		k.GhostLayer.Refresh()
	}
	k.Hover = nil
	for _, t := range k.Targets {
		valid := t.Board == "" || t.Board == c.Board.ID
		if t.Kind == "status" && t.Value != "" {
			valid = false
			for _, o := range c.Board.Field("Status").Options {
				if o.Name == t.Value {
					valid = true
				}
			}
		}
		if t.Kind == "sprint" && t.Value != "" {
			valid = valid && c.Board.SprintByKey(t.Value).ID != ""
		}
		hover := valid && pointIn(t.Object, e.AbsolutePosition) && pointIn(t.Clip, e.AbsolutePosition)
		t.Frame.StrokeWidth = t.StrokeWidth
		t.Frame.StrokeColor = t.StrokeColor
		if hover {
			k.Hover = t
			t.Frame.StrokeWidth = 3
			t.Frame.StrokeColor = widgets.LogoGold
		}
		t.Frame.Refresh()
	}
	Hooks.KanbanUIShowDropSlot(k, c, k.Hover, e.AbsolutePosition)
}

func (c *SprintDragCard) DragEnd() {
	if !c.lifted {
		return
	}
	c.lifted = false
	ReleaseBubble()
	k := c.K
	k.Dragging = false
	// A save that finished mid-drag held its redraw back; do it now unless
	// the drop below redraws anyway.
	defer func() {
		if k.RedrawPending {
			Hooks.KanbanUIDraw(k)
		}
	}()
	k.GhostLayer.Objects = nil
	k.GhostLayer.Refresh()
	t := k.Hover
	k.Hover = nil
	slot := k.HoverSlot
	k.HoverSlot = KanbanSlot{}
	k.DropLine = nil
	for _, target := range k.Targets {
		target.Frame.StrokeWidth = target.StrokeWidth
		target.Frame.StrokeColor = target.StrokeColor
		target.Frame.Refresh()
	}
	if t != nil {
		value := t.Value
		if t.Kind == "sprint" && value != "" {
			value = c.Board.SprintByKey(value).ID
		}
		if t.Kind == "status" {
			Hooks.KanbanUIPlace(k, c.Board, c.Item, value, slot)
			return
		}
		Hooks.KanbanUIMove(k, c.Board, c.Item, t.Kind, value)
	}
}

func (k *KanbanUI) ColorConfig() backend.Config {
	cfg := k.Ui.Cfg
	cfg.ExtraProjectURLs = append([]string{}, cfg.ExtraProjectURLs...)
	for _, b := range k.Plan.Boards {
		cfg.ExtraProjectURLs = append(cfg.ExtraProjectURLs, b.URL)
	}
	return cfg
}

func (k *KanbanUI) VisibleItemType(it backend.SprintItem) bool {
	if it.Kind == "DraftIssue" {
		return false
	}
	if k.Plan.KanbanTypes == nil {
		return true
	}
	for _, selected := range *k.Plan.KanbanTypes {
		if selected == it.ItemType() {
			return true
		}
	}
	return false
}

// KanbanSlot is where a dragged card would land in a status column.
type KanbanSlot struct {
	Ok           bool    // the column holds other cards of the same project
	AfterID      string  // place after this item; "" is the top
	CurrentAfter string  // the card's own predecessor, when it is in this column
	InColumn     bool    // the dragged card is drawn in this column
	LineY        float32 // where to draw the drop line
}

// Unchanged: dropped back into its own gap in its own column.
func (s KanbanSlot) Unchanged() bool { return s.InColumn && s.AfterID == s.CurrentAfter }

func newDropLine() *canvas.Rectangle {
	line := canvas.NewRectangle(widgets.LogoGold)
	line.CornerRadius = 1.5
	line.Hide()
	return line
}

// KanbanPending is a status or sprint change on its way to GitHub. While it
// is in flight the card is drawn where it is headed (status) or where it is
// (sprint), dimmed, with what is being saved written across it.
type KanbanPending struct {
	Kind, Value string
	Label       string // "In Progress", "Sprint 5", "No status"
	// Status is the column the card is headed for once every queued write
	// for it lands; HasStatus says whether any of them changes the status.
	Status    string
	HasStatus bool
	Queued    bool // waiting behind another card's save, not yet sent
}

// KanbanJob is one write to GitHub: a status, a sprint, or a drop into a
// column at a position ("place").
type KanbanJob struct {
	Board     backend.SprintBoard
	Item      backend.SprintItem
	Kind      string // "status", "sprint" or "place"
	Value     string // the status or sprint; for place, the status
	AfterID   string // place: the card it goes after ("" for the top)
	PrevAfter string // place: the card it was after, to put it back on failure
	Start     []func()
	Done      []func(error)
}

func (j *KanbanJob) Key() string { return j.Item.Key(j.Board.ID) }

// PendingWrites counts the writes not yet on GitHub, the one in flight
// included.
func (k *KanbanUI) PendingWrites() int {
	n := len(k.Queue)
	if k.Running != nil {
		n++
	}
	return n
}

// SyncActivity runs the app's loading strip on Kanban and Sprint Calendar
// while a fetch or a write is running (only on the tab in view, as the other
// tabs do), and disables Refresh GitHub meanwhile.
func (k *KanbanUI) SyncActivity() {
	on := k.Busy || k.Loading
	stage := "Loading project boards"
	if k.Busy {
		stage = "Saving to GitHub"
	}
	selected := ""
	if k.Ui.Tabs != nil && k.Ui.Tabs.Selected() != nil {
		selected = k.Ui.Tabs.Selected().Text
	}
	for _, page := range []struct {
		name      string
		indicator *loadingIndicator
		page      *fyne.Container
	}{{KanbanTabName, k.Activity, k.Root}, {PlannerTabName, k.CalendarActivity, k.CalendarPage}} {
		if page.indicator == nil {
			continue
		}
		active := on && (k.Ui.Tabs == nil || selected == page.name)
		if !active && !page.indicator.View.Visible() {
			continue
		}
		if page.indicator.ShowProgress(active, backend.FetchProgress{Stage: stage}) {
			Relayout(page.page)
		}
	}
	for _, b := range k.RefreshButtons {
		if on {
			b.Disable()
		} else {
			b.Enable()
		}
	}
}

type CombinedSprint struct {
	Key    string
	Sprint backend.SprintIteration
	Count  int
}

func (k *KanbanUI) VisibleBoard(b backend.SprintBoard) bool { return !k.ProjectOff[b.ID] }

func (k *KanbanUI) CombinedSprints(kanban ...bool) []CombinedSprint {
	groups := map[string]*CombinedSprint{}
	for _, b := range k.Plan.Boards {
		if !k.VisibleBoard(b) {
			continue
		}
		for _, s := range b.Iterations() {
			count := 0
			for _, it := range b.Items {
				if it.SprintID == s.ID && (len(kanban) == 0 || !kanban[0] || k.VisibleItemType(it)) {
					count++
				}
			}
			if count == 0 {
				continue
			}
			key := backend.SprintKey(s)
			if groups[key] == nil {
				groups[key] = &CombinedSprint{Key: key, Sprint: s}
			}
			groups[key].Count += count
		}
	}
	var result []CombinedSprint
	for _, g := range groups {
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Sprint.StartDate < result[j].Sprint.StartDate })
	return result
}

func (k *KanbanUI) ProjectLegend(calendar ...bool) fyne.CanvasObject {
	row := container.NewHBox(widget.NewLabelWithStyle("Projects", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	for _, b := range k.Plan.Boards {
		b := b
		count, minutes := 0, 0
		hasPR := false
		for _, it := range b.Items {
			include := Hooks.KanbanUIMatchesIssueFilter(k, b, it)
			if len(calendar) > 0 && calendar[0] {
				selected := b.Current(Today())
				if k.CalendarSprint != "" && k.CalendarSprint != "current" {
					selected = b.SprintByKey(k.CalendarSprint)
				}
				include = selected.ID != "" && it.SprintID == selected.ID
			}
			if include {
				count++
				hasPR = hasPR || it.ItemType() == backend.KanbanPullRequests
				minutes += backend.SprintEstimate(it, backend.SprintTask{})
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
		label := fmt.Sprintf("%s (%d %s | %s estimated)", b.Title, count, quantity, backend.HoursMins(minutes))
		row.Add(LegendChip(k.Accent(b, backend.SprintItem{}), label, !k.VisibleBoard(b), func() {
			if k.ProjectOff == nil {
				k.ProjectOff = map[string]bool{}
			}
			k.ProjectOff[b.ID] = !k.ProjectOff[b.ID]
			Hooks.KanbanUIDraw(k)
		}))
	}
	return container.NewHScroll(row)
}

func SprintDateLabel(s backend.SprintIteration) string {
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

func (k *KanbanUI) CalendarItems() []backend.SprintCandidate {
	var out []backend.SprintCandidate
	for _, b := range k.Plan.Boards {
		if !k.VisibleBoard(b) {
			continue
		}
		s := b.Current(Today())
		if k.CalendarSprint != "" && k.CalendarSprint != "current" {
			s = b.SprintByKey(k.CalendarSprint)
		}
		if s.ID == "" {
			continue
		}
		for _, it := range b.Items {
			// Support rota issues are not planned work.
			if it.SprintID == s.ID && !backend.IsSupportIssue(it.Title) {
				out = append(out, backend.SprintCandidate{it.Key(b.ID), it, s})
			}
		}
	}
	return out
}

// PrunePlan saves a pruned plan if pruning removed anything.
func (k *KanbanUI) PrunePlan() {
	if k.StorageErr != nil {
		return
	}
	if p, changed := backend.PruneSprintPlan(k.Plan, Today()); changed {
		k.Save(p)
	}
}

func (k *KanbanUI) BoardItem(c backend.SprintCandidate) (backend.SprintBoard, backend.SprintItem, bool) {
	if c.Key == PlannerMeetingKey {
		return plannerMeetingBoard, PlannerMeetingCandidate().Item, true
	}
	for _, b := range k.Plan.Boards {
		for _, it := range b.Items {
			if it.Key(b.ID) == c.Key {
				return b, it, true
			}
		}
	}
	return backend.SprintBoard{}, backend.SprintItem{}, false
}
