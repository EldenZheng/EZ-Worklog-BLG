package backend

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// A weekend support slate is the week's support paperwork filed up front: under
// the team's epic ("Adib, Elden, … : Weekend Support (09 Nov - 15 Nov)") one
// epic of the user's own ("Elden Worklog: Weekend Support (09 Nov - 15 Nov)"),
// and under that a blank worklog for each of the seven days, titled with the
// day's shift and carrying the questions the day is reported against.

// WeekendSupportRemarks is the Worklog (Remarks) every day of the slate carries.
const WeekendSupportRemarks = "Weekend Support"

// WeekendSupportShift is the hours a day is covered: all day on a weekend or a
// public holiday, otherwise either side of office hours.
func WeekendSupportShift(ds string, holiday bool) string {
	if holiday || IsWeekend(ds) {
		return "(8am - 11pm)"
	}
	return "(8am - 9am) and (5pm-11pm)"
}

// IsWeekend reports whether a YYYY-MM-DD date is a Saturday or Sunday.
func IsWeekend(ds string) bool {
	t, err := time.Parse("2006-01-02", ds)
	return err == nil && (t.Weekday() == time.Saturday || t.Weekday() == time.Sunday)
}

// supportDay is a date the way the slate writes it: "09th Nov".
func supportDay(t time.Time) string {
	d := t.Day()
	suffix := "th"
	if d < 11 || d > 13 {
		switch d % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%02d%s %s", d, suffix, t.Format("Jan"))
}

// WeekendSupportRange is the week as the epics name it: "09 Nov - 15 Nov".
func WeekendSupportRange(weekStart string) string {
	t, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		return weekStart
	}
	return t.Format("02 Jan") + " - " + t.AddDate(0, 0, 6).Format("02 Jan")
}

// WeekendSupportEpicTitle names the user's own epic for the week.
func WeekendSupportEpicTitle(cfg Config, weekStart string) string {
	return fmt.Sprintf("%s Worklog: Weekend Support (%s)", DisplayName(cfg), WeekendSupportRange(weekStart))
}

// weekendSupportDayPrefix is the part of a day's title that does not depend on
// whether it was a holiday, so a day filed once is found again either way.
func weekendSupportDayPrefix(cfg Config, ds string) string {
	t, _ := time.Parse("2006-01-02", ds)
	return fmt.Sprintf("%s : Weekend Support Time: %s,", DisplayName(cfg), supportDay(t))
}

// WeekendSupportDayTitle names one day's worklog:
// "Elden : Weekend Support Time: 12th Nov, (8am - 9am) and (5pm-11pm)".
func WeekendSupportDayTitle(cfg Config, ds string, holiday bool) string {
	return weekendSupportDayPrefix(cfg, ds) + " " + WeekendSupportShift(ds, holiday)
}

// WeekendSupportDayBody is one day's description: the shift, then the
// questions the day is reported against, all unanswered.
func WeekendSupportDayBody(ds string, holiday bool) string {
	t, _ := time.Parse("2006-01-02", ds)
	return fmt.Sprintf("Weekend Support Time: %s, %s, %d %s MYT\n\n"+
		"Which customers reported issues before 9 a.m.?\n\nAnswer: \n\n"+
		"Which customers reported issues after 6 p.m.?\n\nAnswer: \n\n"+
		"Active Customers I Communicated With: \nActive Customers (No Direct Communication): \n\n"+
		"Category::\n\nCreated issues ",
		t.Weekday(), supportDay(t), t.Year(), WeekendSupportShift(ds, holiday))
}

var supportRangeRe = regexp.MustCompile(`\((\d{1,2}) ([A-Za-z]{3}) - (\d{1,2}) ([A-Za-z]{3})\)`)

// WeekendSupportWeekFromTitle reads the week out of an epic's title, "… (09 Nov
// - 15 Nov)", as the Monday it starts on. The title carries no year, so the one
// that puts the week nearest today is taken. Empty when there is no range.
func WeekendSupportWeekFromTitle(title string, today time.Time) string {
	m := supportRangeRe.FindStringSubmatch(title)
	if m == nil {
		return ""
	}
	var best time.Time
	for _, y := range []int{today.Year() - 1, today.Year(), today.Year() + 1} {
		t, err := time.Parse("2 Jan 2006", fmt.Sprintf("%s %s %d", m[1], m[2], y))
		if err != nil {
			return ""
		}
		if best.IsZero() || absDuration(t.Sub(today)) < absDuration(best.Sub(today)) {
			best = t
		}
	}
	return WeekStartOf(best.Format("2006-01-02"))
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// SlateResult is what CreateWeekendSupportSlate filed. Notes say what was found
// already there and reused; Problems say what did not get written.
type SlateResult struct {
	EpicURL  string
	DayURLs  []string
	Notes    []string
	Problems []string
}

// CreateWeekendSupportSlate files the user's epic for the week under parentRef
// and a blank worklog under it for each of the seven days. holidays marks the
// weekdays covered all day.
//
// It can be run again: an epic or a day already filed under its title is reused
// rather than filed twice, so a run cut short is finished by running it again.
var CreateWeekendSupportSlate = createWeekendSupportSlate

func createWeekendSupportSlate(cfg Config, parentRef, weekStart string, holidays map[string]bool, progress ...func(FetchProgress)) (SlateResult, error) {
	emit := func(done int, stage string) {
		for _, f := range progress {
			if f != nil {
				f(FetchProgress{Done: done, Total: 100, Stage: stage})
			}
		}
	}
	var res SlateResult
	if strings.TrimSpace(DisplayName(cfg)) == "" {
		return res, GhErr("Set the worklog owner or display name in Settings first; the slate is titled with it.")
	}
	days := make([]string, 7)
	for i := range days {
		days[i] = AddDays(weekStart, i)
	}
	emit(2, "Reading the epic")
	o, r, _, parent, err := GetIssueContext(parentRef)
	if err != nil {
		return res, err
	}
	chosen := pickProject(parent.ProjectItems.Nodes)
	if chosen == nil {
		res.Problems = append(res.Problems, "The epic isn't on a project board, so no fields were set.")
	}

	// The user's epic: reused when the week already has one.
	epicTitle := WeekendSupportEpicTitle(cfg, weekStart)
	var epic IssueNode
	for _, s := range parent.SubIssues.Nodes {
		if strings.EqualFold(strings.TrimSpace(s.Title), epicTitle) {
			res.Notes = append(res.Notes, fmt.Sprintf("Reused #%d, already under the epic.", s.Number))
			res.EpicURL = s.URL
			break
		}
	}
	if res.EpicURL == "" {
		emit(5, "Creating "+epicTitle)
		url, id, problems, err := createSlateIssue(o, r, parent.ID, epicTitle, "", "Epic")
		if err != nil {
			return res, err
		}
		res.EpicURL = url
		res.Problems = append(res.Problems, problems...)
		if chosen != nil {
			res.Problems = append(res.Problems, placeOnBoard(&chosen.Project, id, "", "", "")...)
		}
	}
	emit(12, "Reading "+epicTitle)
	epicRef, err := IssueRefFromAny(res.EpicURL)
	if err == nil {
		_, _, _, epic, err = GetIssueContext(epicRef)
	}
	if err != nil {
		res.Problems = append(res.Problems, "Could not read the new epic back: "+err.Error())
		return res, nil
	}

	owner := strings.TrimSpace(cfg.WorklogOwner)
	for i, ds := range days {
		emit(15+i*12, "Filing "+ds)
		prefix := strings.ToLower(weekendSupportDayPrefix(cfg, ds))
		found := ""
		for _, s := range epic.SubIssues.Nodes {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(s.Title)), prefix) {
				found = s.URL
				break
			}
		}
		if found != "" {
			res.Notes = append(res.Notes, ds+" was already filed; left as it is.")
			res.DayURLs = append(res.DayURLs, found)
			continue
		}
		holiday := holidays[ds] && !IsWeekend(ds)
		url, id, problems, err := createSlateIssue(o, r, epic.ID,
			WeekendSupportDayTitle(cfg, ds, holiday), WeekendSupportDayBody(ds, holiday), "Worklog")
		if err != nil {
			res.Problems = append(res.Problems, ds+": "+err.Error())
			continue
		}
		res.DayURLs = append(res.DayURLs, url)
		res.Problems = append(res.Problems, problems...)
		if chosen != nil {
			res.Problems = append(res.Problems, placeOnBoard(&chosen.Project, id, ds, owner, WeekendSupportRemarks)...)
		}
	}
	emit(100, "Done")
	return res, nil
}

// createSlateIssue files one issue assigned to the user, links it under parentID
// and gives it an issue type. Only a failed create is an error: the issue is
// there whatever goes wrong after, so later steps come back as problems.
func createSlateIssue(owner, repo, parentID, title, body, issueType string) (url, id string, problems []string, err error) {
	out, err := Gh([]string{
		"issue", "create", "--repo", owner + "/" + repo, "--title", title,
		"--body", body, "--assignee", "@me",
	})
	if err != nil {
		return "", "", nil, err
	}
	url = lastLine(out)
	if id, err = viewID(url); err != nil {
		return url, "", []string{title + ": created but could not read its id: " + err.Error()}, nil
	}
	if _, e := Gh([]string{
		"api", "graphql", "-f",
		"query=mutation($p:ID!,$c:ID!){addSubIssue(input:{issueId:$p,subIssueId:$c}){issue{number}}}",
		"-F", "p=" + parentID, "-F", "c=" + id,
	}, "GraphQL-Features: sub_issues"); e != nil {
		problems = append(problems, title+": could not link as sub-issue: "+e.Error())
	}
	if typeID, e := issueTypeID(owner, issueType); e == nil {
		if e := setIssueType(id, typeID); e != nil {
			problems = append(problems, title+": could not set the "+issueType+" type: "+e.Error())
		}
	}
	return url, id, problems, nil
}

// placeOnBoard adds an issue to the board in its Created column and, for a day,
// writes its Worklog date, owner and remarks. Minutes are left for the day's
// own push.
func placeOnBoard(proj *project, contentID, wdate, owner, remarks string) (problems []string) {
	if contentID == "" {
		return nil
	}
	out, err := Gh([]string{
		"api", "graphql", "-f",
		"query=mutation($p:ID!,$c:ID!){addProjectV2ItemById(input:{projectId:$p,contentId:$c}){item{id}}}",
		"-F", "p=" + proj.ID, "-F", "c=" + contentID,
	})
	if err != nil {
		return []string{"Could not add to the project board: " + err.Error()}
	}
	var resp struct {
		Data struct {
			Add struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"addProjectV2ItemById"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(out), &resp); e != nil || resp.Data.Add.Item.ID == "" {
		return []string{"Could not read the project item id."}
	}
	item := resp.Data.Add.Item.ID
	fields := proj.Fields.Nodes
	if f := exactField(fields, "Status"); f != nil {
		if opt := f.option("Created"); opt != "" {
			if _, e := Gh([]string{"project", "item-edit", "--project-id", proj.ID, "--id", item,
				"--field-id", f.ID, "--single-select-option-id", opt}); e != nil {
				problems = append(problems, "Could not set Status to Created: "+e.Error())
			}
		}
	}
	for _, p := range []struct {
		f           *field
		flag, value string
	}{
		{findField(fields, "worklog", "date"), "--date", wdate},
		{findField(fields, "worklog", "owner"), "--text", owner},
		{findField(fields, "worklog", "remark"), "--text", remarks},
	} {
		if p.f == nil || p.value == "" {
			continue
		}
		if _, e := Gh([]string{"project", "item-edit", "--project-id", proj.ID, "--id", item,
			"--field-id", p.f.ID, p.flag, p.value}); e != nil {
			problems = append(problems, fmt.Sprintf("%s: %s", p.f.Name, e.Error()))
		}
	}
	return problems
}
