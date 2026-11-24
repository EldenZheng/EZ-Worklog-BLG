package backend

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var meetingRecordingLine = regexp.MustCompile(`(?im)^\s*Meeting Recording Link:\s*https?://\S+`)

func MissingMeetingRecording(r Row) bool {
	if r["type"] != KindMeeting || strings.TrimSpace(r["meeting_recording"]) != "" {
		return false
	}
	// Older entries put the recording in their notes before it had a field.
	return !meetingRecordingLine.MatchString(r["issue_body"]) && !meetingRecordingLine.MatchString(r["remarks"]) && !meetingRecordingLine.MatchString(r["description"])
}

func MeetingEntryBody(r Row) (string, error) {
	if title := strings.TrimSpace(r["meeting_title"]); title != "" && (strings.ContainsAny(title, "\r\n") || len(title) > 256) {
		return "", GhErr("Enter a meeting title of 1 to 256 characters on one line.")
	}
	body := strings.TrimSpace(OrDefault(r["issue_body"], r["remarks"]))
	link := strings.TrimSpace(r["meeting_recording"])
	if link != "" {
		u, err := url.Parse(link)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.ContainsAny(link, "\r\n") {
			return "", GhErr("Enter a valid http or https meeting recording link.")
		}
		body += "\n\nMeeting Recording Link: " + link
	}
	if r["meeting_title"] != "" && r["id"] != "" {
		body += "\n\n" + meetingEntryMarker(r)
	}
	body = strings.TrimSpace(body)
	if len(body) > BodyCap {
		return "", GhErr("Meeting content and recording link exceed %d characters.", BodyCap)
	}
	return body, nil
}

func meetingEntryMarker(r Row) string { return "<!-- ez-worklog-meeting:" + r["id"] + " -->" }

func ensureNamedMeetingIssue(cfg Config, r Row) (string, error) {
	if !ValidRepo(cfg.MeetingRepo) {
		return "", GhErr("Set the meeting repository to owner/repo in Settings before pushing.")
	}
	title := strings.TrimSpace(r["meeting_title"])
	if title == "" || strings.ContainsAny(title, "\r\n") || len(title) > 256 {
		return "", GhErr("Enter a meeting title of 1 to 256 characters on one line.")
	}
	body, err := MeetingEntryBody(r)
	if err != nil {
		return "", err
	}
	// A stable entry marker makes retries safe without combining distinct
	// meetings which happen to have the same title, even on the same day.
	if r["id"] != "" {
		out, err := Gh([]string{"issue", "list", "--repo", cfg.MeetingRepo, "--state", "all", "--limit", "100", "--search", fmt.Sprintf("%q in:body", "ez-worklog-meeting:"+r["id"]), "--json", "number,body"})
		if err != nil {
			return "", err
		}
		var issues []struct {
			Number int
			Body   string
		}
		if err := json.Unmarshal([]byte(out), &issues); err != nil {
			return "", err
		}
		for _, issue := range issues {
			if strings.Contains(issue.Body, meetingEntryMarker(r)) {
				return fmt.Sprintf("%s#%d", cfg.MeetingRepo, issue.Number), nil
			}
		}
	}
	_, ref, err := createTypedIssue(cfg.MeetingRepo, title, loggedForBody(body, r["date"]), meetingIssueType)
	if ref != "" {
		return ref, nil
	}
	return "", err
}

// Meetings own their issue. The recording belongs in the issue body and does
// not consume the remarks' separate 1024-character allowance.
func PushMeetingEntry(cfg Config, r Row, progress ...func(FetchProgress)) (PushResult, error) {
	if err := worklogPushDateError(r["date"], r["issue_url"]); err != nil {
		return PushResult{}, err
	}
	body, err := MeetingEntryBody(r)
	if err != nil {
		return PushResult{}, err
	}
	body = loggedForBody(body, r["date"])
	owner, repo, number, err := SplitIssue(r["issue"])
	if err != nil {
		return PushResult{}, err
	}
	issueURL := fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, number)
	args := []string{"issue", "edit", issueURL, "--body", body}
	if title := strings.TrimSpace(r["meeting_title"]); title != "" {
		if strings.ContainsAny(title, "\r\n") || len(title) > 256 {
			return PushResult{}, GhErr("Enter a meeting title of 1 to 256 characters on one line.")
		}
		args = append(args, "--title", title)
	}
	if _, err := Gh(args); err != nil {
		return PushResult{}, err
	}
	return PushEntry(cfg, r["issue"], r["date"], r["owner"], r.Minutes(), r["remarks"], body, "issue", r["issue_url"], SubTitleBaseFor(cfg, r), progress...)
}
