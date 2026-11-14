package projects

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// csvHeader names the columns of an issue export: every field the charts are
// drawn from, so the same charts can be drawn again in a spreadsheet.
var csvHeader = []string{
	"number", "title", "url", "repo", "parent", "state", "status", "sprint", "sprint_start",
	"priority", "assignees", "created", "closed", "days_open",
}

func csvDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// WriteIssuesCSV writes items as CSV, one row an issue. State is open, done,
// or not planned; days_open runs to now for an issue still open.
func WriteIssuesCSV(w io.Writer, items []Item, now time.Time) error {
	out := csv.NewWriter(w)
	if err := out.Write(csvHeader); err != nil {
		return err
	}
	for _, it := range items {
		state, end := "open", now
		switch {
		case it.NotPlanned:
			state, end = "not planned", it.ClosedAt
		case it.Closed:
			state, end = "done", it.ClosedAt
		}
		days := ""
		if !it.Created.IsZero() && !end.IsZero() {
			days = strconv.Itoa(int(end.Sub(it.Created).Hours() / 24))
		}
		row := []string{
			strconv.Itoa(it.Number), it.Title, it.URL, it.Repo, it.Parent, state, it.Status,
			it.Sprint, csvDate(it.SprintStart), it.Priority, strings.Join(it.Assignees, ", "),
			csvDate(it.Created), csvDate(it.ClosedAt), days,
		}
		if err := out.Write(row); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// ExportIssues saves items to dir as a CSV named after the project and what
// the list is, stamped with the time so a later export never overwrites it.
// It returns the file's path.
func ExportIssues(dir, project, list string, items []Item, now time.Time) (string, error) {
	part := func(s string) string { return strings.Trim(unsafeName.ReplaceAllString(s, "-"), "-") }
	name := part(project)
	if l := part(list); l != "" {
		name += "-" + l
	}
	path := filepath.Join(dir, fmt.Sprintf("project-%s-%s.csv", strings.ToLower(name), now.Format("2006-01-02-1504")))
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	err = WriteIssuesCSV(file, items, now)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}
