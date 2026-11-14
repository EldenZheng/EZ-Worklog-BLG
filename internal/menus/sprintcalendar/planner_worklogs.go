package sprintcalendar

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

type PlannerDayWork struct {
	Logged []backend.WorklogItem
	Drafts []backend.Row
}

type plannerWorkData struct {
	Days          map[string]PlannerDayWork
	State, detail string
}

func calendarRange(k *shell.KanbanUI) (string, string) {
	switch k.CalendarMode {
	case "Month":
		if from, to, err := backend.MonthBounds(k.Month); err == nil {
			return from, to
		}
	case "Sprint":
		if s := selectedCalendarSprint(k); s.ID != "" {
			return s.StartDate, backend.AddDays(s.StartDate, s.Duration-1)
		}
	}
	return k.Week, backend.AddDays(k.Week, 6)
}

func CalendarMonths(k *shell.KanbanUI) []string {
	from, to := calendarRange(k)
	if !backend.ValidPlannerDate(from) || !backend.ValidPlannerDate(to) {
		return nil
	}
	var months []string
	seen := map[string]bool{}
	for date := from; date <= to && len(date) >= 7; date = backend.AddDays(date, 1) {
		if !seen[date[:7]] {
			seen[date[:7]] = true
			months = append(months, date[:7])
		}
	}
	return months
}

func loadCalendarWorklogs(k *shell.KanbanUI, force bool) {
	if (!k.CalendarActive && !force) || len(backend.ProjectURLs(k.Ui.Cfg)) == 0 {
		return
	}
	k.Ui.EnsureProjectCache()
	for _, month := range CalendarMonths(k) {
		key := shell.StatusCacheKey(month)
		if k.Ui.ProjErr[key] != nil && !force {
			continue
		}
		if from, to, err := backend.MonthBounds(month); err == nil {
			k.Ui.LoadProject(key, from, to, force)
		}
	}
}

func RefreshCalendarSources(k *shell.KanbanUI) {
	k.AutoStartPending = !k.Busy // see maintain
	k.Load(true)
	loadCalendarWorklogs(k, true)
	shell.Hooks.KanbanUIDraw(k)
}

// The calendar's own project toggles govern its totals, independently of the
// Log Work legend. Worklog-only sources remain visible when not on the board.
func calendarOrgVisible(k *shell.KanbanUI, org string) bool {
	known, visible := false, false
	for _, b := range k.Plan.Boards {
		if ref, err := backend.ParseProjectRef(b.URL); err == nil && strings.EqualFold(ref.Login, org) {
			known = true
			visible = visible || k.VisibleBoard(b)
		}
	}
	return !known || visible
}

func CalendarWorkData(k *shell.KanbanUI) plannerWorkData {
	out := plannerWorkData{Days: map[string]PlannerDayWork{}, State: "ok"}
	k.Ui.EnsureProjectCache()
	from, to := calendarRange(k)
	seen := map[string]bool{}
	for _, month := range CalendarMonths(k) {
		key := shell.StatusCacheKey(month)
		items, loaded := k.Ui.ItemsFor(key)
		state := "ok"
		if k.Ui.ProjLoading[key] {
			state = "loading"
		} else if k.Ui.ProjErr[key] != nil {
			state = "error"
			out.detail = k.Ui.ProjErr[key].Error()
		} else if !loaded {
			state = "loading"
		}
		if shell.StateRank(state) > shell.StateRank(out.State) {
			out.State = state
		}
		for _, it := range items {
			if it.Date < from || it.Date > to || !calendarOrgVisible(k, shell.ItemOrg(it)) {
				continue
			}
			if it.URL != "" {
				key := strings.ToLower(it.URL) + "/" + it.Date
				if seen[key] {
					continue
				}
				seen[key] = true
			}
			d := out.Days[it.Date]
			d.Logged = append(d.Logged, it)
			out.Days[it.Date] = d
		}
	}
	if len(backend.ProjectURLs(k.Ui.Cfg)) == 0 {
		out.State = "off"
	}
	rows, err := k.Ui.Store.ReadRows()
	if err != nil {
		out.detail = "Cannot read local worklog drafts: " + err.Error()
		out.State = "error"
	}
	for _, r := range rows {
		if r["pushed_at"] != "" || r["date"] < from || r["date"] > to || !calendarOrgVisible(k, shell.RowOrg(r)) {
			continue
		}
		// A partial push may already have produced the same GitHub worklog.
		if r["issue_url"] != "" && seen[strings.ToLower(r["issue_url"])+"/"+r["date"]] {
			continue
		}
		d := out.Days[r["date"]]
		d.Drafts = append(d.Drafts, r)
		out.Days[r["date"]] = d
	}
	return out
}

func PlannerIssueRef(raw string) string {
	if owner, repo, n, err := backend.SplitIssue(raw); err == nil {
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
	Logged, Draft, Planned, support int
	byOrg, draftByOrg               map[string]int
	// plannedBy is each issue's share of Planned.
	plannedBy map[string]int
}

func DayTotals(k *shell.KanbanUI, date string, candidates []backend.SprintCandidate, work PlannerDayWork) plannerDayTotals {
	t := plannerDayTotals{byOrg: shell.MinutesByOrg(work.Logged), draftByOrg: map[string]int{}, plannedBy: map[string]int{}}
	supportItems := append([]backend.WorklogItem{}, work.Logged...)
	for _, it := range work.Logged {
		t.Logged += it.Minutes
	}
	for _, r := range work.Drafts {
		t.Draft += r.Minutes()
		t.draftByOrg[shell.RowOrg(r)] += r.Minutes()
		// Drafts are waiting work, separate from the time allocated in a plan.
		supportItems = append(supportItems, backend.WorklogItem{Title: r["issue_title"], ParentTitle: r["parent_title"]})
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c.Key] {
			continue
		}
		seen[c.Key] = true
		task := k.Plan.Tasks[c.Key]
		minutes := 0
		for _, block := range task.Blocks {
			if block.Date == date {
				minutes += block.Minutes
			}
		}
		if minutes == 0 {
			continue
		}
		supportItems = append(supportItems, backend.WorklogItem{Title: c.Item.Title})
		// The plan stands as planned: time logged on GitHub or saved in Log
		// Work, or completing the issue, never takes anything off it.
		t.Planned += minutes
		t.plannedBy[c.Key] += minutes
	}
	t.support = shell.SupportPlaceholderFor(k.Ui.Cfg, SupportOn(k.Ui, date), date, supportItems)
	if t.support > 0 {
		t.draftByOrg[shell.SupportPendingKey] += t.support
	}
	return t
}

func (t plannerDayTotals) parts(cfg backend.Config) []shell.MeterPart {
	denominator := backend.Target
	if total := t.Logged + t.Draft + t.Planned + t.support; total > denominator {
		denominator = total
	}
	parts := shell.MeterParts(cfg, t.byOrg, t.draftByOrg, denominator)
	if t.Planned > 0 {
		parts = append(parts, shell.MeterPart{Key: "@plan", Fill: shell.PlannerPlanColor, Weight: float32(t.Planned) / float32(denominator)})
	}
	return parts
}

// worklogPlannedItems keeps locally saved blocks available before the first
// successful board fetch. Only the tooltip's issue details need that fetch.
// Known items, including those hidden by a project filter, never get a fallback.
func worklogPlannedItems(k *shell.KanbanUI) []backend.SprintCandidate {
	items := ScheduledItems(k)
	if k.Loaded {
		return items
	}
	seen := map[string]bool{}
	for _, c := range items {
		seen[c.Key] = true
	}
	for _, b := range k.Plan.Boards {
		for _, it := range b.Items {
			seen[it.Key(b.ID)] = true
		}
	}
	var keys []string
	for key, task := range k.Plan.Tasks {
		if !seen[key] && len(task.Blocks) > 0 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		items = append(items, backend.SprintCandidate{Key: key,
			Item: backend.SprintItem{URL: key, Title: "Fetching Issue..."}})
	}
	return items
}

// PlannedMinutes is the Sprint Calendar's purple figure for a day: the
// planned blocks of all scheduled issues, in full. Neither GitHub worklogs nor
// Log Work drafts against the same issue reduce it.
// This is what the other menus read, so their key's Planned Issues entry hides
// it here. The Sprint Calendar reads DayTotals and keeps its plans on show.
func PlannedMinutes(ui *shell.UI, date string, logged []backend.WorklogItem, drafts []backend.Row) int {
	k := ui.Kanban
	if k == nil || !ui.PlansShown() {
		return 0
	}
	return DayTotals(k, date, worklogPlannedItems(k), PlannerDayWork{Logged: logged, Drafts: drafts}).Planned
}
