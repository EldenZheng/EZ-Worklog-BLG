package main

import (
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type kanbanSource struct {
	URL    string `json:"url"`
	Filter string `json:"filter"`
}

func kanbanSources(cfg Config) []kanbanSource {
	if cfg.KanbanBoards != nil {
		return cfg.KanbanBoards
	}
	var out []kanbanSource
	for _, u := range projectURLs(cfg) {
		out = append(out, kanbanSource{URL: u, Filter: sprintProjectFilter})
	}
	return out
}

func validateKanbanSources(sources []kanbanSource) error {
	for i, s := range sources {
		u, e := url.Parse(s.URL)
		_, err := parseProjectRef(s.URL)
		if e != nil || err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") {
			return fmt.Errorf("Kanban board %d: enter a GitHub project URL", i+1)
		}
		for j := 0; j < i; j++ {
			if sameSprintProject(s.URL, sources[j].URL) {
				return fmt.Errorf("Kanban board %d duplicates another project", i+1)
			}
		}
	}
	return nil
}

func kanbanSourceEditor(initial []kanbanSource) (fyne.CanvasObject, func() ([]kanbanSource, error)) {
	type sourceRow struct {
		url, filter *widget.Entry
		box         *fyne.Container
	}
	var rows []*sourceRow
	list := container.NewVBox()
	var add func(kanbanSource)
	add = func(source kanbanSource) {
		r := &sourceRow{url: widget.NewEntry(), filter: widget.NewEntry()}
		r.url.SetText(source.URL)
		r.filter.SetText(source.Filter)
		r.url.SetPlaceHolder("https://github.com/orgs/organization/projects/1")
		r.filter.SetPlaceHolder("assignee:@me is:open (blank = all project items)")
		remove := widget.NewButton("Remove", func() {
			for i, x := range rows {
				if x == r {
					rows = append(rows[:i], rows[i+1:]...)
					break
				}
			}
			list.Remove(r.box)
		})
		r.box = container.NewBorder(nil, nil, nil, remove, container.NewGridWithColumns(2, r.url, r.filter))
		rows = append(rows, r)
		list.Add(r.box)
	}
	for _, source := range initial {
		add(source)
	}
	content := container.NewVBox(container.NewGridWithColumns(2, widget.NewLabel("Project board URL"), widget.NewLabel("GitHub project filter")), list, widget.NewButton("Add Kanban board", func() { add(kanbanSource{Filter: sprintProjectFilter}) }))
	return content, func() ([]kanbanSource, error) {
		out := []kanbanSource{}
		for _, r := range rows {
			out = append(out, kanbanSource{URL: strings.TrimSpace(r.url.Text), Filter: strings.TrimSpace(r.filter.Text)})
		}
		return out, validateKanbanSources(out)
	}
}
