// Package projects is the Projects menu: a project is a named set of parent
// issues (epics), and the page charts how their child issues are getting on.
package projects

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"worklog/internal/shared/backend"
)

// Project is one tab of the Projects menu.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Issues are the parent issues whose child issues make up the project, as
	// owner/repo#number. GitHub caps an issue at 100 children, so a large
	// project names several.
	Issues []string `json:"issues"`
	// History is how many child issues were in review and in progress in each
	// week, keyed by the week's Monday (2006-01-02), as last seen by a
	// refresh that week. GitHub keeps no such history, so the app records it.
	History map[string]WeekCounts `json:"history,omitempty"`
	// Excluded are the parent issues (refs from Issues) left out of the
	// figures: unticked on the page, their child issues count for nothing.
	Excluded []string `json:"excluded,omitempty"`
}

// Counts says whether the parent issue ref is in the figures.
func (p Project) Counts(ref string) bool {
	for _, out := range p.Excluded {
		if strings.EqualFold(out, ref) {
			return false
		}
	}
	return true
}

// SetCounts puts the parent issue ref in the figures or leaves it out.
func (p *Project) SetCounts(ref string, on bool) {
	p.Excluded = slices.DeleteFunc(p.Excluded, func(out string) bool { return strings.EqualFold(out, ref) })
	if !on {
		p.Excluded = append(p.Excluded, ref)
	}
	if len(p.Excluded) == 0 {
		p.Excluded = nil
	}
}

// Counted is the snapshot with only the parent issues p counts.
func (p Project) Counted(snap Snapshot) Snapshot {
	if len(p.Excluded) == 0 {
		return snap
	}
	out := snap
	out.Parents = nil
	for _, parent := range snap.Parents {
		if p.Counts(parent.Ref) {
			out.Parents = append(out.Parents, parent)
		}
	}
	return out
}

// WeekCounts is one week's open work by state.
type WeekCounts struct {
	Review   int `json:"review"`
	Progress int `json:"progress"`
}

// Saved is data\projects.json: the projects in tab order, and the one last
// open.
type Saved struct {
	Projects []Project `json:"projects"`
	Selected string    `json:"selected,omitempty"`
}

// Find returns the project with this ID, or nil.
func (s *Saved) Find(id string) *Project {
	for i := range s.Projects {
		if s.Projects[i].ID == id {
			return &s.Projects[i]
		}
	}
	return nil
}

// Current is the project last open, or the first one when that is gone.
func (s *Saved) Current() *Project {
	if p := s.Find(s.Selected); p != nil {
		return p
	}
	if len(s.Projects) > 0 {
		return &s.Projects[0]
	}
	return nil
}

var fileLock sync.Mutex

func savedPath(store *backend.Store) string { return filepath.Join(store.Dir, "projects.json") }

// Load reads the saved projects. A missing file is no projects yet.
func Load(store *backend.Store) (Saved, error) {
	fileLock.Lock()
	defer fileLock.Unlock()
	var s Saved
	raw, err := os.ReadFile(savedPath(store))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return Saved{}, fmt.Errorf("projects.json could not be read: %w", err)
	}
	return s, nil
}

// Save writes the projects through a temporary file, so a crash part-way
// leaves the old file whole.
func Save(store *backend.Store, s Saved) error {
	fileLock.Lock()
	defer fileLock.Unlock()
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(store.Dir, ".projects-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	_, err = file.Write(append(raw, '\n'))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, savedPath(store)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

var (
	issueURL = regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?github\.com/([\w.-]+)/([\w.-]+)/issues/(\d+)(?:[/?#].*)?$`)
	issueRef = regexp.MustCompile(`^([\w.-]+)/([\w.-]+)#(\d+)$`)
)

// ParseIssues reads pasted issues, one per line or separated by spaces or
// commas: links (https://github.com/owner/repo/issues/12) or refs
// (owner/repo#12). It returns them as refs, each once, in the order pasted,
// and whatever it could not read.
func ParseIssues(text string) (refs, unread []string) {
	seen := map[string]bool{}
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	for _, field := range fields {
		m := issueURL.FindStringSubmatch(field)
		if m == nil {
			m = issueRef.FindStringSubmatch(field)
		}
		if m == nil {
			unread = append(unread, field)
			continue
		}
		ref := fmt.Sprintf("%s/%s#%s", m[1], m[2], m[3])
		if key := strings.ToLower(ref); !seen[key] {
			seen[key] = true
			refs = append(refs, ref)
		}
	}
	return refs, unread
}

// Reordered is projects in the order their names are given. A project the
// order does not name keeps its place after those it does.
func Reordered(projects []Project, names []string) []Project {
	rank := map[string]int{}
	for i, name := range names {
		rank[name] = i
	}
	out := slices.Clone(projects)
	slices.SortStableFunc(out, func(a, b Project) int {
		ra, okA := rank[a.Name]
		rb, okB := rank[b.Name]
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

// AddTabLabel is the tab that adds a project; no project may take its name.
const AddTabLabel = "+"

// CheckName says what is wrong with a project's name, or "" when it is fine.
// Names are what the tabs are told apart by, so no two projects share one.
func (s *Saved) CheckName(name, exceptID string) string {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "Give the project a name."
	case name == AddTabLabel:
		return "That name is taken by the add tab."
	}
	for _, p := range s.Projects {
		if p.ID != exceptID && strings.EqualFold(p.Name, name) {
			return "Another project already has that name."
		}
	}
	return ""
}
