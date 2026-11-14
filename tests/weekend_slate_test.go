package tests

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"worklog/internal/shared/backend"
)

func TestWeekendSupportSlateTitlesAndBody(t *testing.T) {
	cfg := backend.Config{WorklogOwner: "blg-elden"}
	if got := backend.WeekendSupportEpicTitle(cfg, "2026-11-09"); got != "Elden Worklog: Weekend Support (09 Nov - 15 Nov)" {
		t.Fatalf("epic title: %q", got)
	}
	cases := []struct {
		ds      string
		holiday bool
		want    string
	}{
		{"2026-11-09", true, "Elden : Weekend Support Time: 09th Nov, (8am - 11pm)"},
		{"2026-11-12", false, "Elden : Weekend Support Time: 12th Nov, (8am - 9am) and (5pm-11pm)"},
		{"2026-11-14", false, "Elden : Weekend Support Time: 14th Nov, (8am - 11pm)"},
		{"2026-12-01", false, "Elden : Weekend Support Time: 01st Dec, (8am - 9am) and (5pm-11pm)"},
		{"2026-12-22", false, "Elden : Weekend Support Time: 22nd Dec, (8am - 9am) and (5pm-11pm)"},
	}
	for _, c := range cases {
		if got := backend.WeekendSupportDayTitle(cfg, c.ds, c.holiday); got != c.want {
			t.Errorf("%s: %q, want %q", c.ds, got, c.want)
		}
	}
	want := "Weekend Support Time: Thursday, 12th Nov, 2026 (8am - 9am) and (5pm-11pm) MYT\n\nWhich customers reported issues before 9 a.m.?\n\nAnswer: \n\nWhich customers reported issues after 6 p.m.?\n\nAnswer: \n\nActive Customers I Communicated With: \nActive Customers (No Direct Communication): \n\nCategory::\n\nCreated issues "
	if got := backend.WeekendSupportDayBody("2026-11-12", false); got != want {
		t.Fatalf("body:\n%q\nwant\n%q", got, want)
	}
}

func TestWeekendSupportWeekReadFromEpicTitle(t *testing.T) {
	today := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	if got := backend.WeekendSupportWeekFromTitle("Adib, Elden : Weekend Support (09 Nov - 15 Nov)", today); got != "2026-11-09" {
		t.Fatalf("got %q", got)
	}
	// Early January read in late December belongs to next year.
	if got := backend.WeekendSupportWeekFromTitle("X : Weekend Support (04 Jan - 10 Jan)", time.Date(2026, 12, 30, 0, 0, 0, 0, time.UTC)); got != "2027-01-04" {
		t.Fatalf("got %q", got)
	}
	if got := backend.WeekendSupportWeekFromTitle("no range here", today); got != "" {
		t.Fatalf("got %q", got)
	}
}

// The slate files the epic under the team's, seven days under it, and only the
// days not already there when run again.
func TestCreateWeekendSupportSlate(t *testing.T) {
	original := backend.Gh
	t.Cleanup(func() { backend.Gh = original })

	board := `"projectItems":{"nodes":[{"id":"PI","project":{"id":"P","number":9,"title":"Ops","fields":{"nodes":[
		{"id":"FS","name":"Status","dataType":"SINGLE_SELECT","options":[{"id":"OC","name":"Created"},{"id":"OD","name":"Done"}]},
		{"id":"FD","name":"Worklog (Date)","dataType":"DATE"},
		{"id":"FR","name":"Worklog (Remarks)","dataType":"TEXT"},
		{"id":"FO","name":"Worklog Owner (blg-xxxx)","dataType":"TEXT"}]}}}]}`
	var epicSubs []string
	created := 0
	var titles []string
	var edits []string
	backend.Gh = func(args []string, headers ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case args[0] == "api" && strings.Contains(joined, "num=11586"):
			return `{"data":{"repository":{"issue":{"id":"PARENT","number":11586,"title":"T : Weekend Support (09 Nov - 15 Nov)","url":"u","subIssues":{"nodes":[]},` + board + `}}}}`, nil
		case args[0] == "api" && strings.Contains(joined, "num=200"):
			return `{"data":{"repository":{"issue":{"id":"ID200","number":200,"title":"e","url":"https://github.com/o/r/issues/200","subIssues":{"nodes":[` + strings.Join(epicSubs, ",") + `]},"projectItems":{"nodes":[]}}}}}`, nil
		case args[0] == "issue" && args[1] == "create":
			created++
			for i, a := range args {
				if a == "--title" {
					titles = append(titles, args[i+1])
				}
			}
			return fmt.Sprintf("https://github.com/o/r/issues/%d\n", 199+created), nil
		case args[0] == "issue" && args[1] == "view":
			return `{"id":"ID` + args[2][strings.LastIndex(args[2], "/")+1:] + `"}`, nil
		case strings.Contains(joined, "issueTypes"):
			return `{"data":{"organization":{"issueTypes":{"nodes":[{"id":"TE","name":"Epic"},{"id":"TW","name":"Worklog"}]}}}}`, nil
		case strings.Contains(joined, "addProjectV2ItemById"):
			return `{"data":{"addProjectV2ItemById":{"item":{"id":"ITEM"}}}}`, nil
		case args[0] == "project":
			edits = append(edits, args[len(args)-1])
		}
		return `{}`, nil
	}

	cfg := backend.Config{WorklogOwner: "blg-elden"}
	res, err := backend.CreateWeekendSupportSlate(cfg, "o/r#11586", "2026-11-09", map[string]bool{"2026-11-09": true})
	if err != nil || len(res.Problems) > 0 {
		t.Fatalf("err %v problems %v", err, res.Problems)
	}
	if created != 8 || titles[0] != "Elden Worklog: Weekend Support (09 Nov - 15 Nov)" ||
		titles[1] != "Elden : Weekend Support Time: 09th Nov, (8am - 11pm)" ||
		titles[2] != "Elden : Weekend Support Time: 10th Nov, (8am - 9am) and (5pm-11pm)" {
		t.Fatalf("created %d: %q", created, titles)
	}
	if !strings.Contains(strings.Join(edits, "|"), "Weekend Support") || !strings.Contains(strings.Join(edits, "|"), "blg-elden") {
		t.Fatalf("remarks and owner should be written: %v", edits)
	}

	// Run again with three days already filed under the epic.
	created, titles = 0, nil
	epicSubs = []string{
		`{"id":"a","number":201,"title":"Elden : Weekend Support Time: 09th Nov, (8am - 11pm)","url":"x1"}`,
		`{"id":"b","number":202,"title":"Elden : Weekend Support Time: 10th Nov, (8am - 9am) and (5pm-11pm)","url":"x2"}`,
		`{"id":"c","number":203,"title":"Elden : Weekend Support Time: 11th Nov, (8am - 11pm)","url":"x3"}`,
	}
	backendParent := backend.Gh
	backend.Gh = func(args []string, headers ...string) (string, error) {
		if args[0] == "api" && strings.Contains(strings.Join(args, " "), "num=11586") {
			return `{"data":{"repository":{"issue":{"id":"PARENT","number":11586,"title":"t","url":"u","subIssues":{"nodes":[{"id":"ID200","number":200,"title":"Elden Worklog: Weekend Support (09 Nov - 15 Nov)","url":"https://github.com/o/r/issues/200"}]},` + board + `}}}}`, nil
		}
		return backendParent(args, headers...)
	}
	res, err = backend.CreateWeekendSupportSlate(cfg, "o/r#11586", "2026-11-09", nil)
	if err != nil || created != 4 || len(res.DayURLs) != 7 {
		t.Fatalf("rerun should file only the 4 missing days: created %d, %v, %v", created, titles, err)
	}
}
