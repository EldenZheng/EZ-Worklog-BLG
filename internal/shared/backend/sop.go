package backend

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// SOPStart is the first day of the worklog SOP. From then on a worklog is a
// Worklog sub-issue whose day is the day it was created and whose owner is its
// assignee, and reads take both from the issue itself. Pushes still fill the
// board's Worklog Date and Worklog Owner for now. Entries dated before it keep
// the old method.
const SOPStart = "2026-11-21"

// SOPApplies reports whether an entry dated date falls under the SOP.
func SOPApplies(date string) bool { return strings.TrimSpace(date) >= SOPStart }

// RowPushable reports whether r may be pushed today. A Worklog's day is the
// day it is created, so an entry is first pushed on its own day and no other:
// not early, which would log it on the wrong day, and not late, which would
// backdate it. An entry already sent, wholly or in part, can always be pushed
// again: that writes to the Worklog it already has and creates nothing.
func RowPushable(r Row, today string) bool {
	if strings.TrimSpace(r["pushed_at"]) != "" || strings.TrimSpace(r["issue_url"]) != "" {
		return true
	}
	return strings.TrimSpace(r["date"]) == today
}

// RowIsLegacy reports whether r was waiting to be pushed when the SOP began,
// with commits that were already on GitHub. Those commits cannot be given the
// Worklog's reference any more (most are merged), so the Worklog links them in
// its description instead.
func RowIsLegacy(r Row) bool { return strings.TrimSpace(r["legacy"]) != "" }

// TagLegacyDrafts marks every saved entry that has not been pushed and has
// commits as legacy. It is run once, the first time this version starts; the
// rows tagged keep the mark, and entries made afterwards never get it. It
// only fills the new legacy column; nothing else in a row changes.
func (s *Store) TagLegacyDrafts() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.readRowsLocked()
	if err != nil {
		return 0, err
	}
	tagged := 0
	for _, r := range rows {
		if strings.TrimSpace(r["pushed_at"]) == "" && strings.TrimSpace(r["refs"]) != "" && !RowIsLegacy(r) {
			r["legacy"] = "1"
			tagged++
		}
	}
	if tagged == 0 {
		return 0, nil
	}
	return tagged, s.writeRowsLocked(rows)
}

// The part of a Worklog's description the app writes after the remarks. It
// opens with a hidden marker, so reading the remarks back can cut it off, and
// ends with the entry's own marker, so a retry can find the Worklog an earlier
// attempt created even if GitHub's reply to that attempt was lost.
const worklogAppSection = "<!-- worklog-app -->"

var entryMarkerRe = regexp.MustCompile(`<!-- worklog-entry:([0-9A-Za-z-]+) -->`)

func entryMarker(id string) string { return "<!-- worklog-entry:" + id + " -->" }

// WorklogIssueBody is the description to push r's Worklog with, given the
// remarks the push would otherwise use. It is "" when the entry needs nothing
// added, so pushes outside the SOP send exactly what they always did.
func WorklogIssueBody(r Row, remarks string) string {
	if !SOPApplies(r["date"]) || RowPushMode(r) != "subissue" || strings.TrimSpace(r["id"]) == "" {
		return r["issue_body"]
	}
	base := strings.TrimSpace(OrDefault(r["issue_body"], remarks))
	lines := []string{worklogAppSection}
	// One "Ref <commit link>" line per commit. A legacy entry's commits cannot
	// be changed, so these lines are their link to this Worklog; any other
	// entry's commits are given this Worklog's reference after the push (see
	// commit_rewrite.go), and the lines stand in until they are.
	for _, c := range ParseCommitRefs(r["refs"]) {
		lines = append(lines, "Ref "+commitLink(c))
	}
	lines = append(lines, entryMarker(r["id"]))
	if base == "" {
		return strings.Join(lines, "\n")
	}
	return base + "\n\n" + strings.Join(lines, "\n")
}

func commitLink(c CommitRef) string {
	if c.Repo == "" {
		return c.Sha
	}
	return fmt.Sprintf("https://github.com/%s/commit/%s", c.Repo, c.Sha)
}

// WorklogRemarks is a Worklog description without the part the app added.
func WorklogRemarks(body string) string {
	if i := strings.Index(body, worklogAppSection); i >= 0 {
		body = body[:i]
	}
	return strings.TrimSpace(body)
}

// WorklogRef is how a commit message references a Worklog, for copying.
func WorklogRef(issueURL string) string {
	ref, err := IssueRefFromAny(issueURL)
	if err != nil {
		return ""
	}
	return "Refs " + ref
}

// ---- reading SOP worklogs ----

// sopWorklogsQuery finds the signed-in user's Worklog issues by when they were
// created. Search is used because SOP worklogs carry no board date to filter
// on; it covers every repository at once and returns the minutes with them.
const sopWorklogsQuery = `
query($q:String!,$after:String){
 search(query:$q,type:ISSUE,first:100,after:$after){
  issueCount
  nodes{ ... on Issue {
   id title url body createdAt state
   parent{ title url }
   assignees(first:10){ nodes{ login } }
   issueFieldValues(first:20){ nodes{
    ... on IssueFieldNumberValue { value field{ ... on IssueFieldNumber { name } } }
   } }
  } }
  pageInfo{ hasNextPage endCursor }
 }
}`

type sopWorklogsResponse struct {
	Data struct {
		Search struct {
			IssueCount int `json:"issueCount"`
			Nodes      []struct {
				ID        string `json:"id"`
				Title     string `json:"title"`
				URL       string `json:"url"`
				Body      string `json:"body"`
				CreatedAt string `json:"createdAt"`
				State     string `json:"state"`
				Parent    *struct {
					Title string `json:"title"`
					URL   string `json:"url"`
				} `json:"parent"`
				Assignees struct {
					Nodes []struct {
						Login string `json:"login"`
					} `json:"nodes"`
				} `json:"assignees"`
				IssueFieldValues struct {
					Nodes []struct {
						Value *float64 `json:"value"`
						Field struct {
							Name string `json:"name"`
						} `json:"field"`
					} `json:"nodes"`
				} `json:"issueFieldValues"`
			} `json:"nodes"`
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
		} `json:"search"`
	} `json:"data"`
}

// searchResultCap is the most results GitHub's search returns for one query.
const searchResultCap = 1000

// FetchSOPWorklogs reads the signed-in user's Worklog issues created on the
// working days from..to. A worklog's day is WorkDate of its creation time, the
// same day the app pushed it on. owner is written on each item, as the board
// reader does, so callers filtering by owner keep working.
func FetchSOPWorklogs(owner, from, to string) ([]WorklogItem, error) {
	// GitHub reads created: dates in UTC, and a working day can start on the
	// UTC day before or after, so ask for a day either side and keep only the
	// worklogs whose working day is in range.
	items, err := searchSOPWorklogs(owner, AddDays(from, -1), AddDays(to, 1))
	if err != nil {
		return nil, err
	}
	var out []WorklogItem
	for _, it := range items {
		if it.Date >= from && it.Date <= to {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

func searchSOPWorklogs(owner, from, to string) ([]WorklogItem, error) {
	q := fmt.Sprintf("is:issue type:Worklog assignee:@me created:%s..%s", from, to)
	var items []WorklogItem
	after := ""
	for {
		args := []string{"api", "graphql", "-f", "query=" + sopWorklogsQuery, "-f", "q=" + q}
		if after != "" {
			args = append(args, "-f", "after="+after)
		}
		out, err := Gh(args)
		if err != nil {
			return nil, err
		}
		var resp sopWorklogsResponse
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			return nil, GhErr("Could not decode Worklog issues: %v", err)
		}
		search := resp.Data.Search
		// One query stops at searchResultCap results. Halve a range that holds
		// more, so nothing past the cap is silently left out of the totals.
		if after == "" && search.IssueCount > searchResultCap && from < to {
			mid := midDate(from, to)
			first, err := searchSOPWorklogs(owner, from, mid)
			if err != nil {
				return nil, err
			}
			second, err := searchSOPWorklogs(owner, AddDays(mid, 1), to)
			if err != nil {
				return nil, err
			}
			return append(first, second...), nil
		}
		for _, n := range search.Nodes {
			if n.URL == "" {
				continue
			}
			created, err := time.Parse(time.RFC3339, n.CreatedAt)
			if err != nil {
				continue
			}
			it := WorklogItem{
				ID: n.ID, Body: n.Body, Title: n.Title, URL: n.URL, Owner: owner,
				Date:    WorkDate(created),
				Remarks: WorklogRemarks(n.Body),
			}
			if n.Parent != nil {
				it.ParentTitle, it.ParentURL = n.Parent.Title, n.Parent.URL
			}
			for _, a := range n.Assignees.Nodes {
				it.Assignees = append(it.Assignees, a.Login)
			}
			for _, v := range n.IssueFieldValues.Nodes {
				if v.Value != nil && nameHas(v.Field.Name, "actual", "worklog", "min") {
					it.Minutes = int(*v.Value)
				}
			}
			if strings.EqualFold(n.State, "closed") {
				it.Status = "Done"
			}
			items = append(items, it)
		}
		if !search.PageInfo.HasNextPage || search.PageInfo.EndCursor == "" {
			break
		}
		after = search.PageInfo.EndCursor
	}
	return items, nil
}

func midDate(from, to string) string {
	a, err1 := time.Parse("2006-01-02", from)
	b, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil || !a.Before(b) {
		return from
	}
	days := int(b.Sub(a).Hours() / 24)
	return a.AddDate(0, 0, days/2).Format("2006-01-02")
}

// MergePushedRows adds the SOP entries this app has pushed in from..to that
// the read did not return. Search takes a little while to see a new issue, and
// a Worklog pushed a moment ago must not drop off Status and Report meanwhile.
func MergePushedRows(items []WorklogItem, rows []Row, owner, from, to string) []WorklogItem {
	seen := map[string]bool{}
	for _, it := range items {
		seen[strings.ToLower(strings.TrimSpace(it.URL))] = true
	}
	for _, r := range rows {
		u := strings.TrimSpace(r["issue_url"])
		date := strings.TrimSpace(r["date"])
		if r["pushed_at"] == "" || u == "" || !SOPApplies(date) || date < from || date > to || seen[strings.ToLower(u)] {
			continue
		}
		seen[strings.ToLower(u)] = true
		items = append(items, WorklogItem{
			Date: date, Minutes: r.Minutes(), Owner: owner, Remarks: r["remarks"],
			Title: OrDefault(r["display_title"], r["description"]), URL: u, ParentURL: issueURLOf(r["issue"]),
		})
	}
	return items
}

func issueURLOf(ref string) string {
	owner, repo, number, err := SplitIssue(ref)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, number)
}

// ---- commits that reference a Worklog ----

type worklogParent struct {
	worklog bool
	parent  string // the Worklog's parent, as owner/repo#N
}

var (
	worklogRefMu    sync.Mutex
	worklogRefCache = map[string]worklogParent{}
)

// ResolveWorklogRefs finds commits that reference a Worklog sub-issue, as the
// SOP has them do. Such a commit is already logged: its Worklog is set on it,
// and its issue becomes the Worklog's parent, the issue the work was for, so
// it is never mistaken for a parent of a further Worklog. A failed lookup
// leaves commits as they were.
func ResolveWorklogRefs(commits []Commit) {
	var refs []string
	seen := map[string]bool{}
	for _, c := range commits {
		for _, ref := range commitIssueRefs(c) {
			key := strings.ToLower(ref)
			if seen[key] {
				continue
			}
			seen[key] = true
			worklogRefMu.Lock()
			_, known := worklogRefCache[key]
			worklogRefMu.Unlock()
			if !known {
				refs = append(refs, ref)
			}
		}
	}
	for start := 0; start < len(refs); start += 50 {
		lookupWorklogRefs(refs[start:min(start+50, len(refs))])
	}
	worklogRefMu.Lock()
	defer worklogRefMu.Unlock()
	for i, c := range commits {
		for _, ref := range commitIssueRefs(c) {
			if wp := worklogRefCache[strings.ToLower(ref)]; wp.worklog {
				commits[i].Worklog = ref
				if wp.parent != "" && (c.Issue == "" || strings.EqualFold(c.Issue, ref)) {
					commits[i].Issue = wp.parent
				}
				break
			}
		}
	}
}

// commitIssueRefs is every issue a commit's message names, its own issue first.
func commitIssueRefs(c Commit) []string {
	var refs []string
	seen := map[string]bool{}
	add := func(ref string) {
		if key := strings.ToLower(ref); ref != "" && !seen[key] {
			seen[key] = true
			refs = append(refs, ref)
		}
	}
	add(c.Issue)
	text := OrDefault(c.Full, c.Message)
	for _, ref := range IssueRefsInText(text) {
		add(ref)
	}
	if c.Repo != "" {
		for _, m := range hashRe.FindAllStringSubmatch(text, -1) {
			add(c.Repo + "#" + m[1])
		}
	}
	return refs
}

var safeName = regexp.MustCompile(`^[\w.-]+$`)

func lookupWorklogRefs(refs []string) {
	var parts []string
	var asked []string
	for _, ref := range refs {
		owner, repo, number, err := SplitIssue(ref)
		if err != nil || !safeName.MatchString(owner) || !safeName.MatchString(repo) {
			continue
		}
		parts = append(parts, fmt.Sprintf(`r%d:repository(owner:"%s",name:"%s"){issue(number:%d){issueType{name} parent{url}}}`,
			len(asked), owner, repo, number))
		asked = append(asked, ref)
	}
	if len(parts) == 0 {
		return
	}
	out, err := Gh([]string{"api", "graphql", "-f", "query={" + strings.Join(parts, " ") + "}"}, "GraphQL-Features: sub_issues")
	// A repository the user cannot see fails its own part, not the others,
	// and GitHub still sends the data it has with the errors.
	var resp struct {
		Data map[string]*struct {
			Issue *struct {
				IssueType *struct {
					Name string `json:"name"`
				} `json:"issueType"`
				Parent *struct {
					URL string `json:"url"`
				} `json:"parent"`
			} `json:"issue"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(out), &resp) != nil && err != nil {
		return
	}
	worklogRefMu.Lock()
	defer worklogRefMu.Unlock()
	for i, ref := range asked {
		repo := resp.Data[fmt.Sprintf("r%d", i)]
		if repo == nil || repo.Issue == nil {
			continue
		}
		wp := worklogParent{}
		if repo.Issue.IssueType != nil && strings.EqualFold(repo.Issue.IssueType.Name, "Worklog") {
			wp.worklog = true
			if repo.Issue.Parent != nil {
				wp.parent, _ = IssueRefFromAny(repo.Issue.Parent.URL)
			}
		}
		worklogRefCache[strings.ToLower(ref)] = wp
	}
}

// lookupIssue reads one issue by URL: nil when GitHub says there is no such
// issue, an error when it could not be asked. The two must not be confused:
// "not there" lets a push create a new Worklog, "could not check" must not.
func lookupIssue(url string) (*SubIssue, error) {
	out, err := Gh([]string{"issue", "view", url, "--json", "id,number,title,url,state,body"})
	if err != nil {
		if issueMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	var s SubIssue
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		return nil, GhErr("Could not read %s: %v", url, err)
	}
	if s.ID == "" {
		return nil, nil
	}
	return &s, nil
}

// issueMissing reports whether a gh error says the issue does not exist (it
// was deleted, or the URL was never one), rather than that asking failed.
func issueMissing(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "could not resolve to an issue") || strings.Contains(msg, "not found")
}

// findIssueByMarker finds the issue an earlier attempt on the same entry
// created in owner/repo, by the entry's marker, attached to a parent or not.
// It pages through the user's issues there, newest first, back to the day
// before since (the entry's day: an SOP Worklog is only created on it), so
// however many issues were made after it, it is still found. Search would be
// quicker to ask but can lag behind an issue made a moment ago.
func findIssueByMarker(owner, repo, body, since string) (*SubIssue, error) {
	m := entryMarkerRe.FindString(body)
	if m == "" {
		return nil, nil
	}
	login, err := GhCurrentUser()
	if err != nil {
		return nil, err
	}
	stop := AddDays(since, -1)
	after := ""
	for {
		args := []string{"api", "graphql", "-f", "query=" + issuesByAuthorQuery,
			"-f", "owner=" + owner, "-f", "repo=" + repo, "-f", "login=" + login}
		if after != "" {
			args = append(args, "-f", "after="+after)
		}
		out, err := Gh(args)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Data struct {
				Repository *struct {
					Issues struct {
						Nodes []struct {
							SubIssue
							CreatedAt string `json:"createdAt"`
						} `json:"nodes"`
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
					} `json:"issues"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &resp); err != nil || resp.Data.Repository == nil {
			return nil, GhErr("Could not read your issues in %s/%s.", owner, repo)
		}
		page := resp.Data.Repository.Issues
		older := false
		for i := range page.Nodes {
			if strings.Contains(page.Nodes[i].Body, m) {
				found := page.Nodes[i].SubIssue
				return &found, nil
			}
			if created, err := time.Parse(time.RFC3339, page.Nodes[i].CreatedAt); err == nil && since != "" &&
				created.UTC().Format("2006-01-02") < stop {
				older = true
			}
		}
		if older || !page.PageInfo.HasNextPage || page.PageInfo.EndCursor == "" {
			return nil, nil
		}
		after = page.PageInfo.EndCursor
	}
}

const issuesByAuthorQuery = `
query($owner:String!,$repo:String!,$login:String!,$after:String){
 repository(owner:$owner,name:$repo){
  issues(first:100,after:$after,orderBy:{field:CREATED_AT,direction:DESC},filterBy:{createdBy:$login},states:[OPEN,CLOSED]){
   nodes{ id number title url state body createdAt }
   pageInfo{ hasNextPage endCursor }
  }
 }
}`

// adoptWorklog attaches w, a Worklog an earlier attempt made, to parent and
// returns it for reuse. A failed attach is reported, not fatal: reusing the
// Worklog beats making a second one.
func adoptWorklog(parent IssueNode, w *SubIssue, problems []string) (*SubIssue, []string) {
	if w == nil {
		return nil, problems
	}
	if _, err := Gh([]string{
		"api", "graphql", "-f",
		"query=mutation($p:ID!,$c:ID!){addSubIssue(input:{issueId:$p,subIssueId:$c}){issue{number}}}",
		"-F", "p=" + parent.ID, "-F", "c=" + w.ID,
	}, "GraphQL-Features: sub_issues"); err != nil {
		problems = append(problems, fmt.Sprintf("Could not link #%d as a sub-issue: %v", w.Number, err))
	}
	return w, problems
}

// FindSubIssueByMarker picks out the sub-issue an earlier attempt on the same
// entry created, by the entry's marker in its description. It covers an
// attempt whose reply never arrived, which left no URL on the row to resume.
func FindSubIssueByMarker(node IssueNode, body string) *SubIssue {
	m := entryMarkerRe.FindString(body)
	if m == "" {
		return nil
	}
	for i := range node.SubIssues.Nodes {
		if s := &node.SubIssues.Nodes[i]; strings.Contains(s.Body, m) {
			return s
		}
	}
	return nil
}
