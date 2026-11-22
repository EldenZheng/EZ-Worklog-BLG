package tests

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestSOPPushOnlyOnTheEntrysDay(t *testing.T) {
	today := "2026-12-01"
	if !backend.RowPushable(backend.Row{"date": today}, today) {
		t.Fatal("an entry should push on its own day")
	}
	for _, day := range []string{"2026-11-30", "2026-12-02"} {
		if backend.RowPushable(backend.Row{"date": day}, today) {
			t.Fatalf("an unpushed entry for %s must not push on %s", day, today)
		}
	}
	if !backend.RowPushable(backend.Row{"date": "2026-11-30", "pushed_at": "2026-11-30T10:00:00"}, today) {
		t.Fatal("an entry already pushed can be pushed again any day")
	}
	if !backend.RowPushable(backend.Row{"date": "2026-11-30", "issue_url": "https://github.com/o/r/issues/9"}, today) {
		t.Fatal("a partly pushed entry can be retried any day")
	}

	logwork.PushDay = func() string { return today }
	t.Cleanup(func() { logwork.PushDay = nil })
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Cfg: backend.Config{WorklogOwner: "test"}}
	tile := func(r backend.Row) bool {
		r["minutes"], r["issue"], r["owner"] = "60", "exampleorg/app#1", "test"
		return buttonNamed(logwork.RowTile(ui, r, func() {}), "Push") != nil
	}
	if !tile(backend.Row{"date": today}) {
		t.Fatal("today's entry should show Push")
	}
	if tile(backend.Row{"date": "2026-12-02"}) || tile(backend.Row{"date": "2026-11-30"}) {
		t.Fatal("an entry for another day should not show Push")
	}
	pushed := backend.Row{"date": "2026-11-30", "pushed_at": "2026-11-30T10:00:00", "minutes": "60", "issue": "exampleorg/app#1"}
	if buttonNamed(logwork.RowTile(ui, pushed, func() {}), "Re-push") == nil {
		t.Fatal("a pushed entry keeps Re-push")
	}
}

func TestSOPPushRefusesAnotherDayBeforeTouchingGitHub(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	backend.Gh = func(args []string, _ ...string) (string, error) {
		t.Fatalf("GitHub was called: %v", args)
		return "", nil
	}
	tomorrow := backend.AddDays(backend.WorkDate(time.Now()), 1)
	if !backend.SOPApplies(tomorrow) {
		t.Skip("the SOP has not started")
	}
	_, err := backend.PushEntry(backend.Config{}, "o/r#1", tomorrow, "test", 60, "", "", "subissue", "", "")
	if err == nil || !strings.Contains(err.Error(), "own day") {
		t.Fatalf("a push for another day should be refused, got %v", err)
	}
}

func TestSOPWorklogBodyLinksLegacyCommitsAndMarksTheEntry(t *testing.T) {
	legacy := backend.Row{"id": "abc123", "date": "2026-11-24", "mode": "subissue", "legacy": "1",
		"refs": "exampleorg/app@1a2b3c4,5d6e7f8"}
	body := backend.WorklogIssueBody(legacy, "Fixed the export")
	for _, want := range []string{"Fixed the export", "\nRef https://github.com/exampleorg/app/commit/1a2b3c4\n", "\nRef 5d6e7f8\n", "worklog-entry:abc123"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body is missing %q:\n%s", want, body)
		}
	}
	if got := backend.WorklogRemarks(body); got != "Fixed the export" {
		t.Fatalf("remarks read back as %q", got)
	}
	current := backend.Row{"id": "def456", "date": "2026-11-24", "mode": "subissue", "refs": "exampleorg/app@1a2b3c4"}
	// Its commits get the same Ref lines, until they carry the Worklog's
	// reference themselves.
	if body := backend.WorklogIssueBody(current, "x"); !strings.Contains(body, "\nRef https://github.com/exampleorg/app/commit/1a2b3c4\n") ||
		strings.Contains(body, "Commits") || !strings.Contains(body, "worklog-entry:def456") {
		t.Fatalf("a non-legacy entry lists its commits and is marked:\n%s", body)
	}
	before := backend.Row{"id": "g", "date": "2026-11-20", "mode": "subissue", "issue_body": "kept", "legacy": "1", "refs": "a@b"}
	if body := backend.WorklogIssueBody(before, "x"); body != "kept" {
		t.Fatalf("an entry before the SOP pushes as it always did, got %q", body)
	}
	node := backend.IssueNode{}
	node.SubIssues.Nodes = []backend.SubIssue{{URL: "u1", Body: "other"}, {URL: "u2", Body: "x\n<!-- worklog-entry:def456 -->"}}
	if s := backend.FindSubIssueByMarker(node, backend.WorklogIssueBody(current, "x")); s == nil || s.URL != "u2" {
		t.Fatalf("the retry should find the Worklog by its marker, got %+v", s)
	}
}

func TestSOPTagsPendingCommitEntriesLegacyOnce(t *testing.T) {
	dir := tempDir(t)
	store := backend.NewStore(dir)
	made, err := store.AppendRows([]backend.Row{
		{"date": "2026-11-24", "refs": "o/r@aaa", "minutes": "60"},
		{"date": "2026-11-20", "refs": "o/r@bbb", "pushed_at": "2026-11-20T10:00:00"},
		{"date": "2026-11-25", "minutes": "30"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := store.TagLegacyDrafts(); err != nil || n != 1 {
		t.Fatalf("tagged %d, err %v", n, err)
	}
	rows, _ := store.ReadRows()
	for _, r := range rows {
		if want := r["id"] == made[0]["id"]; backend.RowIsLegacy(r) != want {
			t.Fatalf("row %v legacy=%v", r, backend.RowIsLegacy(r))
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "worklog.csv.pre-sop-legacy.bak")); err == nil {
		t.Fatal("tagging must not leave a backup in data")
	}
	if n, _ := store.TagLegacyDrafts(); n != 0 {
		t.Fatalf("a second run tagged %d", n)
	}
}

func TestSOPWorklogsReadByCreationDayAndMinutesField(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	var queries []string
	backend.Gh = func(args []string, _ ...string) (string, error) {
		joined := strings.Join(args, " ")
		for _, a := range args {
			if strings.HasPrefix(a, "q=") {
				queries = append(queries, strings.TrimPrefix(a, "q="))
			}
		}
		if !strings.Contains(joined, "search(") {
			t.Fatalf("only the search should run for SOP days: %v", args)
		}
		return `{"data":{"search":{"issueCount":2,"nodes":[
{"id":"I1","title":"Worklog: 2026-11-24","url":"https://github.com/o/r/issues/7","body":"did things\n\n<!-- worklog-app -->\n<!-- worklog-entry:x -->","createdAt":"2026-11-23T16:00:00Z","state":"CLOSED","parent":{"title":"Parent","url":"https://github.com/o/r/issues/1"},"assignees":{"nodes":[{"login":"me"}]},"issueFieldValues":{"nodes":[{"value":90,"field":{"name":"Actual Worklog (mins)"}}]}},
{"id":"I2","title":"Worklog: far","url":"https://github.com/o/r/issues/8","body":"","createdAt":"2026-12-30T04:00:00Z","state":"OPEN","issueFieldValues":{"nodes":[]}}
],"pageInfo":{"hasNextPage":false}}}}`, nil
	}
	items, err := backend.FetchProjectWorklogs(backend.Config{}, "team-alex", "2026-11-23", "2026-11-25")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Minutes != 90 || items[0].Date != "2026-11-24" ||
		items[0].Owner != "team-alex" || items[0].Remarks != "did things" || items[0].ParentURL != "https://github.com/o/r/issues/1" {
		t.Fatalf("items = %+v", items)
	}
	if len(queries) != 1 || !strings.Contains(queries[0], "type:Worklog") || !strings.Contains(queries[0], "assignee:@me") ||
		!strings.Contains(queries[0], "created:2026-11-22..2026-11-26") {
		t.Fatalf("query = %v", queries)
	}
}

func TestSOPReadSplitsAtTheCutover(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	var filter, query string
	backend.Gh = func(args []string, _ ...string) (string, error) {
		joined := strings.Join(args, " ")
		for _, a := range args {
			if strings.HasPrefix(a, "filter=") {
				filter = a
			}
			if strings.HasPrefix(a, "q=") {
				query = a
			}
		}
		switch {
		case strings.Contains(joined, "search("):
			return `{"data":{"search":{"issueCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}`, nil
		case strings.Contains(joined, "items(first:"):
			return `{"data":{"owner":{"projectV2":{"items":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`, nil
		default:
			return `{"data":{"owner":{"projectV2":{"fields":{"nodes":[{"name":"Worklog Owner (blg-xxxx)"},{"name":"Worklog (Date)"}]}}}}}`, nil
		}
	}
	cfg := backend.Config{ProjectURL: "https://github.com/orgs/sop-split-test/projects/1"}
	if _, err := backend.FetchProjectWorklogs(cfg, "team-alex", "2026-11-16", "2026-11-22"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filter, ">=2026-11-16") || !strings.Contains(filter, "<=2026-11-20") {
		t.Fatalf("the board should be read up to the day before the SOP: %s", filter)
	}
	if !strings.Contains(query, "created:2026-11-20..2026-11-23") {
		t.Fatalf("Worklogs from the SOP on should be searched: %s", query)
	}
}

func TestSOPCommitReferencingAWorklogIsLoggedUnderItsParent(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	backend.Gh = func(args []string, _ ...string) (string, error) {
		q := strings.Join(args, " ")
		if !strings.Contains(q, `owner:"sop-ref-org",name:"app"`) {
			return "", backend.GhErr("unexpected")
		}
		return `{"data":{"r0":{"issue":{"issueType":{"name":"Worklog"},"parent":{"url":"https://github.com/sop-ref-org/app/issues/5185"}}},"r1":{"issue":{"issueType":{"name":"Task"},"parent":null}}}}`, nil
	}
	commits := []backend.Commit{
		{Sha: "aaa1111", Repo: "sop-ref-org/app", Date: "2026-11-24", Issue: "sop-ref-org/app#6001", Message: "Fix (Refs sop-ref-org/app#6001)"},
		{Sha: "bbb2222", Repo: "sop-ref-org/app", Date: "2026-11-24", Issue: "sop-ref-org/app#77", Message: "Other (Refs sop-ref-org/app#77)"},
	}
	backend.ResolveWorklogRefs(commits)
	if commits[0].Worklog != "sop-ref-org/app#6001" || commits[0].Issue != "sop-ref-org/app#5185" {
		t.Fatalf("a Worklog ref should resolve to its parent: %+v", commits[0])
	}
	if commits[1].Worklog != "" || commits[1].Issue != "sop-ref-org/app#77" {
		t.Fatalf("an ordinary issue ref is left alone: %+v", commits[1])
	}
	groups := backend.GroupPending(commits, map[string]bool{}, map[string]bool{})
	if len(groups) != 1 || groups[0].Commits[0].Sha != "bbb2222" {
		t.Fatalf("the commit naming its Worklog is logged already: %+v", groups)
	}
}

func TestSOPRetryNeverCreatesAWorklogOffItsDay(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	created := false
	backend.Gh = func(args []string, _ ...string) (string, error) {
		if len(args) > 1 && args[0] == "issue" && args[1] == "create" {
			created = true
		}
		return `{"data":{"repository":{"issue":{"id":"P","number":1,"title":"Parent","url":"https://github.com/o/r/issues/1","subIssues":{"nodes":[]},"projectItems":{"nodes":[]}}}}}`, nil
	}
	tomorrow := backend.AddDays(backend.WorkDate(time.Now()), 1)
	if !backend.SOPApplies(tomorrow) {
		t.Skip("the SOP has not started")
	}
	_, err := backend.PushEntry(backend.Config{}, "o/r#1", tomorrow, "test", 60, "", "", "subissue", "https://github.com/o/r/issues/99", "")
	if err == nil || created || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("a retry that cannot find its Worklog must not make a new one off its day: created=%v err=%v", created, err)
	}
}

func TestSOPRetryAdoptsAWorklogThatNeverGotAttached(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	parent := `{"data":{"repository":{"issue":{"id":"P","number":1,"title":"Parent","url":"https://github.com/o/r/issues/1","subIssues":{"nodes":[]},"projectItems":{"nodes":[]}}}}}`
	for _, c := range []struct{ name, resume, body string }{
		{"attach failed, URL saved", "https://github.com/o/r/issues/77", "notes"},
		{"reply lost, only the marker", "", "notes\n\n<!-- worklog-app -->\n<!-- worklog-entry:e1 -->"},
	} {
		t.Run(c.name, func(t *testing.T) {
			created, attached := false, ""
			backend.Gh = func(args []string, _ ...string) (string, error) {
				joined := strings.Join(args, " ")
				switch {
				case len(args) > 1 && args[0] == "issue" && args[1] == "create":
					created = true
					return "https://github.com/o/r/issues/78", nil
				case len(args) > 1 && args[0] == "issue" && args[1] == "view":
					return `{"id":"W77","number":77,"title":"Worklog","url":"https://github.com/o/r/issues/77","body":"notes"}`, nil
				case len(args) > 1 && args[0] == "api" && args[1] == "user":
					return "me", nil
				case strings.Contains(joined, "issues(first:100"):
					return authoredIssuesPage(joined), nil
				case strings.Contains(joined, "addSubIssue"):
					for _, a := range args {
						if strings.HasPrefix(a, "c=") {
							attached = strings.TrimPrefix(a, "c=")
						}
					}
					return `{}`, nil
				case strings.Contains(joined, "repository(owner"):
					return parent, nil
				}
				return "", backend.GhErr("offline")
			}
			res, err := backend.PushEntry(backend.Config{}, "o/r#1", shell.Today(), "test", 60, "notes", c.body, "subissue", c.resume, "")
			if err != nil || created || attached != "W77" || res.URL != "https://github.com/o/r/issues/77" {
				t.Fatalf("the earlier Worklog should be attached and reused: created=%v attached=%q res=%+v err=%v", created, attached, res, err)
			}
		})
	}
}

// authoredIssuesPage is the user's issues newest first, a hundred a page: the
// Worklog with marker e1 is the 151st.
func authoredIssuesPage(query string) string {
	start := 0
	if strings.Contains(query, "after=page2") {
		start = 100
	}
	created := time.Now().UTC().Format(time.RFC3339)
	var nodes []string
	for i := start; i < start+100 && i < 151; i++ {
		body := "other"
		id := fmt.Sprintf("X%d", i)
		number := 1000 - i
		if i == 150 {
			body, id, number = "notes <!-- worklog-entry:e1 -->", "W77", 77
		}
		nodes = append(nodes, fmt.Sprintf(`{"id":%q,"number":%d,"url":"https://github.com/o/r/issues/%d","body":%q,"createdAt":%q}`, id, number, number, body, created))
	}
	next := `"hasNextPage":false`
	if start == 0 {
		next = `"hasNextPage":true,"endCursor":"page2"`
	}
	return `{"data":{"repository":{"issues":{"nodes":[` + strings.Join(nodes, ",") + `],"pageInfo":{` + next + `}}}}}`
}

func TestSOPRefMustNameTheWholeIssueNumber(t *testing.T) {
	if backend.MessageNamesIssue("Fix\n\nRefs o/r#60010\n", "o/r#6001") {
		t.Fatal("#60010 does not name #6001")
	}
	for _, msg := range []string{"Refs o/r#6001", "Fix (Refs o/r#6001)", "x\nRefs O/R#6001\n"} {
		if !backend.MessageNamesIssue(msg, "o/r#6001") {
			t.Fatalf("%q names o/r#6001", msg)
		}
	}
	if got := backend.AddWorklogRefs("Fix\n\nRefs o/r#60010\n", []string{"o/r#6001"}); !strings.HasSuffix(got, "Refs o/r#60010\nRefs o/r#6001\n") {
		t.Fatalf("the missing ref should be added: %q", got)
	}
}

func TestSOPPushStopsWhenItCannotCheckForAnEarlierWorklog(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	parent := `{"data":{"repository":{"issue":{"id":"P","number":1,"title":"Parent","url":"https://github.com/o/r/issues/1","subIssues":{"nodes":[]},"projectItems":{"nodes":[]}}}}}`
	for _, c := range []struct{ name, resume, body string }{
		{"saved URL cannot be read", "https://github.com/o/r/issues/77", "notes"},
		{"latest issues cannot be listed", "", "notes\n<!-- worklog-entry:e2 -->"},
	} {
		t.Run(c.name, func(t *testing.T) {
			created := false
			backend.Gh = func(args []string, _ ...string) (string, error) {
				switch {
				case len(args) > 1 && args[0] == "issue" && args[1] == "create":
					created = true
					return "https://github.com/o/r/issues/78", nil
				case len(args) > 1 && args[0] == "issue" && args[1] == "view",
					strings.Contains(strings.Join(args, " "), "issues(first:100"):
					return "", backend.GhErr("connection reset")
				case len(args) > 1 && args[0] == "api" && args[1] == "user":
					return "me", nil
				case strings.Contains(strings.Join(args, " "), "repository(owner"):
					return parent, nil
				}
				return "", backend.GhErr("offline")
			}
			_, err := backend.PushEntry(backend.Config{}, "o/r#1", shell.Today(), "test", 60, "notes", c.body, "subissue", c.resume, "")
			if err == nil || created {
				t.Fatalf("a failed check must stop the push: created=%v err=%v", created, err)
			}
		})
	}
}

func TestWorklogRefsWaitForAMergeAndFinishAfterACrash(t *testing.T) {
	f := newRewriteFixture(t)
	store, cfg, made := f.store(t, f.b)
	batches := backend.PlanWorklogRefs(cfg, made)
	if len(batches) != 1 || batches[0].Problem != "" {
		t.Fatalf("plan = %+v", batches)
	}
	realGit := backend.Git
	t.Cleanup(func() { backend.Git = realGit })
	gitDir := gitT(t, f.clone, "rev-parse", "--absolute-git-dir")
	merging := filepath.Join(gitDir, "MERGE_HEAD")
	// A merge starts right after the push, before the local branch moves.
	backend.Git = func(dir string, env []string, stdin string, args ...string) (string, error) {
		out, err := realGit(dir, env, stdin, args...)
		if len(args) > 0 && args[0] == "push" && err == nil {
			_ = os.WriteFile(merging, []byte(f.mainTip+"\n"), 0o644)
		}
		return out, err
	}
	before := gitT(t, f.clone, "rev-parse", "refs/heads/feat")
	done, err := store.RunWorklogRefs(batches[0])
	if err != nil || done.Stage != "local-sync" || gitT(t, f.clone, "rev-parse", "refs/heads/feat") != before {
		t.Fatalf("the branch must not move during a merge: stage=%s err=%v", done.Stage, err)
	}
	backend.Git = realGit
	journal, _ := store.RewriteJournal()
	if len(journal) != 1 {
		t.Fatalf("journal = %+v", journal)
	}
	stale := journal[0]
	// The merge ends with a commit of its own; the retry carries it across.
	if err := os.Remove(merging); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.clone, "e.txt"), []byte("e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, f.clone, "add", "e.txt")
	gitT(t, f.clone, "commit", "--quiet", "-m", "Commit E")
	if _, err := store.ResumeWorklogRefs(stale); err != nil {
		t.Fatal(err)
	}
	tip := gitT(t, f.clone, "rev-parse", "refs/heads/feat")
	if gitT(t, f.clone, "log", "-1", "--format=%s", tip) != "Commit E" || gitT(t, f.clone, "rev-parse", tip+"~2") != done.NewRemote {
		t.Fatalf("E and C should sit on the rewritten B:\n%s", gitT(t, f.clone, "log", "--format=%h %s", "feat"))
	}
	// A run cut short after moving the branch, before recording it, is
	// retried from the journal as it stood: it must finish, not stick.
	if _, err := store.ResumeWorklogRefs(stale); err != nil {
		t.Fatal(err)
	}
	if gitT(t, f.clone, "rev-parse", "refs/heads/feat") != tip {
		t.Fatal("a repeated retry must leave the branch alone")
	}
	rows, _ := store.ReadRows()
	if rows[0]["ref_state"] != backend.RefComplete {
		t.Fatalf("the entry should be complete: %v", rows[0])
	}
}

func TestSOPWeekEmailFiltersWorklogIssuesOnTheSavedBoards(t *testing.T) {
	cfg := sampleExportConfig()
	cfg.GithubAuthor = "blg-sample"
	for _, tc := range []struct {
		from, to string
		filters  []string
	}{
		{"2026-11-09", "2026-11-15", []string{
			`"zzz-(deprecated)-worklog-owner-(blg-xxxx)":team-alex "worklog-(date)":>=2026-11-09 "worklog-(date)":<=2026-11-15`,
			`type:"Worklog" assignee:blg-sample created:2026-11-09..2026-11-15`,
		}},
		{"2026-11-23", "2026-11-29", []string{`type:"Worklog" assignee:blg-sample created:2026-11-23..2026-11-29`}},
		{"2026-11-16", "2026-11-22", []string{
			`"zzz-(deprecated)-worklog-owner-(blg-xxxx)":team-alex "worklog-(date)":>=2026-11-16 "worklog-(date)":<=2026-11-20`,
			`type:"Worklog" assignee:blg-sample created:2026-11-16..2026-11-22`,
		}},
	} {
		links := backend.WeekBoardLinks(cfg, "team-alex", tc.from, tc.to)
		if len(links) != len(cfg.BoardViewURLs)*len(tc.filters) {
			t.Fatalf("week %s has the wrong number of board links: %v", tc.from, links)
		}
		for i, raw := range links {
			view, _ := url.Parse(cfg.BoardViewURLs[i%len(cfg.BoardViewURLs)])
			link, err := url.Parse(raw)
			filter := tc.filters[i/len(cfg.BoardViewURLs)]
			viewQuery := view.Query()
			viewQuery.Set("filterQuery", filter)
			if err != nil || link.Host != view.Host || link.Path != view.Path || link.Query().Encode() != viewQuery.Encode() {
				t.Fatalf("week %s link %d should keep its project view and apply %q: %q", tc.from, i, filter, raw)
			}
			if strings.HasPrefix(filter, "type:") && !strings.HasSuffix(backend.BoardLinkLabel(raw), " · Worklog issues") {
				t.Fatalf("Worklog filter should be visible in the board label: %s", backend.BoardLinkLabel(raw))
			}
		}
	}
}

func TestSOPJustPushedWorklogsStayOnStatusUntilSearchSeesThem(t *testing.T) {
	rows := []backend.Row{
		{"date": "2026-11-24", "pushed_at": "2026-11-24T10:00:00", "issue_url": "https://github.com/o/r/issues/9", "minutes": "45", "issue": "o/r#1"},
		{"date": "2026-11-24", "pushed_at": "2026-11-24T11:00:00", "issue_url": "https://github.com/o/r/issues/10", "minutes": "30"},
		{"date": "2026-11-19", "pushed_at": "2026-11-19T11:00:00", "issue_url": "https://github.com/o/r/issues/11", "minutes": "30"},
		{"date": "2026-11-24", "minutes": "20"},
	}
	items := []backend.WorklogItem{{URL: "https://github.com/o/r/issues/10", Date: "2026-11-24", Minutes: 30}}
	got := backend.MergePushedRows(items, rows, "team-alex", "2026-11-01", "2026-11-30")
	if len(got) != 2 || got[1].URL != "https://github.com/o/r/issues/9" || got[1].Minutes != 45 || got[1].Owner != "team-alex" {
		t.Fatalf("got %+v", got)
	}
}
