package main

import (
	"fmt"
	"image/color"
	"net/url"
	"strings"
	"time"
)

var plannerPlanColor = color.NRGBA{R: 151, G: 112, B: 207, A: 255}

type plannerDayWork struct {
	logged []WorklogItem
	drafts []Row
}
type plannerWorkData struct {
	days          map[string]plannerDayWork
	state, detail string
}

func (k *kanbanUI) calendarRange() (string, string) {
	switch k.calendarMode {
	case "Month":
		if from, to, err := monthBounds(k.month); err == nil {
			return from, to
		}
	case "Sprint":
		if s := k.selectedCalendarSprint(); s.ID != "" {
			return s.StartDate, addDays(s.StartDate, s.Duration-1)
		}
	}
	return k.week, addDays(k.week, 6)
}
func (k *kanbanUI) calendarMonths() []string {
	from, to := k.calendarRange()
	if !validPlannerDate(from) || !validPlannerDate(to) {
		return nil
	}
	var months []string
	seen := map[string]bool{}
	for date := from; date <= to && len(date) >= 7; date = addDays(date, 1) {
		if !seen[date[:7]] {
			seen[date[:7]] = true
			months = append(months, date[:7])
		}
	}
	return months
}
func (k *kanbanUI) loadCalendarWorklogs(force bool) {
	if (!k.calendarActive && !force) || len(projectURLs(k.ui.cfg)) == 0 {
		return
	}
	k.ui.ensureProjectCache()
	for _, month := range k.calendarMonths() {
		key := statusCacheKey(month)
		if k.ui.projErr[key] != nil && !force {
			continue
		}
		if from, to, err := monthBounds(month); err == nil {
			k.ui.loadProject(key, from, to, force)
		}
	}
}
func (k *kanbanUI) refreshCalendarSources() {
	k.load(true)
	k.loadCalendarWorklogs(true)
	k.draw()
}

// The calendar's own project toggles govern its totals, independently of the
// Log Work legend. Worklog-only sources remain visible when not on the board.
func (k *kanbanUI) calendarOrgVisible(org string) bool {
	known, visible := false, false
	for _, b := range k.plan.Boards {
		if ref, err := parseProjectRef(b.URL); err == nil && strings.EqualFold(ref.login, org) {
			known = true
			visible = visible || k.visibleBoard(b)
		}
	}
	return !known || visible
}
func (k *kanbanUI) calendarWorkData() plannerWorkData {
	out := plannerWorkData{days: map[string]plannerDayWork{}, state: "ok"}
	k.ui.ensureProjectCache()
	from, to := k.calendarRange()
	seen := map[string]bool{}
	for _, month := range k.calendarMonths() {
		key := statusCacheKey(month)
		items, loaded := k.ui.itemsFor(key)
		state := "ok"
		if k.ui.projLoading[key] {
			state = "loading"
		} else if k.ui.projErr[key] != nil {
			state = "error"
			out.detail = k.ui.projErr[key].Error()
		} else if !loaded {
			state = "loading"
		}
		if stateRank(state) > stateRank(out.state) {
			out.state = state
		}
		for _, it := range items {
			if it.Date < from || it.Date > to || !k.calendarOrgVisible(itemOrg(it)) {
				continue
			}
			if it.URL != "" {
				key := strings.ToLower(it.URL) + "/" + it.Date
				if seen[key] {
					continue
				}
				seen[key] = true
			}
			d := out.days[it.Date]
			d.logged = append(d.logged, it)
			out.days[it.Date] = d
		}
	}
	if len(projectURLs(k.ui.cfg)) == 0 {
		out.state = "off"
	}
	rows, err := k.ui.store.ReadRows()
	if err != nil {
		out.detail = "Cannot read local worklog drafts: " + err.Error()
		out.state = "error"
	}
	for _, r := range rows {
		if r["pushed_at"] != "" || r["date"] < from || r["date"] > to || !k.calendarOrgVisible(rowOrg(r)) {
			continue
		}
		// A partial push may already have produced the same GitHub worklog.
		if r["issue_url"] != "" && seen[strings.ToLower(r["issue_url"])+"/"+r["date"]] {
			continue
		}
		d := out.days[r["date"]]
		d.drafts = append(d.drafts, r)
		out.days[r["date"]] = d
	}
	return out
}

func plannerIssueRef(raw string) string {
	if owner, repo, n, err := splitIssue(raw); err == nil {
		return strings.ToLower(fmt.Sprintf("%s/%s#%d", owner, repo, n))
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Host, "github.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "issues" {
		return ""
	}
	return strings.ToLower(parts[0] + "/" + parts[1] + "#" + parts[3])
}

type plannerDayTotals struct {
	logged, draft, planned, support int
	byOrg, draftByOrg               map[string]int
}

func (k *kanbanUI) dayTotals(date string, candidates []sprintCandidate, work plannerDayWork) plannerDayTotals {
	t := plannerDayTotals{byOrg: minutesByOrg(work.logged), draftByOrg: map[string]int{}}
	credited := map[string]int{}
	supportItems := append([]WorklogItem{}, work.logged...)
	for _, it := range work.logged {
		t.logged += it.Minutes
		if ref := plannerIssueRef(orDefault(it.ParentURL, it.URL)); ref != "" {
			credited[ref] += it.Minutes
		}
	}
	for _, r := range work.drafts {
		t.draft += r.Minutes()
		t.draftByOrg[rowOrg(r)] += r.Minutes()
		if ref := plannerIssueRef(r["issue"]); ref != "" {
			credited[ref] += r.Minutes()
		}
		supportItems = append(supportItems, WorklogItem{Title: r["issue_title"], ParentTitle: r["parent_title"]})
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c.Key] {
			continue
		}
		seen[c.Key] = true
		task := k.plan.Tasks[c.Key]
		minutes := 0
		for _, block := range task.Blocks {
			if block.Date == date {
				minutes += block.Minutes
			}
		}
		if minutes == 0 {
			continue
		}
		supportItems = append(supportItems, WorklogItem{Title: c.Item.Title})
		if plannerIssueFinished(c.Item, task) {
			continue
		}
		ref := plannerIssueRef(c.Item.URL)
		covered := credited[ref]
		if covered > minutes {
			covered = minutes
		}
		t.planned += minutes - covered
		if ref != "" {
			credited[ref] -= covered
		}
	}
	t.support = supportPlaceholderFor(k.ui.cfg, k.calendarSupport, date, supportItems)
	if t.support > 0 {
		t.draftByOrg[strings.ToLower(orgOf(k.ui.cfg.WorklogOwner))] += t.support
	}
	return t
}
func (t plannerDayTotals) parts(cfg Config) []meterPart {
	denominator := target
	if total := t.logged + t.draft + t.planned + t.support; total > denominator {
		denominator = total
	}
	parts := meterParts(cfg, t.byOrg, t.draftByOrg, denominator)
	if t.planned > 0 {
		parts = append(parts, meterPart{key: "@plan", fill: plannerPlanColor, weight: float32(t.planned) / float32(denominator)})
	}
	return parts
}

// Keep range traversal finite if a malformed persisted date reaches the UI.
func validPlannerDate(date string) bool { _, err := time.Parse("2006-01-02", date); return err == nil }
