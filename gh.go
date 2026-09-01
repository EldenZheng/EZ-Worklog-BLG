package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GhError is a user-facing failure from a gh invocation.
type GhError struct{ msg string }

func (e *GhError) Error() string        { return e.msg }
func ghErr(f string, a ...any) *GhError { return &GhError{fmt.Sprintf(f, a...)} }

// gh runs the gh CLI and returns stdout, or a GhError.
func gh(args []string, extraHeaders ...string) (string, error) {
	full := append([]string{}, args...)
	for _, h := range extraHeaders {
		full = append(full, "-H", h)
	}
	cmd := exec.Command("gh", full...)
	hideConsole(cmd) // stop a console window flashing per call on Windows
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		var ee *exec.Error
		if errors.As(err, &ee) {
			return "", ghErr("gh CLI not found — install it and run `gh auth login`.")
		}
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		lines := strings.Split(msg, "\n")
		last := "gh command failed"
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			last = strings.TrimSpace(lines[len(lines)-1])
		}
		return "", ghErr("%s", last)
	}
	return out.String(), nil
}

// isNotFound reports whether a gh failure was GitHub answering 404.
//
// gh prints "gh: Not Found (HTTP 404)" and exits non-zero, so the status has to
// be read back out of the message; there is no typed error to match on.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 404") || strings.Contains(msg, "Not Found")
}

var (
	// Both spellings of a cross-repo ref: the URL path form and the shorthand.
	// Only the path form was matched before, so "Refs owner/repo#48" fell to the
	// bare-number fallback below, which threw the owner/repo away and refiled the
	// commit under whatever #48 happened to be in the scraped repo.
	refRe  = regexp.MustCompile(`(?i)refs?\s+([\w.-]+)/([\w.-]+)(?:/issues/|#)(\d+)`)
	hashRe = regexp.MustCompile(`#(\d+)`)
	issRe  = regexp.MustCompile(`^([\w.-]+)/([\w.-]+)#(\d+)$`)
)

func parseIssue(message, fallbackRepo string) string {
	if m := refRe.FindStringSubmatch(message); m != nil {
		return fmt.Sprintf("%s/%s#%s", m[1], m[2], m[3])
	}
	if m := hashRe.FindStringSubmatch(message); m != nil && fallbackRepo != "" {
		return fmt.Sprintf("%s#%s", fallbackRepo, m[1])
	}
	return ""
}

// Commit is one scraped commit. Message is the first line (for display);
// Full is the complete multi-line commit message (used for remarks).
type Commit struct {
	Sha     string `json:"sha"`
	Repo    string `json:"repo"`
	Date    string `json:"date"`
	Message string `json:"message"`
	Full    string `json:"full"`
	Issue   string `json:"issue"`
}

// Group is unlogged commits bucketed by (date, issue).
type Group struct {
	Date    string   `json:"date"`
	Issue   string   `json:"issue"`
	Commits []Commit `json:"commits"`
	Remarks string   `json:"remarks"`
	// Ignored commits are carried rather than dropped, so the list can offer
	// them back. Dropping them here would mean a refetch to undo one click.
	Ignored bool `json:"ignored"`
}

// PendingResult mirrors the /api/pending payload.
type PendingResult struct {
	Groups []Group  `json:"groups"`
	Errors []string `json:"errors"`
	Count  int      `json:"count"`
	Range  []string `json:"range"`
}

// commitJQ shapes each commit into a compact JSON object; we decode it in Go
// rather than juggling delimiters.
const commitJQ = `[.[] | {sha: .sha[0:7], date: .commit.author.date, message: .commit.message}]`

// commitWorkDate is the working day a commit belongs to: its own timestamp in
// the machine's zone, rolled over at workDayStart rather than at midnight.
//
// Midnight was the wrong line to draw. Work carried past it is still the
// evening's work — the entry is logged against that day, and the worklog on the
// board carries that date — but the commit's own timestamp had already turned
// over, so a 1am commit was offered under tomorrow. On a Sunday night that is
// not just the wrong day, it is the wrong week: the overtime leaked forward into
// a week it was never part of, and the week being reported on came out short at
// one end and long at the other.
//
// The same rollover the rest of the app uses, so the day a commit is offered
// under is the day its worklog will be filed on.
func commitWorkDate(ts string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, ts); err == nil {
			return workDate(t.Local())
		}
	}
	// Unparseable: fall back to whatever localDate can make of it rather than
	// losing the commit out of every bucket.
	return localDate(ts)
}

// localDate renders a GitHub timestamp as YYYY-MM-DD in the machine's own zone.
// The two APIs disagree on format — search/commits returns an offset
// ("2026-08-05T17:49:37.000+08:00") while repos/*/commits normalises to Z
// ("2026-08-06T14:58:12Z") — so slicing the first ten characters buckets the
// same commit on different days depending on which path found it.
func localDate(ts string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, ts); err == nil {
			return t.Local().Format("2006-01-02")
		}
	}
	// Only fall back to slicing when the head really is a date, so an
	// unexpected value is left intact rather than silently mangled.
	if len(ts) >= 10 {
		if _, err := time.Parse("2006-01-02", ts[:10]); err == nil {
			return ts[:10]
		}
	}
	return ts
}

// repoCommits lists an author's commits on one ref. An empty ref means the
// repository's default branch; pass a branch name to reach work that has not
// been merged yet.
func repoCommits(repo, ref, author, since, until string) ([]Commit, string) {
	args := []string{
		"api", "repos/" + repo + "/commits", "-X", "GET", "--paginate",
		"-f", "author=" + author, "-f", "since=" + since, "-f", "until=" + until,
		"-f", "per_page=100",
	}
	if ref != "" {
		args = append(args, "-f", "sha="+ref)
	}
	// --paginate concatenates one JSON array per page, so decode a stream of
	// arrays rather than assuming a single one.
	args = append(args, "--jq", commitJQ)
	out, err := gh(args)
	if err != nil {
		// A branch that is gone is not a failure worth putting in front of
		// anyone. repoBranchesSince names every branch the author pushed to,
		// but that log is filtered to the author's own actions — when somebody
		// else merges the pull request and deletes the branch, the deletion is
		// recorded under their name and the branch still looks live here.
		// Listing its commits then 404s. By that point the work is on the
		// default branch, where the commit search has already found it.
		//
		// Only for a named branch: a 404 with no ref means the repository
		// itself cannot be read, which is worth saying.
		if ref != "" && isNotFound(err) {
			return nil, ""
		}
		return nil, fmt.Sprintf("%s: %s", repo, err.Error())
	}
	type rawCommit struct {
		Sha     string `json:"sha"`
		Date    string `json:"date"`
		Message string `json:"message"`
	}
	var raw []rawCommit
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(out)))
	for {
		var page []rawCommit
		if err := dec.Decode(&page); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Sprintf("%s: bad commit data: %v", repo, err)
		}
		raw = append(raw, page...)
	}
	var got []Commit
	for _, c := range raw {
		first := c.Message
		if i := strings.IndexByte(c.Message, 10); i >= 0 {
			first = c.Message[:i]
		}
		got = append(got, Commit{
			Sha: c.Sha, Repo: repo, Date: commitWorkDate(c.Date),
			Message: first, Full: c.Message, Issue: parseIssue(c.Message, repo),
		})
	}
	return got, ""
}

// IssueInfo is the readable identity behind an "owner/repo#123" ref, so a
// pending group can be shown as its issue rather than as a bare number.
type IssueInfo struct {
	Org    string
	Repo   string
	Number int
	Title  string
	URL    string
}

// Label renders the org/repo#number line. Always available, even when the
// title lookup failed.
func (i IssueInfo) Label() string {
	if i.Org == "" || i.Repo == "" {
		return ""
	}
	return fmt.Sprintf("%s / %s #%d", i.Org, i.Repo, i.Number)
}

// fetchIssueInfo resolves one ref to its title. The org, repo, number and a
// usable URL come from the ref itself, so a failed lookup still returns
// something worth displaying alongside the error.
func fetchIssueInfo(ref string) (IssueInfo, error) {
	owner, repo, num, err := splitIssue(ref)
	if err != nil {
		return IssueInfo{}, err
	}
	info := IssueInfo{
		Org: owner, Repo: repo, Number: num,
		URL: fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, num),
	}
	out, err := gh([]string{
		"api", fmt.Sprintf("repos/%s/%s/issues/%d", owner, repo, num),
		"--jq", `{title: .title, url: .html_url}`,
	})
	if err != nil {
		return info, err
	}
	var got struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &got) != nil {
		return info, ghErr("could not read issue %s", ref)
	}
	info.Title = got.Title
	if got.URL != "" {
		info.URL = got.URL
	}
	return info, nil
}

// FetchIssueInfos resolves many refs concurrently, de-duplicated. Errors are
// returned per ref so one unreadable issue does not blank the rest.
func FetchIssueInfos(refs []string) (map[string]IssueInfo, map[string]error) {
	infos := map[string]IssueInfo{}
	errs := map[string]error{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	seen := map[string]bool{}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		wg.Add(1)
		go func(ref string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			info, err := fetchIssueInfo(ref)
			mu.Lock()
			infos[ref] = info
			if err != nil {
				errs[ref] = err
			}
			mu.Unlock()
		}(ref)
	}
	wg.Wait()
	return infos, errs
}

// ---- reading worklogs back from a GitHub Project ----

// WorklogItem is one row in the project, read from its Worklog fields.
//
// Status and Assignees are carried for the week export, which is meant to read
// like the board's own TSV download; nothing else on the tabs uses them.
type WorklogItem struct {
	Date        string
	Minutes     int
	Owner       string
	Remarks     string
	Title       string
	URL         string
	ParentTitle string
	ParentURL   string
	Status      string
	Assignees   []string
}

// Repo is the owner/repo the worklog sits in, read off its own issue URL. The
// board has it as a column of its own; here it is one less thing to fetch.
func (it WorklogItem) Repo() string {
	u := it.URL
	if u == "" {
		u = it.ParentURL
	}
	i := strings.Index(u, "github.com/")
	if i < 0 {
		return ""
	}
	parts := strings.Split(strings.Trim(u[i+len("github.com/"):], "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

var projURLRe = regexp.MustCompile(`(?:github\.com/)?(orgs|users)/([\w.-]+)/projects/(\d+)`)

type projectRef struct {
	ownerKind string
	login     string
	number    int
}

func parseProjectRef(url string) (projectRef, error) {
	m := projURLRe.FindStringSubmatch(url)
	if m == nil {
		return projectRef{}, ghErr("Project URL should look like https://github.com/orgs/<org>/projects/<number>.")
	}
	var number int
	if _, err := fmt.Sscanf(m[3], "%d", &number); err != nil {
		return projectRef{}, ghErr("Invalid project number in %q.", url)
	}
	return projectRef{ownerKind: m[1], login: m[2], number: number}, nil
}

// parseProjectURL pulls the org/user login and project number out of a
// github.com/orgs/<org>/projects/<n> URL (query string ignored).
func parseProjectURL(url string) (login string, number int, err error) {
	ref, err := parseProjectRef(url)
	if err != nil {
		return "", 0, err
	}
	return ref.login, ref.number, nil
}

type pvFieldValue struct {
	Typename string   `json:"__typename"`
	Text     string   `json:"text"`
	Number   *float64 `json:"number"`
	Date     string   `json:"date"`
	// Name is the chosen option of a single-select field, which is how Status
	// arrives. Only single-select values carry one, so it is empty everywhere
	// else and the Field.Name below is what says which field this is.
	Name  string `json:"name"`
	Field struct {
		Name string `json:"name"`
	} `json:"field"`
}

func nameHas(n string, musts ...string) bool {
	n = strings.ToLower(n)
	for _, m := range musts {
		if !strings.Contains(n, m) {
			return false
		}
	}
	return true
}

// projectWorklogsQuery applies the same server-side query syntax as the filter
// bar in GitHub Projects. It returns only the fields Status and Report need.
const projectWorklogsQuery = `
query($login:String!,$number:Int!,$filter:String!,$after:String){
 owner:OWNER(login:$login){
  projectV2(number:$number){
   items(first:100,after:$after,query:$filter){
    nodes{
     content{
      __typename
      ... on Issue { title url parent { title url } assignees(first:10){ nodes{ login } } }
      ... on PullRequest { title url }
      ... on DraftIssue { title }
     }
     fieldValues(first:50){ nodes{
      __typename
      ... on ProjectV2ItemFieldTextValue         { text   field{ ... on ProjectV2FieldCommon { name } } }
      ... on ProjectV2ItemFieldNumberValue       { number field{ ... on ProjectV2FieldCommon { name } } }
      ... on ProjectV2ItemFieldDateValue         { date   field{ ... on ProjectV2FieldCommon { name } } }
      ... on ProjectV2ItemFieldSingleSelectValue { name   field{ ... on ProjectV2FieldCommon { name } } }
     } }
    }
    pageInfo{ hasNextPage endCursor }
   }
  }
 }
}`

type projectItemContent struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Parent *struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	} `json:"parent"`
	Assignees struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`
}

type projectWorklogsResponse struct {
	Data struct {
		Owner *struct {
			Project *struct {
				Items struct {
					Nodes []struct {
						Content     *projectItemContent `json:"content"`
						FieldValues struct {
							Nodes []pvFieldValue `json:"nodes"`
						} `json:"fieldValues"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"items"`
			} `json:"projectV2"`
		} `json:"owner"`
	} `json:"data"`
}

func monthBounds(month string) (string, string, error) {
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return "", "", ghErr("Invalid month %q.", month)
	}
	end := start.AddDate(0, 1, -1)
	return start.Format("2006-01-02"), end.Format("2006-01-02"), nil
}

// supportCarryDays is how far back the report reads for weekend support work
// that has not yet been paid into a set.
//
// A set is seven issues run over consecutive days, so a set caught by the
// cut-off began at most six days before it. One week reaches all of it.
//
// A longer run-up is not a safer one, which is the trap here. The carry is read
// as a remainder — issues seen, modulo seven — and that is only right if the
// window opens after a completed set. Widen it and it eventually opens in the
// middle of an earlier set instead, counting a few issues that were already
// paid: measured over this account, three months of run-up reported a carry of
// zero where the true figure was four, because the window opened two days
// before an older set finished. It also took twenty seconds against four.
const supportCarryDays = 7

// reportFetchBounds is the range the Report tab actually asks GitHub for: the
// payroll period, plus the week of run-up above. The report itself only ever
// shows the period — drawReport splits the two apart again.
func reportFetchBounds(month string) (string, string, error) {
	from, to, err := reportBounds(month)
	if err != nil {
		return "", "", err
	}
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return "", "", err
	}
	return start.AddDate(0, 0, -supportCarryDays).Format("2006-01-02"), to, nil
}

// reportBounds returns the payroll period ending in month: the 21st of the
// previous month through the 20th of the selected month.
func reportBounds(month string) (string, string, error) {
	endMonth, err := time.Parse("2006-01", month)
	if err != nil {
		return "", "", ghErr("Invalid report month %q.", month)
	}
	start := endMonth.AddDate(0, -1, 20)
	end := endMonth.AddDate(0, 0, 19)
	return start.Format("2006-01-02"), end.Format("2006-01-02"), nil
}

func worklogProjectFilter(owner, fromDate, toDate string) string {
	return fmt.Sprintf(`"worklog-owner-(blg-xxxx)":%s "worklog-(date)":>=%s "worklog-(date)":<=%s`,
		owner, fromDate, toDate)
}

func worklogFromNode(content *projectItemContent, values []pvFieldValue) (WorklogItem, bool) {
	it := WorklogItem{}
	if content != nil {
		it.Title, it.URL = content.Title, content.URL
		if content.Parent != nil {
			it.ParentTitle, it.ParentURL = content.Parent.Title, content.Parent.URL
		}
		for _, a := range content.Assignees.Nodes {
			it.Assignees = append(it.Assignees, a.Login)
		}
	}
	for _, fv := range values {
		switch {
		case nameHas(fv.Field.Name, "worklog", "owner"):
			it.Owner = fv.Text
		case nameHas(fv.Field.Name, "worklog", "date"):
			it.Date = fv.Date
		case nameHas(fv.Field.Name, "worklog", "min"):
			if fv.Number != nil {
				it.Minutes = int(*fv.Number)
			}
		case nameHas(fv.Field.Name, "worklog", "remark"):
			it.Remarks = fv.Text
		// Status is the board's own column, not a Worklog field, so it is
		// matched on its own name rather than through the worklog prefix.
		case nameHas(fv.Field.Name, "status"):
			it.Status = fv.Name
		}
	}
	return it, it.Date != ""
}

// FetchProjectWorklogs reads a date range of an owner's worklogs directly from
// the configured project. GitHub applies the owner/date filters before sending
// results, avoiding both a full-board scan and one request per issue.
func FetchProjectWorklogs(cfg Config, ownerFilter, fromDate, toDate string) ([]WorklogItem, error) {
	urls := projectURLs(cfg)
	if len(urls) == 0 {
		return nil, ghErr("Set the Worklog project URL in Settings to load Status and Report from GitHub.")
	}
	owner := strings.TrimSpace(ownerFilter)
	if owner == "" {
		u, err := ghCurrentUser()
		if err != nil {
			return nil, err
		}
		owner = u
	}
	from, err := time.Parse("2006-01-02", fromDate)
	if err != nil {
		return nil, ghErr("Invalid start date %q.", fromDate)
	}
	to, err := time.Parse("2006-01-02", toDate)
	if err != nil || to.Before(from) {
		return nil, ghErr("Invalid end date %q.", toDate)
	}
	filter := worklogProjectFilter(owner, fromDate, toDate)

	// One board per org, so a month is the union of them all. A failure is not
	// swallowed into a partial total: half a month reads as a real month and
	// would under-report the pay, so the first board that cannot be read stops
	// the lot and the tab shows why.
	var items []WorklogItem
	seen := map[string]bool{}
	for _, u := range urls {
		got, err := fetchOneProject(u, owner, filter)
		if err != nil {
			return nil, err
		}
		for _, it := range got {
			// A worklog issue can sit on two boards at once; counting it twice
			// would inflate the day.
			if it.URL != "" && seen[it.URL] {
				continue
			}
			seen[it.URL] = true
			items = append(items, it)
		}
	}
	return items, nil
}

// projectURLs is every board to read, the main one first. Duplicates are
// dropped so listing the same project twice does not double a day's minutes.
func projectURLs(cfg Config) []string {
	var out []string
	seen := map[string]bool{}
	for _, u := range append([]string{cfg.ProjectURL}, cfg.ExtraProjectURLs...) {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

// fetchOneProject reads every page of one board's worklogs for an owner.
func fetchOneProject(projectURL, owner, filter string) ([]WorklogItem, error) {
	ref, err := parseProjectRef(projectURL)
	if err != nil {
		return nil, err
	}
	rootField := map[string]string{"orgs": "organization", "users": "user"}[ref.ownerKind]
	query := strings.Replace(projectWorklogsQuery, "OWNER", rootField, 1)

	var items []WorklogItem
	after := ""
	for {
		args := []string{
			"api", "graphql", "-f", "query=" + query,
			"-f", "login=" + ref.login,
			"-F", fmt.Sprintf("number=%d", ref.number),
			"-f", "filter=" + filter,
		}
		if after != "" {
			args = append(args, "-f", "after="+after)
		}
		out, err := gh(args)
		if err != nil {
			return nil, err
		}
		var resp projectWorklogsResponse
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			return nil, ghErr("Could not decode project worklogs: %v", err)
		}
		if resp.Data.Owner == nil || resp.Data.Owner.Project == nil {
			return nil, ghErr("Could not find project %d for %s.", ref.number, ref.login)
		}
		page := resp.Data.Owner.Project.Items
		for _, node := range page.Nodes {
			it, ok := worklogFromNode(node.Content, node.FieldValues.Nodes)
			if !ok || !strings.EqualFold(strings.TrimSpace(it.Owner), owner) {
				continue
			}
			items = append(items, it)
		}
		if !page.PageInfo.HasNextPage || page.PageInfo.EndCursor == "" {
			break
		}
		after = page.PageInfo.EndCursor
	}
	return items, nil
}

// searchCommits finds an author's commits across a whole org in one indexed
// query — far faster than listing every repo and scanning each.
func searchCommits(org, author, sinceDate, untilDate string) ([]Commit, string) {
	q := fmt.Sprintf("org:%s author:%s author-date:%s..%s", org, author, sinceDate, untilDate)
	out, err := gh([]string{
		"api", "-X", "GET", "search/commits", "--paginate",
		"-f", "q=" + q, "-f", "per_page=100",
		"--jq", `.items[] | {sha: .sha[0:7], date: .commit.author.date, message: .commit.message, repo: .repository.full_name}`,
	})
	if err != nil {
		return nil, fmt.Sprintf("%s: %s", org, err.Error())
	}
	var got []Commit
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var c struct {
			Sha     string `json:"sha"`
			Date    string `json:"date"`
			Message string `json:"message"`
			Repo    string `json:"repo"`
		}
		if json.Unmarshal([]byte(line), &c) != nil {
			continue
		}
		first := c.Message
		if i := strings.IndexByte(c.Message, 10); i >= 0 {
			first = c.Message[:i]
		}
		got = append(got, Commit{
			Sha: c.Sha, Repo: c.Repo, Date: commitWorkDate(c.Date),
			Message: first, Full: c.Message, Issue: parseIssue(c.Message, c.Repo),
		})
	}
	return got, ""
}

// pushRef is one branch an author pushed to, discovered from their event feed.
type pushRef struct{ repo, ref string }

// authorEvents is one pass over the event feed: the branches the author was
// seen pushing to, and every repo they touched by any means at all.
type authorEvents struct {
	refs  []pushRef
	repos []string
}

// userEvents reads the author's event feed and reports both the branches they
// pushed to and the repos they were active in.
//
// Both commit sources are blind to unmerged work: search/commits only indexes
// default branches, and repos/*/commits defaults to one. A branch-per-issue
// workflow therefore goes unreported until merge, so the event feed is used to
// decide which branches are worth listing. It keeps roughly 90 days and 300
// events, which comfortably covers any sane lookback window.
//
// The feed is best-effort, though: GitHub drops PushEvents often enough that a
// commit pushed hours ago can be missing from it while the commit itself is
// plainly on the branch. So the repos are carried out too — FetchPending walks
// their branch tips directly, which does not depend on an event arriving.
//
// Even the repo list is not trustworthy on its own: when the dropped PushEvent
// is the only thing that would have named a repo, the feed never mentions it
// and the walk skips it. reposPushedSince covers that, and this stays as a
// union partner for anything outside the configured orgs.
//
// The event window is padded because a commit is often pushed some time after
// it was authored; commits themselves are still filtered by author date later.
func userEvents(author string, since, until time.Time) (authorEvents, string) {
	out, err := gh([]string{
		"api", "users/" + author + "/events", "-X", "GET", "--paginate",
		"-f", "per_page=100",
		"--jq", `.[] | {at: .created_at, type: .type, repo: .repo.name, ref: .payload.ref, refType: .payload.ref_type}`,
	})
	if err != nil {
		return authorEvents{}, fmt.Sprintf("events for %s: %s", author, err.Error())
	}
	return parseAuthorEvents(out, since.AddDate(0, 0, -2), until.AddDate(0, 0, 2)), ""
}

// parseAuthorEvents turns the streamed event lines into refs and repos. A
// CreateEvent counts as a ref too: pushing a brand new branch reports the
// branch under that type, and the PushEvent that should follow it is exactly
// the one the feed is prone to losing.
func parseAuthorEvents(out string, lo, hi time.Time) authorEvents {
	var got authorEvents
	seenRef := map[pushRef]bool{}
	seenRepo := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e struct {
			At      string `json:"at"`
			Type    string `json:"type"`
			Repo    string `json:"repo"`
			Ref     string `json:"ref"`
			RefType string `json:"refType"`
		}
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339, e.At)
		if err != nil || at.Before(lo) || at.After(hi) || e.Repo == "" {
			continue
		}
		if !seenRepo[e.Repo] {
			seenRepo[e.Repo] = true
			got.repos = append(got.repos, e.Repo)
		}
		if e.Type != "PushEvent" && !(e.Type == "CreateEvent" && e.RefType == "branch") {
			continue
		}
		ref := strings.TrimPrefix(e.Ref, "refs/heads/")
		if ref == "" {
			continue
		}
		p := pushRef{repo: e.Repo, ref: ref}
		if seenRef[p] {
			continue
		}
		seenRef[p] = true
		got.refs = append(got.refs, p)
	}
	return got
}

// repoBranchesSince lists the branches of a repo the author moved since the
// given moment. This is the backstop for a lost PushEvent: the repo's own
// activity log is kept per repository and filtered by actor server-side, so it
// still names the push the global feed dropped.
//
// The alternative — listing every branch and reading its tip date over GraphQL
// — costs a page per hundred branches, which on a repo with thousands of stale
// branches took a minute on its own.
func repoBranchesSince(repo, author string, since time.Time) ([]string, string) {
	out, err := gh([]string{
		"api", "repos/" + repo + "/activity", "-X", "GET",
		"-f", "per_page=100", "-f", "actor=" + author,
		"--jq", `.[] | {ref: .ref, at: .timestamp, kind: .activity_type}`,
	})
	if err != nil {
		return nil, fmt.Sprintf("activity for %s: %s", repo, err.Error())
	}
	return parseActivityRefs(out, since), ""
}

// parseActivityRefs keeps the branches touched on or after since, newest first
// and each named once. A branch created today and pushed to an hour later is
// one branch worth listing, however it got there.
//
// The log runs newest first, so a branch whose latest entry is its deletion is
// gone: asking for its commits is a 404 and an error in front of the user. The
// work on it reached the default branch when the pull request merged, which the
// commit search already covers.
func parseActivityRefs(out string, since time.Time) []string {
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var a struct {
			Ref  string `json:"ref"`
			At   string `json:"at"`
			Kind string `json:"kind"`
		}
		if json.Unmarshal([]byte(line), &a) != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339, a.At)
		if err != nil || at.Before(since) {
			continue
		}
		ref := strings.TrimPrefix(a.Ref, "refs/heads/")
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		if a.Kind == "branch_deletion" {
			continue
		}
		names = append(names, ref)
	}
	return names
}

// reposPushedSince names the owner's repositories whose last push is at or
// after cutoff — the only ones that can be holding unlogged work.
//
// This is what makes the branch sweep independent of the event feed. The feed
// drops PushEvents, and when the dropped push is a repo's only appearance in
// the window, nothing else ever names it: the repo is skipped, its branch is
// never listed, and the commit stays invisible for as long as the feed stays
// wrong. Asking the organisation directly does not have that failure mode.
//
// Cost is one request, not one per repo. The listing is sorted newest-push
// first, so the walk stops at the first repo older than the cutoff — a few
// dozen active repos out of hundreds, on the first page.
func reposPushedSince(owner string, cutoff time.Time) ([]string, string) {
	// An entry in the config may be an organisation or a personal account, and
	// they are different endpoints. Try the org first; most are.
	var lastErr string
	for _, path := range []string{"orgs/" + owner + "/repos", "users/" + owner + "/repos"} {
		names, err := listReposPushedSince(path, cutoff)
		if err == "" {
			return names, ""
		}
		lastErr = err
	}
	return nil, fmt.Sprintf("repos for %s: %s", owner, lastErr)
}

// pageCap bounds the walk at ten pages. A thousand repositories pushed inside a
// lookback window is not a real account, so hitting this means the sort or the
// cutoff is wrong, and paging forever would hang the fetch rather than show it.
const pageCap = 10

func listReposPushedSince(path string, cutoff time.Time) ([]string, string) {
	var names []string
	for page := 1; page <= pageCap; page++ {
		out, err := gh([]string{
			"api", path, "-X", "GET",
			"-f", "sort=pushed", "-f", "direction=desc", "-f", "type=all",
			"-f", "per_page=100", "-f", fmt.Sprintf("page=%d", page),
			"--jq", `.[] | {name: .full_name, at: .pushed_at}`,
		})
		if err != nil {
			return nil, err.Error()
		}
		got, more := parseReposPushedSince(out, cutoff)
		names = append(names, got...)
		if !more {
			break
		}
	}
	return names, ""
}

// parseReposPushedSince reads one page of the listing. It returns the repos
// pushed at or after cutoff, and whether the next page is still worth asking
// for: the page is sorted newest push first, so the first repo older than the
// cutoff ends the walk, and so does a page that was not full.
//
// A repo with no push date at all — one that was created and never used —
// neither counts nor stops the walk. GitHub sorts those to one end, and taking
// a missing date as "older than the cutoff" would end the walk on the first of
// them and lose every active repo behind it.
func parseReposPushedSince(out string, cutoff time.Time) ([]string, bool) {
	var names []string
	seen := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r struct {
			Name string `json:"name"`
			At   string `json:"at"`
		}
		if json.Unmarshal([]byte(line), &r) != nil {
			continue
		}
		seen++
		at, err := time.Parse(time.RFC3339, r.At)
		if err != nil {
			continue
		}
		if at.Before(cutoff) {
			return names, false
		}
		if r.Name != "" {
			names = append(names, r.Name)
		}
	}
	return names, seen >= 100
}

// matchesConfig reports whether a repo falls under a configured entry, which is
// either a bare owner ("bigledger") or an explicit "owner/repo".
func matchesConfig(repo string, entries []string) bool {
	owner := repo
	if i := strings.IndexByte(repo, '/'); i >= 0 {
		owner = repo[:i]
	}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if strings.EqualFold(e, repo) || strings.EqualFold(e, owner) {
			return true
		}
	}
	return false
}

// ghCurrentUser returns the login of the account gh is authenticated as.
func ghCurrentUser() (string, error) {
	out, err := gh([]string{"api", "user", "--jq", ".login"})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// listOrgRepos returns every "owner/repo" in an organisation the user can see,
// most recently pushed first.
//
// The order matters to the repo picker, which puts this list in a dropdown: the
// repo you want is nearly always one you have touched lately, and it should not
// be four hundred names down a list ordered by a letter. The scan that also
// calls this does not care, so one order serves both.
func listOrgRepos(org string) ([]string, error) {
	out, err := gh([]string{
		"api", "orgs/" + org + "/repos", "--paginate",
		"-X", "GET", "-f", "per_page=100", "-f", "type=all",
		"-f", "sort=pushed", "-f", "direction=desc",
		"--jq", ".[].full_name",
	})
	if err != nil {
		// Fall back to a user account if it isn't an org.
		out2, err2 := gh([]string{
			"api", "users/" + org + "/repos", "--paginate",
			"-X", "GET", "-f", "per_page=100",
			"-f", "sort=pushed", "-f", "direction=desc", "--jq", ".[].full_name",
		})
		if err2 != nil {
			return nil, ghErr("could not list repos for '%s': %s", org, err.Error())
		}
		out = out2
	}
	var repos []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			repos = append(repos, s)
		}
	}
	return repos, nil
}

// expandRepos turns bare org names into their repo list, keeping explicit
// owner/repo entries as-is. Duplicates are removed.
func expandRepos(entries []string) ([]string, []string) {
	var out, errs []string
	seen := map[string]bool{}
	add := func(r string) {
		if r != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if strings.Contains(e, "/") {
			add(e) // explicit owner/repo
			continue
		}
		repos, err := listOrgRepos(e)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		for _, r := range repos {
			add(r)
		}
	}
	return out, errs
}

// FetchPending returns unlogged commits in the lookback window, grouped.
func (s *Store) FetchPending(cfg Config) (PendingResult, error) {
	days := cfg.LookbackDays
	if days < 1 {
		days = 7
	}
	// Work in local days. Stamping a local date with "Z" shifted the window by
	// the UTC offset, so commits near midnight fell outside it.
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, -(days - 1))
	endExclusive := start.AddDate(0, 0, days)
	since := start.Format(time.RFC3339)
	until := endExclusive.Format(time.RFC3339)

	var (
		commits []Commit
		errs    []string
		mu      sync.Mutex
		wg      sync.WaitGroup
	)
	// Author defaults to whoever gh is logged in as.
	author := strings.TrimSpace(cfg.GithubAuthor)
	if author == "" {
		if u, err := ghCurrentUser(); err == nil {
			author = u
		} else {
			return PendingResult{}, err
		}
	}

	// Split entries: bare org names use one fast search query; explicit
	// owner/repo entries are scanned directly.
	sinceDate := start.Format("2006-01-02")
	untilDate := endExclusive.AddDate(0, 0, -1).Format("2006-01-02")
	sem := make(chan struct{}, 8)
	collect := func(got []Commit, e string) {
		mu.Lock()
		commits = append(commits, got...)
		if e != "" {
			errs = append(errs, e)
		}
		mu.Unlock()
	}
	scan := func(entry string) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()
		if strings.Contains(entry, "/") {
			collect(repoCommits(entry, "", author, since, until))
		} else {
			collect(searchCommits(entry, author, sinceDate, untilDate))
		}
	}
	for _, entry := range cfg.Repos {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		wg.Add(1)
		go scan(entry)
	}

	// Neither source above sees unmerged branches, so also walk every branch the
	// author worked on in the window and list it explicitly. Results are unioned
	// with the default-branch sweep and de-duplicated by sha.
	//
	// Which repos to walk comes from two places at once, because either alone
	// has a hole. The organisation listing is authoritative but only covers the
	// configured owners; the event feed reaches anything else the author touched
	// but silently loses pushes. Both are one request each, so both are asked.
	// The cutoff is padded a day: a commit authored just before midnight and
	// pushed a moment later still belongs to the window it was written in.
	cutoff := start.AddDate(0, 0, -1)
	var (
		discoverWG sync.WaitGroup
		ev         authorEvents
		refErr     string
		owned      = make([][]string, len(cfg.Repos))
	)
	discoverWG.Add(1)
	go func() {
		defer discoverWG.Done()
		ev, refErr = userEvents(author, start, endExclusive)
	}()
	for i, entry := range cfg.Repos {
		entry = strings.TrimSpace(entry)
		if entry == "" || strings.Contains(entry, "/") {
			continue // an explicit repo is already scanned; there is nothing to list
		}
		i, entry := i, entry
		discoverWG.Add(1)
		go func() {
			defer discoverWG.Done()
			names, e := reposPushedSince(entry, cutoff)
			owned[i] = names
			if e != "" {
				mu.Lock()
				errs = append(errs, e)
				mu.Unlock()
			}
		}()
	}
	discoverWG.Wait()
	if refErr != "" {
		mu.Lock()
		errs = append(errs, refErr)
		mu.Unlock()
	}

	// The union, in config order first so the sweep starts on the repos the user
	// actually named. matchesConfig still gates everything: the feed reports work
	// on repos outside the config, and those are not this worklog's business.
	var sweepRepos []string
	inSweep := map[string]bool{}
	addSweep := func(repo string) {
		if repo == "" || inSweep[repo] || !matchesConfig(repo, cfg.Repos) {
			return
		}
		inSweep[repo] = true
		sweepRepos = append(sweepRepos, repo)
	}
	for _, names := range owned {
		for _, n := range names {
			addSweep(n)
		}
	}
	for _, repo := range ev.repos {
		addSweep(repo)
	}

	// Branch scans are started from the goroutines that discover them, so the
	// bookkeeping is shared and locked. wg.Wait below is only reached once the
	// discovery pass is done, so no scan can be added after the wait starts.
	var scanMu sync.Mutex
	scanned := map[pushRef]bool{}
	scanRef := func(p pushRef) {
		if !matchesConfig(p.repo, cfg.Repos) {
			return
		}
		scanMu.Lock()
		seen := scanned[p]
		scanned[p] = true
		scanMu.Unlock()
		if seen {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			collect(repoCommits(p.repo, p.ref, author, since, until))
		}()
	}
	for _, p := range ev.refs {
		scanRef(p)
	}

	// Ask each candidate repo's own activity log which branches the author moved
	// in the window, and scan those. Each branch is scanned as soon as its repo
	// answers, so the two rounds of calls overlap rather than the whole sweep
	// waiting on the slowest repo.
	var branchWG sync.WaitGroup
	for _, repo := range sweepRepos {
		repo := repo
		branchWG.Add(1)
		go func() {
			defer branchWG.Done()
			sem <- struct{}{}
			names, e := repoBranchesSince(repo, author, cutoff)
			<-sem
			if e != "" {
				mu.Lock()
				errs = append(errs, e)
				mu.Unlock()
			}
			for _, n := range names {
				scanRef(pushRef{repo: repo, ref: n})
			}
		}()
	}
	branchWG.Wait()
	wg.Wait()

	done, err := s.LoggedShas()
	if err != nil {
		return PendingResult{}, err
	}
	skip, err := s.IgnoredShas()
	if err != nil {
		return PendingResult{}, err
	}
	groups := groupPending(commits, done, skip)
	live := 0
	for _, g := range groups {
		if !g.Ignored {
			live++
		}
	}
	return PendingResult{
		Groups: groups, Errors: errs, Count: live,
		Range: []string{sinceDate, untilDate},
	}, nil
}

// groupPending turns the fetched commits into one bubble each, dropping merges,
// duplicates and anything already logged.
//
// Commits on the same issue and day used to share a bubble. That produced a
// single block of ten bullets carrying one sha, with no way to tell which line
// came from which commit — so they are separate now.
func groupPending(commits []Commit, done, ignored map[string]bool) []Group {
	var pending []Commit
	seenSha := map[string]bool{}
	for _, c := range commits {
		if done[c.Sha] || seenSha[c.Sha] {
			continue
		}
		seenSha[c.Sha] = true
		if isMergeCommit(c) {
			continue
		}
		pending = append(pending, c)
	}
	// Newest date first, then issue descending, then sha so repeated fetches
	// present the same order.
	sort.Slice(pending, func(i, j int) bool {
		a, b := pending[i], pending[j]
		if a.Date != b.Date {
			return a.Date > b.Date
		}
		if a.Issue != b.Issue {
			return a.Issue > b.Issue
		}
		return a.Sha < b.Sha
	})
	groups := make([]Group, 0, len(pending))
	for _, c := range pending {
		cs := []Commit{c}
		groups = append(groups, Group{
			Date: c.Date, Issue: c.Issue, Commits: cs,
			Remarks: buildRemarks(cs),
			Ignored: ignored[c.Sha],
		})
	}
	return groups
}

// commitSubject is the one line worth putting on a tile: the commit's first
// real body line, since the subject is often only the "Refs …" bookkeeping
// header that names the issue the tile already shows.
func commitSubject(c Commit) string {
	if body := commitBody(c); len(body) > 0 {
		return body[0]
	}
	if s := strings.TrimSpace(c.Message); s != "" {
		return s
	}
	return "Commit " + c.Sha
}

// refHeaderRe matches the bookkeeping line a commit starts with, e.g.
// "Refs bigledger/blg-sd-senwave-senheng/issues/483:". It names the issue the
// group is already filed under, so repeating it in the remarks says nothing.
// The reference itself must look like an issue so an ordinary subject that
// happens to begin with the word "refs" is not swallowed.
var refHeaderRe = regexp.MustCompile(`(?i)^refs?\s+([\w.-]+/[\w.-]+/issues/\d+|[\w.-]+/[\w.-]+#\d+|#?\d+)\s*:?\s*$`)

// commitBody returns the lines of a commit message worth putting in remarks:
// the body if there is one, otherwise the subject. Leading bullet markers are
// stripped so the caller can apply its own consistently.
func commitBody(c Commit) []string {
	full := strings.TrimSpace(c.Full)
	if full == "" {
		full = strings.TrimSpace(c.Message)
	}
	var body []string
	for _, ln := range strings.Split(full, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || refHeaderRe.MatchString(ln) {
			continue
		}
		ln = strings.TrimSpace(strings.TrimLeft(ln, "-*• \t"))
		if ln != "" {
			body = append(body, ln)
		}
	}
	if len(body) == 0 {
		// Header-only message: fall back to the subject so the sha is still
		// traceable to something.
		if subject := strings.TrimSpace(c.Message); subject != "" {
			body = []string{subject}
		}
	}
	return body
}

func buildRemarks(commits []Commit) string {
	seen := map[string]bool{}
	var lines []string
	for _, c := range commits {
		if isMergeCommit(c) {
			continue
		}
		body := commitBody(c)
		if len(body) == 0 {
			body = []string{"Commit " + c.Sha}
		}
		for _, b := range body {
			key := strings.ToLower(b)
			if seen[key] {
				continue
			}
			seen[key] = true
			// No sha tag. Each bubble is one commit now, so the sha said nothing
			// the entry did not already say, and it cost characters against the
			// 1024 cap. The commit is still reachable: the sha rides on the
			// tile, links out from the editor's commit list, and is stored in
			// the row's refs column.
			lines = append(lines, "- "+b)
		}
	}
	// Deliberately uncapped. Dropping bullets here lost real work silently; the
	// editor shows a live character counter, refuses to push over remarkCap, and
	// offers "Compact with AI", so trimming is the author's call, not ours.
	return strings.Join(lines, "\n")
}

func isMergeCommit(c Commit) bool {
	msg := strings.TrimSpace(c.Full)
	if msg == "" {
		msg = strings.TrimSpace(c.Message)
	}
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	msg = strings.ToLower(strings.TrimSpace(msg))
	return strings.HasPrefix(msg, "merge pull request ") ||
		strings.HasPrefix(msg, "merge branch ") ||
		strings.HasPrefix(msg, "merge remote-tracking branch ")
}

func compactLocally(lines []string) string {
	text := strings.Join(lines, "\n")
	if len(text) <= remarkCap {
		return text
	}
	var kept []string
	for _, ln := range lines {
		trial := strings.Join(append(append([]string{}, kept...), ln), "\n")
		if len(trial) > remarkCap-20 {
			break
		}
		kept = append(kept, ln)
	}
	dropped := len(lines) - len(kept)
	if len(kept) == 0 && len(lines) > 0 {
		return truncateRunes(lines[0], remarkCap)
	}
	if dropped > 0 {
		kept = append(kept, fmt.Sprintf("- (+%d more)", dropped))
	}
	res := strings.Join(kept, "\n")
	if len(res) > remarkCap {
		res = truncateRunes(res, remarkCap)
	}
	return res
}

func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	var b strings.Builder
	for _, r := range s {
		part := string(r)
		if b.Len()+len(part) > max-3 {
			break
		}
		b.WriteString(part)
	}
	return b.String() + "..."
}

// supportSetSize is how many qualifying issues make one complete weekend
// support set, and supportSetPay is what a set is worth in the report's base
// currency.
const (
	supportSetSize = 7
	supportSetPay  = 300
)

// weekendSupportIssues counts the unique qualifying issues in a run of
// worklogs. An issue worked over two days is one issue, not two.
func weekendSupportIssues(items []WorklogItem) (qualifying int) {
	seen := map[string]bool{}
	for _, it := range items {
		title, key := it.Title, it.URL
		if !strings.Contains(strings.ToLower(title), "weekend support") &&
			strings.Contains(strings.ToLower(it.ParentTitle), "weekend support") {
			title, key = it.ParentTitle, it.ParentURL
		}
		if !strings.Contains(strings.ToLower(title), "weekend support") {
			continue
		}
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(title))
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		qualifying++
	}
	return qualifying
}

// weekendSupportBonus works out what a period owes for weekend support, given
// the issues counted inside it and the ones carried in from before it.
//
// A set is seven issues and the seven do not care where the payroll period
// ends. Four can fall before the cut-off and three after, so counting each
// period on its own reaches seven in neither and the set is paid in neither —
// the work simply disappears at the boundary. So the leftover crosses it: what
// the previous period could not make a set out of is counted first here.
//
// carriedIn is a remainder, always under a full set, so a period never pays for
// work an earlier report already paid for.
func weekendSupportBonus(carriedIn, qualifying int) (sets int, bonus float64) {
	sets = (carriedIn + qualifying) / supportSetSize
	return sets, float64(sets * supportSetPay)
}

// ---- project discovery + writing ----

// subIssues is read so a push can resume: if creating the sub-issue succeeded
// last time but a later step failed, the retry reuses it instead of leaving a
// second orphan behind. Requires the sub_issues feature header.
const issueQuery = `
query($owner:String!,$repo:String!,$num:Int!){
 repository(owner:$owner,name:$repo){
  issue(number:$num){ id number title url
   subIssues(first:100){ nodes{ id number title url state } }
   projectItems(first:20){ nodes{ id
     project{ id number title
       fields(first:60){ nodes{
         ... on ProjectV2FieldCommon { id name dataType }
         ... on ProjectV2SingleSelectField { options { id name } } } } } } } } } }
`

func splitIssue(ref string) (owner, repo string, num int, err error) {
	m := issRe.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return "", "", 0, ghErr("Issue ref '%s' should look like owner/repo#123.", ref)
	}
	fmt.Sscanf(m[3], "%d", &num)
	return m[1], m[2], num, nil
}

type fieldOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type field struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	DataType string        `json:"dataType"`
	Options  []fieldOption `json:"options"` // single-select only
}

// option finds a choice on a single-select field by name.
func (f *field) option(name string) string {
	for _, o := range f.Options {
		if strings.EqualFold(strings.TrimSpace(o.Name), name) {
			return o.ID
		}
	}
	return ""
}

// exactField matches a field by its full name, unlike findField's substring
// search — "Status" must not collide with another field that merely contains it.
func exactField(fields []field, name string) *field {
	for i := range fields {
		if strings.EqualFold(strings.TrimSpace(fields[i].Name), name) {
			return &fields[i]
		}
	}
	return nil
}
type project struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Fields struct {
		Nodes []field `json:"nodes"`
	} `json:"fields"`
}
type projectItem struct {
	ID      string  `json:"id"`
	Project project `json:"project"`
}
type subIssue struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

type issueNode struct {
	ID           string `json:"id"`
	Number       int    `json:"number"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	SubIssues    struct {
		Nodes []subIssue `json:"nodes"`
	} `json:"subIssues"`
	ProjectItems struct {
		Nodes []projectItem `json:"nodes"`
	} `json:"projectItems"`
}

// findSubIssue returns the sub-issue with exactly this title, if one is already
// there.
//
// Closed ones count. Setting Status to Done can close the issue through a board
// workflow, so "already pushed" and "closed" routinely mean the same thing —
// skipping closed matches would make every re-push create a duplicate. An open
// match still wins when both exist.
// worklogTitle names the sub-issue for one entry. The first worklog of a day is
// "Worklog: 2026-08-30"; a second entry against the same issue on the same day
// is "Worklog: 2026-08-30 Part 2", then Part 3, and so on.
//
// Two sub-issues may share a title as far as GitHub is concerned, but not as far
// as anyone reading the list is concerned: a day with three identical rows says
// nothing about which is which, and the board shows the title beside the
// minutes. The number is taken from the highest part already filed rather than
// from a count, so a deleted Part 2 does not hand its name to the next entry.
func worklogTitle(node issueNode, wdate string) string {
	base := "Worklog: " + wdate
	highest := 0
	for i := range node.SubIssues.Nodes {
		if n := worklogPartOf(node.SubIssues.Nodes[i].Title, base); n > highest {
			highest = n
		}
	}
	if highest == 0 {
		return base
	}
	return fmt.Sprintf("%s Part %d", base, highest+1)
}

// worklogPartOf reads which part of a day's worklog a title names: 1 for the
// unnumbered first one, N for "… Part N", and 0 for a title that is not this
// day's worklog at all.
func worklogPartOf(title, base string) int {
	title = strings.TrimSpace(title)
	if strings.EqualFold(title, base) {
		return 1
	}
	rest, ok := cutPrefixFold(title, base+" Part ")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// cutPrefixFold is strings.CutPrefix ignoring case. Both sides are ASCII here,
// so comparing byte-for-byte over the prefix length is safe.
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", false
	}
	return s[len(prefix):], true
}

// findSubIssueByURL picks out the sub-issue an earlier attempt on one entry
// created, by the URL that attempt recorded on the row.
//
// The URL, not the title: every worklog for a given day is titled
// "Worklog: <date>", so a title match cannot tell one entry's sub-issue from
// another's, and picking the wrong one means writing this entry's minutes over
// that one's. Only the row that created a sub-issue knows which is its own.
//
// State is not consulted. A worklog sub-issue is closed as soon as the board
// moves it to Done, so by the time anything is retried the entry's own
// sub-issue is usually closed — skipping closed ones would create a second.
func findSubIssueByURL(node issueNode, url string) *subIssue {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}
	for i := range node.SubIssues.Nodes {
		if s := &node.SubIssues.Nodes[i]; strings.EqualFold(strings.TrimSpace(s.URL), url) {
			return s
		}
	}
	return nil
}

func getIssueContext(ref string) (owner, repo string, num int, node issueNode, err error) {
	owner, repo, num, err = splitIssue(ref)
	if err != nil {
		return
	}
	out, e := gh([]string{
		"api", "graphql", "-f", "query=" + issueQuery,
		"-F", "owner=" + owner, "-F", "repo=" + repo, "-F", fmt.Sprintf("num=%d", num),
	}, "GraphQL-Features: sub_issues")
	if e != nil {
		err = e
		return
	}
	var resp struct {
		Data struct {
			Repository struct {
				Issue *issueNode `json:"issue"`
			} `json:"repository"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(out), &resp); e != nil {
		err = ghErr("could not parse issue response: %v", e)
		return
	}
	if resp.Data.Repository.Issue == nil {
		err = ghErr("Issue %s not found.", ref)
		return
	}
	node = *resp.Data.Repository.Issue
	return
}

// pickProject chooses the item whose project actually has Worklog fields.
func pickProject(items []projectItem) *projectItem {
	var best *projectItem
	for i := range items {
		for _, f := range items[i].Project.Fields.Nodes {
			if strings.HasPrefix(strings.ToLower(f.Name), "worklog") {
				return &items[i]
			}
		}
		if best == nil {
			best = &items[i]
		}
	}
	return best
}

func findField(fields []field, musts ...string) *field {
	for i := range fields {
		n := strings.ToLower(fields[i].Name)
		ok := true
		for _, m := range musts {
			if !strings.Contains(n, m) {
				ok = false
				break
			}
		}
		if ok {
			return &fields[i]
		}
	}
	return nil
}

func setFields(projectID, itemID string, fields []field, wdate, owner string, mins int, remarks string) (done, missing []string) {
	if len(remarks) > remarkCap {
		remarks = remarks[:remarkCap]
	}
	type plan struct {
		f     *field
		flag  string
		value string
	}
	plans := []plan{
		{findField(fields, "worklog", "date"), "--date", wdate},
		{findField(fields, "worklog", "owner"), "--text", owner},
		{findField(fields, "worklog", "min"), "--number", fmt.Sprintf("%d", mins)},
		{findField(fields, "worklog", "remark"), "--text", remarks},
	}
	for _, p := range plans {
		if p.f == nil || p.value == "" {
			continue
		}
		_, err := gh([]string{
			"project", "item-edit", "--project-id", projectID, "--id", itemID,
			"--field-id", p.f.ID, p.flag, p.value,
		})
		if err != nil {
			missing = append(missing, fmt.Sprintf("%s: %s", p.f.Name, err.Error()))
		} else {
			done = append(done, p.f.Name)
		}
	}
	return
}

var (
	issueTypeMu    sync.Mutex
	issueTypeCache = map[string]string{}
)

// issueTypeID resolves an organisation issue type by name.
//
// gh 2.93 has no --type flag on `issue create`, so the type has to be applied
// afterwards over GraphQL. Results are cached; the list rarely changes.
func issueTypeID(org, name string) (string, error) {
	key := strings.ToLower(org + "/" + name)
	issueTypeMu.Lock()
	id, cached := issueTypeCache[key]
	issueTypeMu.Unlock()
	if cached {
		return id, nil
	}
	out, err := gh([]string{
		"api", "graphql", "-f",
		"query=query($org:String!){organization(login:$org){issueTypes(first:50){nodes{id name}}}}",
		"-F", "org=" + org,
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		Data struct {
			Organization struct {
				IssueTypes struct {
					Nodes []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"issueTypes"`
			} `json:"organization"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(out), &resp); e != nil {
		return "", ghErr("could not read issue types for %s: %v", org, e)
	}
	found := ""
	issueTypeMu.Lock()
	for _, t := range resp.Data.Organization.IssueTypes.Nodes {
		issueTypeCache[strings.ToLower(org+"/"+t.Name)] = t.ID
		if strings.EqualFold(t.Name, name) {
			found = t.ID
		}
	}
	issueTypeMu.Unlock()
	if found == "" {
		return "", fmt.Errorf("%w: %s has no %q issue type", errNoIssueType, org, name)
	}
	return found, nil
}

// errNoIssueType marks an organisation that simply does not define the type, as
// opposed to a lookup that failed. Not every org carries a Worklog type —
// BigLedger-Support has only Task, Bug and Feature — and an untyped sub-issue
// still holds its worklog fields and its Done status, so this must not be
// counted as a failed push or the row waits to be retried forever.
var errNoIssueType = errors.New("issue type not defined")

// setIssueType applies an organisation issue type to an issue.
func setIssueType(issueID, typeID string) error {
	_, err := gh([]string{
		"api", "graphql", "-f",
		"query=mutation($i:ID!,$t:ID!){updateIssueIssueType(input:{issueId:$i,issueTypeId:$t}){issue{number}}}",
		"-F", "i=" + issueID, "-F", "t=" + typeID,
	})
	return err
}

// setStatusDone moves the board's Status column to Done. A worklog is written
// after the work is finished, so leaving it in the default column would keep
// reporting it as outstanding.
func setStatusDone(projectID, itemID string, fields []field) (string, error) {
	f := exactField(fields, "Status")
	if f == nil {
		return "", nil // board has no Status column; nothing to do
	}
	opt := f.option("Done")
	if opt == "" {
		return "", ghErr("the Status field has no \"Done\" option")
	}
	_, err := gh([]string{
		"project", "item-edit", "--project-id", projectID, "--id", itemID,
		"--field-id", f.ID, "--single-select-option-id", opt,
	})
	if err != nil {
		return "", err
	}
	return f.Name, nil
}

// pushToProject puts a sub-issue on the board and writes its Worklog fields.
// Both the create and the resume path go through here so a retry does exactly
// what the first attempt would have done.
//
// addProjectV2ItemById returns the existing item when the content is already on
// the board, so calling it again after a partial failure is safe.
func pushToProject(issueURL, contentID string, items []projectItem,
	wdate, owner string, mins int, remarks string, notes, problems []string) (PushResult, error) {

	chosen := pickProject(items)
	if chosen == nil {
		return PushResult{
			URL: issueURL, ItemID: "", Notes: notes,
			Problems: append(problems, "Parent issue isn't in a project, so no fields were set."),
		}, nil
	}
	proj := &chosen.Project
	out, err := gh([]string{
		"api", "graphql", "-f",
		"query=mutation($p:ID!,$c:ID!){addProjectV2ItemById(input:{projectId:$p,contentId:$c}){item{id}}}",
		"-F", "p=" + proj.ID, "-F", "c=" + contentID,
	})
	if err != nil {
		return PushResult{}, err
	}
	var addResp struct {
		Data struct {
			Add struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"addProjectV2ItemById"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(out), &addResp); e != nil {
		return PushResult{}, ghErr("could not read project item id: %v", e)
	}
	targetItem := addResp.Data.Add.Item.ID

	done, failed := setFields(proj.ID, targetItem, proj.Fields.Nodes, wdate, owner, mins, remarks)
	if len(done) == 0 {
		problems = append(problems, "No Worklog fields matched on this project board.")
	}
	problems = append(problems, failed...)

	// Status is set last: a failure here should not cost the field values.
	if name, err := setStatusDone(proj.ID, targetItem, proj.Fields.Nodes); err != nil {
		problems = append(problems, "Could not set Status to Done: "+err.Error())
	} else if name != "" {
		done = append(done, name+" = Done")
	}
	return PushResult{
		URL: issueURL, ItemID: targetItem,
		FieldsSet: done, Notes: notes, Problems: problems,
	}, nil
}

// PushResult is the outcome of writing a worklog to GitHub.
//
// Notes are informational ("reused an existing sub-issue"); Problems are things
// that did not get written. Only a push with no Problems counts as complete —
// marking a partial push as done is how entries ended up on the board with no
// field values and no way to notice.
type PushResult struct {
	URL       string   `json:"url"`
	ItemID    string   `json:"item_id"`
	FieldsSet []string `json:"fields_set"`
	Notes     []string `json:"notes"`
	Problems  []string `json:"problems"`
}

// Complete reports whether everything the push set out to write landed.
func (p PushResult) Complete() bool { return len(p.Problems) == 0 }

func viewID(url string) (string, error) {
	out, err := gh([]string{"issue", "view", url, "--json", "id"})
	if err != nil {
		return "", err
	}
	var v struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return "", ghErr("could not read new issue id: %v", err)
	}
	return v.ID, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// PushEntry writes a worklog to GitHub (subissue or issue mode).
//
// resume is the sub-issue URL this same entry already created, taken off the
// row's issue_url. Empty means there is nothing to pick back up and a new
// sub-issue is created — including when another entry has already filed one
// under the same title for the same day.
func PushEntry(cfg Config, ref, wdate, owner string, mins int, remarks, mode, resume string) (PushResult, error) {
	o, r, _, issue, err := getIssueContext(ref)
	if err != nil {
		return PushResult{}, err
	}
	items := issue.ProjectItems.Nodes
	var notes, problems []string
	var targetItem string
	var proj *project
	resultURL := issue.URL

	body := remarks
	if len(body) > remarkCap {
		body = body[:remarkCap]
	}

	if mode == "subissue" {
		var createdURL string

		// Creating the sub-issue is the one step that cannot be undone, so a
		// retry must never repeat it. If a previous attempt on *this entry* got
		// that far, pick that sub-issue back up — resume is the URL the row
		// recorded when it did.
		//
		// It used to be found by title instead, which cannot tell a retry from a
		// second entry logged against the same issue on the same day: both want
		// "Worklog: 2026-08-30". The second push landed on the first one's board
		// item and overwrote its minutes and its remarks, so a 300 and a 240
		// showed up as a 240 and the 300's remarks were gone. Two entries are
		// two sub-issues now, and only the row that created one can claim it.
		if existing := findSubIssueByURL(issue, resume); existing != nil {
			notes = append(notes, fmt.Sprintf(
				"Reused sub-issue #%d from an earlier attempt on this entry.", existing.Number))
			return pushToProject(existing.URL, existing.ID, items, wdate, owner, mins, remarks, notes, problems)
		}

		// Numbered only if the day already has one on this issue, so a normal
		// day's worklog keeps its plain title and a second entry is named for
		// what it is rather than turning up as a twin of the first.
		title := worklogTitle(issue, wdate)
		if part := worklogPartOf(title, "Worklog: "+wdate); part > 1 {
			notes = append(notes, fmt.Sprintf(
				"This issue already had a worklog for %s, so this one was filed as part %d.",
				wdate, part))
		}

		// The parent link and the issue type go through GraphQL rather than
		// `gh issue create` flags: gh 2.93 has neither --parent nor --type, so
		// passing them made every create fail into a fallback that silently
		// dropped the type. GraphQL works on any gh version.
		out, err := gh([]string{
			"issue", "create", "--repo", o + "/" + r, "--title", title,
			"--body", body, "--assignee", "@me",
		})
		if err != nil {
			return PushResult{}, err
		}
		createdURL = lastLine(out)

		newID, e := viewID(createdURL)
		if e != nil {
			// The sub-issue exists whatever happened next, so its URL goes back
			// with the failure rather than being dropped on the floor. That is
			// what lets the retry above find it; returning a bare error here
			// left the row with nothing to resume from and the next attempt
			// created a second sub-issue for the same entry.
			return PushResult{URL: createdURL, Notes: notes, Problems: append(problems,
				"Created the sub-issue but could not read its id: "+e.Error())}, nil
		}
		if _, e := gh([]string{
			"api", "graphql", "-f",
			"query=mutation($p:ID!,$c:ID!){addSubIssue(input:{issueId:$p,subIssueId:$c}){issue{number}}}",
			"-F", "p=" + issue.ID, "-F", "c=" + newID,
		}, "GraphQL-Features: sub_issues"); e != nil {
			problems = append(problems, "Could not link as sub-issue: "+e.Error())
		}
		typeID, typeErr := issueTypeID(o, "Worklog")
		switch {
		case errors.Is(typeErr, errNoIssueType):
			notes = append(notes, o+" has no Worklog issue type, so the sub-issue was left untyped.")
		case typeErr != nil:
			problems = append(problems, "Could not set the Worklog issue type: "+typeErr.Error())
		default:
			if e := setIssueType(newID, typeID); e != nil {
				problems = append(problems, "Could not set the Worklog issue type: "+e.Error())
			}
		}
		return pushToProject(createdURL, newID, items, wdate, owner, mins, remarks, notes, problems)
	} else {
		chosen := pickProject(items)
		if chosen == nil {
			return PushResult{}, ghErr("%s isn't on a project board, so there are no Worklog fields.", ref)
		}
		proj = &chosen.Project
		targetItem = chosen.ID
	}

	// "issue" mode: the ref itself is the board item, so only fields are set.
	done, failed := setFields(proj.ID, targetItem, proj.Fields.Nodes, wdate, owner, mins, remarks)
	if len(done) == 0 {
		problems = append(problems, "No Worklog fields matched on this project board.")
	}
	problems = append(problems, failed...)
	if name, err := setStatusDone(proj.ID, targetItem, proj.Fields.Nodes); err != nil {
		problems = append(problems, "Could not set Status to Done: "+err.Error())
	} else if name != "" {
		done = append(done, name+" = Done")
	}
	return PushResult{
		URL: resultURL, ItemID: targetItem,
		FieldsSet: done, Notes: notes, Problems: problems,
	}, nil
}
