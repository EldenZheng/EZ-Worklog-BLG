package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The week export is the one thing on the Log work tab aimed at somebody else
// reading it: a week goes to the CEO as an email with the board links to click
// and a file to open. Both are built from the same fetched items the strip is
// already showing, so what is sent is what was on screen.

// boardViewURLs are the saved board views the weekly mail links to, kept whole
// rather than rebuilt from the project URLs in Settings.
//
// Everything after the project number — the view id, which columns are visible,
// what the rows are grouped by and which field is summed — is view state that a
// project URL simply does not carry. Reconstructing a link from
// "orgs/<org>/projects/9" would open the default view with the default columns,
// which is not the view the mail is about. The dates and the owner in the
// filter are the only parts that change week to week, and those are the only
// parts rewritten.
var boardViewURLs = []string{
	"https://github.com/orgs/bigledger/projects/9/views/19?filterQuery=%22worklog-owner-%28blg-xxxx%29%22%3Ablg-elden+%22worklog-%28date%29%22%3A%3E%3D2026-08-17+%22worklog-%28date%29%22%3A%3C%3D2026-08-23&visibleFields=%5B%22Parent+issue%22%2C%22Title%22%2C208086921%2C%22Assignees%22%2C%22Status%22%2C209221222%2C210708320%2C209219619%5D",
	"https://github.com/orgs/BigLedger-Support/projects/1/views/66?reload=1&filterQuery=%22worklog-owner-%28blg-xxxx%29%22%3Ablg-elden+%22worklog-%28date%29%22%3A%3E%3D2026-08-17+%22worklog-%28date%29%22%3A%3C%3D2026-08-23&groupedBy%5BcolumnId%5D=362969068&sumFields=%5B362969103%5D&hideItemsCount=false&sortedBy%5Bdirection%5D=desc&sortedBy%5BcolumnId%5D=362969068",
}

// weekBoardLinks points each saved view at one week and one owner.
//
// Only the filterQuery is touched. It is rewritten wholesale from
// worklogProjectFilter — the same string the app already sends to the API, so
// the link and the fetch behind the numbers ask GitHub the same question — and
// every other parameter is carried across untouched.
//
// A link that cannot be parsed is passed through as it stands. It is a link to
// paste in an email, and a broken one the reader can still see beats a silently
// missing board.
func weekBoardLinks(owner, fromDate, toDate string) []string {
	out := make([]string, 0, len(boardViewURLs))
	for _, raw := range boardViewURLs {
		u, err := url.Parse(raw)
		if err != nil {
			out = append(out, raw)
			continue
		}
		q := u.Query()
		q.Set("filterQuery", worklogProjectFilter(orDefault(owner, "blg-elden"), fromDate, toDate))
		u.RawQuery = q.Encode()
		out = append(out, u.String())
	}
	return out
}

// viewIDRe pulls the saved view out of a board URL. Two links to the same
// project differ only by it, so a label without it names them both the same.
var viewIDRe = regexp.MustCompile(`/views/(\d+)`)

// boardLinkLabel names a board link for a human: the org, the project number and
// the saved view. Read off the URL rather than fetched — the label is there to
// tell two links apart, and a round trip to GitHub to title a hyperlink that
// already says where it goes is not worth the wait.
func boardLinkLabel(rawURL string) string {
	ref, err := parseProjectRef(rawURL)
	if err != nil {
		return rawURL
	}
	label := fmt.Sprintf("%s · project %d", ref.login, ref.number)
	if m := viewIDRe.FindStringSubmatch(rawURL); m != nil {
		label += " · view " + m[1]
	}
	return label
}

// issueTotal is one issue's week: every worklog filed under it, added up.
type issueTotal struct {
	Title   string
	URL     string
	Minutes int
	Entries int
}

// weekByIssue is the summary the mail is actually written from: what was worked
// on this week and how long each thing took.
//
// Grouped by the parent issue, because that is the piece of work with a name.
// The worklogs themselves are all called "Worklog: <date>", so a list of those
// says only that the week happened. A worklog with no parent stands as its own
// row rather than being dropped — a missing link is not a missing five hours.
//
// Longest first: the top of the list is what the week was about.
func weekByIssue(items []WorklogItem) []issueTotal {
	byKey := map[string]*issueTotal{}
	var order []string
	for _, it := range items {
		title, link := it.ParentTitle, it.ParentURL
		if link == "" {
			title, link = it.Title, it.URL
		}
		key := link
		if key == "" {
			key = title
		}
		if key == "" {
			key = "(no issue)"
		}
		if _, seen := byKey[key]; !seen {
			byKey[key] = &issueTotal{Title: orDefault(title, "(untitled)"), URL: link}
			order = append(order, key)
		}
		byKey[key].Minutes += it.Minutes
		byKey[key].Entries++
	}

	out := make([]issueTotal, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Minutes != out[j].Minutes {
			return out[i].Minutes > out[j].Minutes
		}
		return out[i].Title < out[j].Title
	})
	return out
}

// weekTSVHeader matches the board's own download, so the file opens looking like
// the one the reader may already have seen. The two boards export their columns
// in different orders; this is their union in one fixed order, since the point
// here is a single file rather than one per board.
var weekTSVHeader = []string{
	"Parent issue", "Title", "URL", "Worklog (Date)", "Worklog (mins)",
	"Repository", "Assignees", "Status", "Worklog Owner (blg-xxxx)", "Worklog (Remarks)",
}

// weekTSV renders the week as the board would download it, oldest first.
func weekTSV(items []WorklogItem) string {
	rows := append([]WorklogItem(nil), items...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Date != rows[j].Date {
			return rows[i].Date < rows[j].Date
		}
		return rows[i].Minutes > rows[j].Minutes
	})

	var b strings.Builder
	b.WriteString(strings.Join(weekTSVHeader, "\t"))
	b.WriteByte('\n')
	for _, it := range rows {
		b.WriteString(strings.Join([]string{
			tsvCell(it.ParentURL),
			tsvCell(it.Title),
			tsvCell(it.URL),
			tsvCell(it.Date),
			strconv.Itoa(it.Minutes),
			tsvCell(it.Repo()),
			tsvCell(strings.Join(it.Assignees, ",")),
			tsvCell(it.Status),
			tsvCell(it.Owner),
			tsvCell(it.Remarks),
		}, "\t"))
		b.WriteByte('\n')
	}
	return b.String()
}

// tsvCell flattens a value onto one line. TSV has no quoting to fall back on,
// so a tab or a newline inside a cell does not escape — it silently becomes a
// new column or a new row, and the remarks are full of both.
func tsvCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.TrimSpace(s)
}

// weekMailText is what goes on the clipboard: the links to click and the
// summary to talk to, in the order they would be read.
//
// The TSV is not in here. It goes in the attached file, where a spreadsheet can
// open it; pasted into a mail body it is a wall of tab-separated remarks.
func weekMailText(owner, fromDate, toDate string, items []WorklogItem, tsvPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Worklog %s to %s — %s\n\n", fromDate, toDate, orDefault(owner, "blg-elden"))

	b.WriteString("Boards:\n")
	for _, link := range weekBoardLinks(owner, fromDate, toDate) {
		b.WriteString("  " + link + "\n")
	}

	totals := weekByIssue(items)
	total := weekTotalMinutes(totals)
	fmt.Fprintf(&b, "\nBy issue (%d min · %s across %d issues):\n",
		total, hoursMins(total), len(totals))
	if len(totals) == 0 {
		b.WriteString("  Nothing on the board for this week.\n")
	}
	for _, t := range totals {
		// Minutes, not hours: that is the unit the board sums in and the unit
		// the figures are argued about in.
		fmt.Fprintf(&b, "  %-7s %s\n", strconv.Itoa(t.Minutes)+"m", t.Title)
		if t.URL != "" {
			fmt.Fprintf(&b, "          %s\n", t.URL)
		}
	}
	if tsvPath != "" {
		fmt.Fprintf(&b, "\nDetail attached: %s\n", tsvPath)
	}
	return b.String()
}

func weekTotalMinutes(totals []issueTotal) int {
	total := 0
	for _, t := range totals {
		total += t.Minutes
	}
	return total
}

// downloadsDir is where the export lands: the folder the mail client's attach
// dialog opens in, and where the board's own TSV downloads go, so the week's
// file sits beside the ones it is meant to look like.
//
// Falls back to fallback — the app's own folder — when there is no home or no
// Downloads under it. A file written somewhere findable beats a failed export.
func downloadsDir(fallback string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return fallback
	}
	dl := filepath.Join(home, "Downloads")
	if info, err := os.Stat(dl); err != nil || !info.IsDir() {
		return fallback
	}
	return dl
}

// exportWeek writes the week's TSV and builds the mail text that goes with it.
// It returns the path written, the number of entries in it, and that text.
func exportWeek(dir, owner, fromDate, toDate string, items []WorklogItem) (string, int, string, error) {
	path := filepath.Join(dir, fmt.Sprintf("worklog-%s-to-%s.tsv", fromDate, toDate))
	if err := os.WriteFile(path, []byte(weekTSV(items)), 0o644); err != nil {
		return "", 0, "", err
	}
	return path, len(items), weekMailText(owner, fromDate, toDate, items, path), nil
}
