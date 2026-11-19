package projects

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// parentsView is a parent issue list, one short title and one far too long
// for the column, in a narrow window.
func parentsView(t *testing.T) (*view, fyne.Window, fyne.CanvasObject) {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	p := &page{ui: &shell.UI{}, ticks: map[string]*widgets.TickCheck{}}
	snap := Snapshot{Parents: []Parent{
		{Ref: "o/r#1", Title: "A very long parent issue title that would never fit in this narrow column at all", URL: "https://github.com/o/r/issues/1",
			Items: []Item{{Number: 2, Title: "child", URL: "https://github.com/o/r/issues/2", Repo: "o/r"}}},
		{Ref: "o/r#3", Title: "Short one but a bit longer", URL: "https://github.com/o/r/issues/3",
			Items: []Item{{Number: 4, Title: "child", URL: "https://github.com/o/r/issues/4", Repo: "o/r", Closed: true}}},
	}}
	v := &view{p: p, cur: Project{ID: "x"}, all: snap, pal: currentPalette(), now: time.Now()}
	panel, _ := v.parents()
	w := test.NewWindow(panel)
	w.Resize(fyne.NewSize(320, 260))
	return v, w, panel
}

func visitAll(o fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	visit(o)
	switch x := o.(type) {
	case *fyne.Container:
		for _, c := range x.Objects {
			visitAll(c, visit)
		}
	case fyne.Widget:
		for _, c := range test.WidgetRenderer(x).Objects() {
			visitAll(c, visit)
		}
	}
}

// A parent's title takes only the width of its words, so a click beside it
// ticks the row, and never runs past the row: a short title shows whole and a
// long one is cut short with an ellipsis.
func TestParentTitlesFitTheirRow(t *testing.T) {
	_, _, panel := parentsView(t)
	var list *HoverList
	links := map[string]*shell.HoverLink{}
	visitAll(panel, func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *HoverList:
			list = x
		case *shell.HoverLink:
			links[x.Text] = x
		}
	})
	row := list.Rows.Objects[0].Size().Width
	long, short := links["A very long parent issue title that would never fit in this narrow column at all"], links["Short one but a bit longer"]
	if long == nil || short == nil {
		t.Fatal("both titles should be links")
	}
	if long.Size().Width >= row {
		t.Fatalf("the long title should be cut to the row: %v of %v", long.Size().Width, row)
	}
	if short.Size().Width >= long.Size().Width {
		t.Fatalf("the short title should take only its words: %v", short.Size().Width)
	}
}

// Ticking a parent draws the list again under the pointer; the row it is on
// comes up still lit.
func TestParentRowKeepsItsHoverWhenTicked(t *testing.T) {
	v, w, panel := parentsView(t)
	find := func(o fyne.CanvasObject) (list *HoverList) {
		visitAll(o, func(o fyne.CanvasObject) {
			if l, ok := o.(*HoverList); ok {
				list = l
			}
		})
		return list
	}
	find(panel).hover(1, true)
	again, _ := v.parents()
	w.SetContent(again)
	if l := find(again); l.hovered != 1 {
		t.Fatalf("the row under the pointer should stay lit, got row %d", l.hovered)
	}
}
