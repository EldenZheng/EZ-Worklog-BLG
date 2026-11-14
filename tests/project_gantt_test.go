package tests

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"testing"
	"time"
	"worklog/internal/menus/projects"
	"worklog/internal/shared/shell"
)

func TestProjectIssuesGanttToggleKeepsSearchAndState(t *testing.T) {
	defer stubChildren(t, map[string]string{"": childrenPageOne, "c1": childrenPageTwo})()
	ui := projectsUI(t, projects.Saved{Selected: "b", Projects: []projects.Project{{ID: "b", Name: "Checkout", Issues: []string{"exampleorg/shop#1"}}}})
	defer fyne.CurrentApp().Quit()
	openProjects(ui)
	page := projectsPage(ui)
	var mode *shell.UnderlineTabs
	var search *widget.Entry
	walk(page, func(o fyne.CanvasObject) {
		if s, ok := o.(*shell.UnderlineTabs); ok {
			for _, choice := range s.Options {
				if choice == "Gantt" {
					mode = s
				}
			}
		}
		if e, ok := o.(*widget.Entry); ok && len(e.PlaceHolder) > 13 && e.PlaceHolder[:13] == "Search issues" {
			search = e
		}
	})
	if mode == nil || search == nil {
		t.Fatal("the issue section needs both view toggle and existing search")
	}
	base := fyne.CurrentApp()
	driver := &collectDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: driver})
	defer fyne.SetCurrentApp(base)
	var transition *shell.Morph
	walk(page, func(o fyne.CanvasObject) {
		if m, ok := o.(*shell.Morph); ok {
			transition = m
		}
	})
	chart := func() *projects.GanttChart {
		var found *projects.GanttChart
		walk(page, func(o fyne.CanvasObject) {
			if g, ok := o.(*projects.GanttChart); ok {
				found = g
			}
		})
		return found
	}
	mode.SetSelected("Gantt")
	if transition == nil || len(driver.anims) == 0 {
		t.Fatal("the view toggle should start a sliding transition")
	}
	slide := driver.anims[len(driver.anims)-1]
	if slide.Duration != 600*time.Millisecond {
		t.Fatal("Issues/Gantt should slide over 600ms")
	}
	slide.Tick(.5)
	if transition.Content.Position().X <= 0 {
		t.Fatal("Gantt should slide in from the right")
	}
	slide.Tick(1)
	if transition.Content.Position().X != 0 {
		t.Fatal("the view must settle in place")
	}
	if chart() == nil || len(chart().Ranges) != 1 || chart().Ranges[0].Item.Number != 4 {
		t.Fatal("Gantt must show the same open-issue filter as the list")
	}
	search.SetText("no matching issue")
	if chart() != nil || !contains(labels(page), "No issues match.") {
		t.Fatal("search must narrow the Gantt too")
	}
	search.SetText("Payment")
	if chart() == nil || len(chart().Ranges) != 1 {
		t.Fatal("clearing a search must restore the matching timeline")
	}
	mode.SetSelected("Issues")
	if chart() != nil || !contains(labels(page), "#4  Payment") {
		t.Fatal("switching back must preserve the same issue search")
	}
}
