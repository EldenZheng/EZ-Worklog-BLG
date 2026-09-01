package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Not every hour is a commit. Three kinds of entry are logged by hand, and they
// differ in where the work is filed, not in how it is counted:
//
//   - meeting     — the day's own "<Name> Meeting & ad hocs: <date>" issue,
//     which carries the Worklog fields itself.
//   - other       — an issue that already exists; you paste its link.
//   - independent — an issue that does not exist yet; the app creates it in a
//     repo you pick, then logs under it the way a commit entry does.
//
// All three save locally first and touch GitHub only on push, which is the same
// bargain the commit entries make: nothing is created for an entry that ends up
// being deleted or re-dated.

const (
	// meetingRepo is where the day's meeting issue lives. Fixed, because that
	// is what the board expects: every "<Name> Meeting & ad hocs" issue filed so
	// far is in this repo, and one filed anywhere else would fall out of the
	// views the weekly mail links to.
	meetingRepo = "bigledger/blg-int-general-task"

	// The issue types the two created kinds take. A meeting is not a piece of
	// development work and the board types it apart; an independent entry is
	// ordinary work that simply had no issue yet.
	meetingIssueType     = "Meeting / Training"
	independentIssueType = "Task"

	kindCommit      = "commit"
	kindMeeting     = "meeting"
	kindOther       = "other"
	kindIndependent = "independent"
)

// displayName is the name a meeting issue is titled with.
//
// Derived from the worklog owner by default — "blg-elden" becomes "Elden" — so
// a fresh install titles its meetings correctly without being told twice. The
// setting overrides it for anyone whose handle is not their name.
func displayName(cfg Config) string {
	if n := strings.TrimSpace(cfg.DisplayName); n != "" {
		return n
	}
	owner := strings.TrimSpace(cfg.WorklogOwner)
	if owner == "" {
		return ""
	}
	// Handles are "blg-elden": drop the org prefix, keep the last part.
	if i := strings.LastIndexAny(owner, "-_"); i >= 0 && i+1 < len(owner) {
		owner = owner[i+1:]
	}
	r := []rune(owner)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// meetingTitle is the day's meeting issue, named the way the ones already on the
// board are named.
func meetingTitle(cfg Config, date string) string {
	return fmt.Sprintf("%s Meeting & ad hocs: %s", displayName(cfg), date)
}

// findIssueByTitle looks for an exact title in a repo, open or closed.
//
// The search is GitHub's, so it matches loosely and can return near misses;
// the exact comparison afterwards is what decides. Closed counts — a meeting
// issue is closed once the board moves it to Done, and a second entry logged
// later the same day belongs on that same issue, not on a new one.
func findIssueByTitle(repo, title string) (string, error) {
	out, err := gh([]string{
		"issue", "list", "--repo", repo, "--state", "all", "--limit", "30",
		"--search", fmt.Sprintf("%q in:title", title),
		"--json", "number,title",
	})
	if err != nil {
		return "", err
	}
	var found []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal([]byte(out), &found); err != nil {
		return "", ghErr("could not read the issue search: %v", err)
	}
	for _, f := range found {
		if strings.EqualFold(strings.TrimSpace(f.Title), title) {
			return fmt.Sprintf("%s#%d", repo, f.Number), nil
		}
	}
	return "", nil
}

// createTypedIssue opens an issue, assigns it to you and gives it an issue type.
//
// The type goes through GraphQL because `gh issue create` has no --type, and it
// is applied after the fact: a type that cannot be set is worth a note, not a
// lost issue, since the issue itself is the thing that could not be undone.
func createTypedIssue(repo, title, body, issueType string) (string, string, error) {
	owner, _, ok := strings.Cut(repo, "/")
	if !ok {
		return "", "", ghErr("Repo should look like owner/repo, got %q.", repo)
	}
	args := []string{"issue", "create", "--repo", repo, "--title", title, "--assignee", "@me"}
	// An empty --body is still a body; leaving the flag off entirely would make
	// gh open an editor, which in this process means hanging forever.
	args = append(args, "--body", body)
	out, err := gh(args)
	if err != nil {
		return "", "", err
	}
	url := lastLine(out)

	ref, err := refFromIssueURL(url)
	if err != nil {
		return url, "", err
	}
	if issueType == "" {
		return url, ref, nil
	}

	id, err := viewID(url)
	if err != nil {
		return url, ref, ghErr("Created %s but could not read its id to set the type: %v", ref, err)
	}
	typeID, typeErr := issueTypeID(owner, issueType)
	switch {
	case errors.Is(typeErr, errNoIssueType):
		return url, ref, nil // the org has no such type; the issue still stands
	case typeErr != nil:
		return url, ref, ghErr("Created %s but could not look up the %q type: %v", ref, issueType, typeErr)
	}
	if err := setIssueType(id, typeID); err != nil {
		return url, ref, ghErr("Created %s but could not set its type: %v", ref, err)
	}
	return url, ref, nil
}

// refFromIssueURL turns the URL gh prints back into an owner/repo#123 ref.
func refFromIssueURL(url string) (string, error) {
	i := strings.Index(url, "github.com/")
	if i < 0 {
		return "", ghErr("Could not read the new issue's number from %q.", url)
	}
	parts := strings.Split(strings.Trim(url[i+len("github.com/"):], "/"), "/")
	if len(parts) < 4 {
		return "", ghErr("Could not read the new issue's number from %q.", url)
	}
	return fmt.Sprintf("%s/%s#%s", parts[0], parts[1], parts[3]), nil
}

// ensureMeetingIssue returns the ref of the day's meeting issue, creating it the
// first time something is logged against that day.
//
// Reused rather than created per entry: the board has one meeting issue per day
// and its Worklog (mins) is the day's total, so a second meeting logged on the
// same day adds to that issue rather than opening another beside it.
func ensureMeetingIssue(cfg Config, date, body string) (string, error) {
	if displayName(cfg) == "" {
		return "", ghErr("Set your worklog owner (or a display name) in Settings before logging a meeting.")
	}
	title := meetingTitle(cfg, date)
	if ref, err := findIssueByTitle(meetingRepo, title); err != nil {
		return "", err
	} else if ref != "" {
		return ref, nil
	}
	_, ref, err := createTypedIssue(meetingRepo, title, body, meetingIssueType)
	if ref == "" {
		return "", err
	}
	// A ref with an error means the issue exists but its type did not stick.
	// The ref is what the push needs; the rest is a note, not a failure.
	return ref, nil
}

// pushableWithoutIssue reports whether a row with no issue ref can still be
// pushed, because its issue is decided when it is pushed rather than typed in.
//
// A meeting goes to the day's own meeting issue and an independent entry creates
// the parent it names, so both sit on disk looking unfiled and are not. Reading
// an empty issue column as "nothing to push to" would grey out the button on the
// two kinds that were built to work that way.
func pushableWithoutIssue(r Row) bool {
	switch r["type"] {
	case kindMeeting:
		return true
	case kindIndependent:
		return strings.TrimSpace(r["parent_repo"]) != "" &&
			strings.TrimSpace(r["parent_title"]) != ""
	}
	return false
}

// issueRefFromAny accepts an issue however it comes to hand — pasted browser
// URL or typed owner/repo#123 — and returns the ref the rest of the app uses.
//
// A pasted URL is what actually happens: you are looking at the issue, so you
// copy the address bar. Refusing that and demanding the shorthand would be the
// app being difficult about a string it can read perfectly well.
func issueRefFromAny(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ghErr("Paste the issue link, or type it as owner/repo#123.")
	}
	if strings.Contains(s, "github.com/") {
		ref, err := refFromIssueURL(s)
		if err != nil {
			return "", ghErr("That does not look like an issue link: %q", s)
		}
		return ref, nil
	}
	if issRe.MatchString(s) {
		return s, nil
	}
	return "", ghErr("Issue should look like owner/repo#123, got %q.", s)
}

// ensureIssueRef fills in the issue a saved row should be pushed against, for
// the kinds whose issue is only decided at push time. It returns the patch to
// apply to the row, or an empty patch when there is nothing to resolve.
func ensureIssueRef(cfg Config, r Row) (Row, error) {
	if strings.TrimSpace(r["issue"]) != "" {
		return Row{}, nil // already filed somewhere
	}
	body := strings.TrimSpace(r["remarks"])
	if body == "" {
		body = strings.TrimSpace(r["description"])
	}

	switch r["type"] {
	case kindMeeting:
		ref, err := ensureMeetingIssue(cfg, r["date"], body)
		if err != nil {
			return Row{}, err
		}
		// The meeting issue carries the Worklog fields itself — it is the work,
		// not a parent for it, and a "Worklog: <date>" child under a one-day
		// meeting issue would be a tree of two saying one thing.
		return Row{"issue": ref, "mode": "issue"}, nil

	case kindIndependent:
		repo := strings.TrimSpace(r["parent_repo"])
		title := strings.TrimSpace(r["parent_title"])
		if repo == "" || title == "" {
			return Row{}, ghErr("This entry has no parent issue to create — set a repo and a title on it.")
		}
		_, ref, err := createTypedIssue(repo, title, body, independentIssueType)
		if ref == "" {
			return Row{}, err
		}
		return Row{"issue": ref, "mode": "subissue"}, nil
	}
	return Row{}, nil
}
