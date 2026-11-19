package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"worklog/internal/menus/projects"
	"worklog/internal/shared/backend"
)

func TestProjectsIgnoreLegacySelectionAndKeepOnlySessionChoice(t *testing.T) {
	store := backend.NewStore(tempDir(t))
	path := filepath.Join(store.Dir, "projects.json")
	if err := os.WriteFile(path, []byte(`{"selected":"b","projects":[{"id":"a","name":"Left"},{"id":"b","name":"Right"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := projects.Load(store)
	if err != nil || loaded.Selected != "" || loaded.Current() == nil || loaded.Current().ID != "a" {
		t.Fatal("a previous selection must not override the leftmost project", loaded, err)
	}
	loaded.Selected = "b"
	if loaded.Current().ID != "b" {
		t.Fatal("choosing another project must still work during the session")
	}
	if err := projects.Save(store, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err = projects.Load(store)
	if err != nil || loaded.Current() == nil || loaded.Current().ID != "a" {
		t.Fatal("reopening must return to the leftmost project", loaded, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, saved := fields["selected"]; saved {
		t.Fatal("the project choice must not be written to disk")
	}
}

func TestParentIssueChecksSurviveReload(t *testing.T) {
	store := backend.NewStore(tempDir(t))
	project := projects.Project{ID: "p", Name: "Example", Issues: []string{"o/r#1", "o/r#2"}}
	project.SetCounts("o/r#2", false)
	if err := projects.Save(store, projects.Saved{Selected: "p", Projects: []projects.Project{project}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := projects.Load(backend.NewStore(store.Dir))
	if err != nil || loaded.Current() == nil || !loaded.Current().Counts("o/r#1") || loaded.Current().Counts("o/r#2") {
		t.Fatal("parent checkbox preferences must survive reopening", loaded, err)
	}
}

func TestProjectIssueDefaultSortIsSprintWithinMilestone(t *testing.T) {
	var page map[string]any
	if err := json.Unmarshal([]byte(childrenPageTwo), &page); err != nil {
		t.Fatal(err)
	}
	issue := page["data"].(map[string]any)["repository"].(map[string]any)["issue"].(map[string]any)
	children := issue["subIssues"].(map[string]any)
	children["nodes"] = append(children["nodes"].([]any), map[string]any{
		"number": 5, "title": "Earlier sprint", "url": "https://github.com/exampleorg/shop/issues/5", "state": "OPEN", "createdAt": "2026-11-01T03:00:00Z",
		"repository":   map[string]any{"nameWithOwner": "exampleorg/shop"},
		"milestone":    map[string]any{"title": "Beta", "dueOn": "2026-12-01T00:00:00Z"},
		"projectItems": map[string]any{"nodes": []any{map[string]any{"sprint": map[string]any{"title": "Early", "startDate": "2026-10-05", "duration": 7}}}},
	})
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	defer stubChildren(t, map[string]string{"": childrenPageOne, "c1": string(raw)})()
	ui := projectsUI(t, projects.Saved{Selected: "p", Projects: []projects.Project{{ID: "p", Name: "Example", Issues: []string{"exampleorg/shop#1"}}}})
	defer fyne.CurrentApp().Quit()
	openProjects(ui)
	if got := labels(projectsPage(ui)); !inOrder(got, "#5  Earlier sprint", "#4  Payment") {
		t.Fatal("the default must put assigned sprints first within the same milestone", got)
	}
}
