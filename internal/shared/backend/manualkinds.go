package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Not every hour is a commit. Four kinds of entry are logged by hand, and they
// differ in where the work is filed, not in how it is counted:
//
//   - meeting     — the day's own "<Name> Meeting & ad hocs: <date>" issue,
//     which carries the Worklog fields itself.
//   - other       — an issue that already exists; you paste its link.
//   - codereview  — one review filed under the development issue pasted into
//     the form, retaining the original single-review workflow.
//   - codereview_bulk — the day's own "<Name> Code Review: <date>" issue, with
//     all reviewed PR links and their minutes in its body.
//   - independent — an issue that does not exist yet; the app creates it in a
//     repo you pick, then logs under it the way a commit entry does.
//
// All four save locally first and touch GitHub only on push, which is the same
// bargain the commit entries make: nothing is created for an entry that ends up
// being deleted or re-dated.

const (
	// The issue types the created kinds take. A meeting is not a piece of
	// development work and the board types it apart; code review and independent
	// entries are ordinary tasks.
	meetingIssueType     = "Meeting / Training"
	independentIssueType = "Task"
	codeReviewIssueType  = "Task"

	KindCommit      = "commit"
	KindMeeting     = "meeting"
	KindOther       = "other"
	KindCodeReview  = "codereview"
	KindBulkReview  = "codereview_bulk"
	KindIndependent = "independent"
)

// RowIsBulkCodeReview includes the early bulk drafts that used the single
// review kind before Bulk Review had its own stored type.
func RowIsBulkCodeReview(r Row) bool {
	return r["type"] == KindBulkReview ||
		r["type"] == KindCodeReview && RowCreatesOwnIssue(r)
}

// RowIsSingleCodeReview also recognises older manual rows by their saved
// generated description. Named daily bulk titles and ordinary issue titles
// mentioning reviews do not count.
func RowIsSingleCodeReview(r Row) bool {
	if RowIsBulkCodeReview(r) {
		return false
	}
	kind := strings.TrimSpace(r["type"])
	single := kind == KindCodeReview || strings.EqualFold(kind, "Code Review")
	if !single && (kind == "" || kind == KindOther) {
		saved := strings.TrimSpace(r["description"])
		if strings.EqualFold(saved, "Code Review") {
			single = true
		} else if date, ok := strings.CutPrefix(saved, "Code Review: "); ok {
			_, err := time.Parse("2006-01-02", date)
			single = err == nil
		}
		if _, err := ParseCodeReviewItems(r["remarks"]); err == nil {
			single = false
		}
	}
	return single
}

// RowCreatesOwnIssue identifies manual work whose GitHub issue is determined
// by the app rather than pasted from an existing development issue. These rows
// share the standalone bubble/editor treatment; a single Code Review and Other
// remain issue-backed.
func RowCreatesOwnIssue(r Row) bool {
	switch r["type"] {
	case KindMeeting, KindBulkReview, KindIndependent:
		return true
	case KindCodeReview:
		// Compatibility for bulk rows saved before Bulk Review had its own
		// type, including ones that have since gained an issue ref.
		_, err := ParseCodeReviewItems(r["remarks"])
		return err == nil
	}
	return false
}

// RowUsesIndependentStyle is the visual grouping for work that owns the issue
// the app creates. A regular Worklog (the stored kindOther) points at an
// existing parent and therefore follows that parent's organisation colour.
func RowUsesIndependentStyle(r Row) bool {
	switch r["type"] {
	case KindMeeting, KindBulkReview, KindIndependent:
		return true
	case KindCodeReview:
		return RowCreatesOwnIssue(r) // legacy bulk-review rows only
	}
	return false
}

// DisplayName is the name a meeting issue is titled with.
//
// Derived from the worklog owner by default — "team-alex" becomes "Alex" — so
// a fresh install titles its meetings correctly without being told twice. The
// setting overrides it for anyone whose handle is not their name.
func DisplayName(cfg Config) string {
	if n := strings.TrimSpace(cfg.DisplayName); n != "" {
		return n
	}
	owner := strings.TrimSpace(cfg.WorklogOwner)
	if owner == "" {
		return ""
	}
	// Handles are "team-alex": drop the org prefix, keep the last part.
	if i := strings.LastIndexAny(owner, "-_"); i >= 0 && i+1 < len(owner) {
		owner = owner[i+1:]
	}
	r := []rune(owner)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// MeetingTitle is the day's meeting issue, named the way the ones already on the
// board are named.
func MeetingTitle(cfg Config, date string) string {
	return fmt.Sprintf("%s Meeting & ad hocs: %s", DisplayName(cfg), date)
}

// CodeReviewTitle is the auto-title a code-review sub-issue takes on push,
// mirroring the "Worklog: <date>" default that other and independent entries
// wear. A review filed under someone else's issue reads on the board as
// "Code Review: <date>" rather than as another Worklog stub.
func CodeReviewTitle(date string) string {
	return fmt.Sprintf("Code Review: %s", date)
}

// DailyCodeReviewTitle is the shared issue that holds every PR reviewed on one
// day. It mirrors the meeting issue convention and the existing manually made
// issues, for example "Alex Code Review: 2026-09-15".
func DailyCodeReviewTitle(cfg Config, date string) string {
	return fmt.Sprintf("%s Code Review: %s", DisplayName(cfg), date)
}

// SubTitleBaseFor is what PushEntry should title a row's sub-issue with. Empty
// means "use the default 'Worklog: <date>'" — the vast majority; only a code-
// review row overrides it so its sub-issue is named as such on the board.
func SubTitleBaseFor(r Row) string {
	if r["type"] == KindCodeReview {
		return CodeReviewTitle(r["date"])
	}
	return ""
}

// findIssueByTitle looks for an exact title in a repo, open or closed.
//
// The search is GitHub's, so it matches loosely and can return near misses;
// the exact comparison afterwards is what decides. Closed counts — a meeting
// issue is closed once the board moves it to Done, and a second entry logged
// later the same day belongs on that same issue, not on a new one.
func findIssueByTitle(repo, title string) (string, error) {
	out, err := Gh([]string{
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
		return "", GhErr("could not read the issue search: %v", err)
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
		return "", "", GhErr("Repo should look like owner/repo, got %q.", repo)
	}
	args := []string{"issue", "create", "--repo", repo, "--title", title, "--assignee", "@me"}
	// An empty --body is still a body; leaving the flag off entirely would make
	// gh open an editor, which in this process means hanging forever.
	args = append(args, "--body", body)
	out, err := Gh(args)
	if err != nil {
		return "", "", err
	}
	url := lastLine(out)

	ref, err := RefFromIssueURL(url)
	if err != nil {
		return url, "", err
	}
	if issueType == "" {
		return url, ref, nil
	}

	id, err := viewID(url)
	if err != nil {
		return url, ref, GhErr("Created %s but could not read its id to set the type: %v", ref, err)
	}
	typeID, typeErr := issueTypeID(owner, issueType)
	switch {
	case errors.Is(typeErr, ErrNoIssueType):
		return url, ref, nil // the org has no such type; the issue still stands
	case typeErr != nil:
		return url, ref, GhErr("Created %s but could not look up the %q type: %v", ref, issueType, typeErr)
	}
	if err := setIssueType(id, typeID); err != nil {
		return url, ref, GhErr("Created %s but could not set its type: %v", ref, err)
	}
	return url, ref, nil
}

// RefFromIssueURL turns the URL gh prints back into an owner/repo#123 ref.
func RefFromIssueURL(url string) (string, error) {
	i := strings.Index(url, "github.com/")
	if i < 0 {
		return "", GhErr("Could not read the new issue's number from %q.", url)
	}
	parts := strings.Split(strings.Trim(url[i+len("github.com/"):], "/"), "/")
	if len(parts) < 4 {
		return "", GhErr("Could not read the new issue's number from %q.", url)
	}
	return fmt.Sprintf("%s/%s#%s", parts[0], parts[1], parts[3]), nil
}

// EnsureMeetingIssue returns the ref of the day's meeting issue, creating it the
// first time something is logged against that day.
//
// Reused rather than created per entry: the board has one meeting issue per day
// and its Worklog (mins) is the day's total, so a second meeting logged on the
// same day adds to that issue rather than opening another beside it.
func EnsureMeetingIssue(cfg Config, date, body string) (string, error) {
	repo := strings.TrimSpace(cfg.MeetingRepo)
	if !ValidRepo(repo) {
		return "", GhErr("Set the meeting repository to owner/repo in Settings before pushing.")
	}
	if DisplayName(cfg) == "" {
		return "", GhErr("Set your worklog owner (or a display name) in Settings before logging a meeting.")
	}
	title := MeetingTitle(cfg, date)
	if ref, err := findIssueByTitle(repo, title); err != nil {
		return "", err
	} else if ref != "" {
		return ref, nil
	}
	_, ref, err := createTypedIssue(repo, title, body, meetingIssueType)
	if ref == "" {
		return "", err
	}
	// A ref with an error means the issue exists but its type did not stick.
	// The ref is what the push needs; the rest is a note, not a failure.
	return ref, nil
}

// EnsureCodeReviewIssue returns the day's bulk code-review issue, creating it
// with the PR list as its body the first time that date is pushed.
func EnsureCodeReviewIssue(cfg Config, date, body string) (string, error) {
	repo := strings.TrimSpace(cfg.BulkCodeReviewRepo)
	if !ValidRepo(repo) {
		return "", GhErr("Set the bulk review repository to owner/repo in Settings before pushing.")
	}
	if DisplayName(cfg) == "" {
		return "", GhErr("Set your worklog owner (or a display name) in Settings before logging code review.")
	}
	title := DailyCodeReviewTitle(cfg, date)
	if ref, err := findIssueByTitle(repo, title); err != nil {
		return "", err
	} else if ref != "" {
		return ref, nil
	}
	_, ref, err := createTypedIssue(repo, title, body, codeReviewIssueType)
	if ref == "" {
		return "", err
	}
	return ref, nil
}

// PushableWithoutIssue reports whether a row with no issue ref can still be
// pushed, because its issue is decided when it is pushed rather than typed in.
//
// Meetings and code reviews go to their own daily issues and an independent
// entry creates the parent it names, so all three sit on disk looking unfiled
// and are not. Reading an empty issue column as "nothing to push to" would grey
// out the button on the kinds that were built to work that way.
func PushableWithoutIssue(r Row) bool {
	switch r["type"] {
	case KindMeeting:
		return true
	case KindBulkReview:
		return strings.TrimSpace(r["remarks"]) != ""
	case KindCodeReview:
		// Compatibility for a bulk draft saved by the short-lived build that
		// used the old codereview type before bulk received its own type.
		_, err := ParseCodeReviewItems(r["remarks"])
		return err == nil
	case KindIndependent:
		return strings.TrimSpace(r["parent_repo"]) != "" &&
			strings.TrimSpace(r["parent_title"]) != ""
	}
	return false
}

// IssueRefFromAny accepts an issue however it comes to hand — pasted browser
// URL or typed owner/repo#123 — and returns the ref the rest of the app uses.
//
// A pasted URL is what actually happens: you are looking at the issue, so you
// copy the address bar. Refusing that and demanding the shorthand would be the
// app being difficult about a string it can read perfectly well.
//
// Every spelling GitHub itself uses is read the same way:
//
//	owner/repo#123
//	owner/repo/issues/123
//	github.com/owner/repo/issues/123
//	https://github.com/owner/repo/issues/123#issuecomment-1
//
// A pull-request link is refused here: work is logged against the issue a PR
// belongs to, and only Code Review knows how to find that issue.
func IssueRefFromAny(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", GhErr("Paste the issue link, or type it as owner/repo#123.")
	}
	if m := issRe.FindStringSubmatch(s); m != nil {
		return fmt.Sprintf("%s/%s#%s", m[1], m[2], m[3]), nil
	}
	if m := githubPathRe.FindStringSubmatch(s); m != nil {
		ref := fmt.Sprintf("%s/%s#%s", m[1], m[2], m[4])
		if !strings.EqualFold(m[3], "issues") {
			return "", GhErr("%s is a pull request. Paste the issue it belongs to, or log it as a Code Review.", ref)
		}
		return ref, nil
	}
	return "", GhErr("Issue should look like owner/repo#123, owner/repo/issues/123 or its GitHub link, got %q.", s)
}

// githubPathRe reads an issue or pull-request path with or without the
// github.com host in front: owner, repo, "issues" or "pull", number. Anything
// after the number (/files, #issuecomment-1, ?diff=split) is ignored.
var githubPathRe = regexp.MustCompile(`(?i)^(?:(?:https?://)?(?:www\.)?github\.com/)?([\w.-]+)/([\w.-]+)/(issues|pull|pulls)/(\d+)(?:[/#?].*)?$`)

// EnsureIssueRef fills in the issue a saved row should be pushed against, for
// the kinds whose issue is only decided at push time. It returns the patch to
// apply to the row, or an empty patch when there is nothing to resolve.
func EnsureIssueRef(cfg Config, r Row) (Row, error) {
	if strings.TrimSpace(r["issue"]) != "" {
		return Row{}, nil // already filed somewhere
	}
	body := strings.TrimSpace(r["issue_body"])
	if body == "" {
		body = strings.TrimSpace(r["remarks"])
	}
	if body == "" {
		body = strings.TrimSpace(r["description"])
	}

	switch r["type"] {
	case KindMeeting:
		ref, err := EnsureMeetingIssue(cfg, r["date"], body)
		if err != nil {
			return Row{}, err
		}
		// The meeting issue carries the Worklog fields itself — it is the work,
		// not a parent for it, and a "Worklog: <date>" child under a one-day
		// meeting issue would be a tree of two saying one thing.
		return Row{"issue": ref, "mode": "issue"}, nil

	case KindBulkReview:
		ref, err := EnsureCodeReviewIssue(cfg, r["date"], body)
		if err != nil {
			return Row{}, err
		}
		return Row{"issue": ref, "mode": "issue"}, nil

	case KindCodeReview:
		// Compatibility with bulk drafts saved before kindBulkReview existed.
		if _, err := ParseCodeReviewItems(r["remarks"]); err == nil {
			ref, err := EnsureCodeReviewIssue(cfg, r["date"], body)
			if err != nil {
				return Row{}, err
			}
			return Row{"issue": ref, "mode": "issue"}, nil
		}

	case KindIndependent:
		repo := strings.TrimSpace(r["parent_repo"])
		title := strings.TrimSpace(r["parent_title"])
		if repo == "" || title == "" {
			return Row{}, GhErr("This entry has no parent issue to create — set a repo and a title on it.")
		}
		_, ref, err := createTypedIssue(repo, title, body, independentIssueType)
		if ref == "" {
			return Row{}, err
		}
		return Row{"issue": ref, "mode": "subissue"}, nil
	}
	return Row{}, nil
}
