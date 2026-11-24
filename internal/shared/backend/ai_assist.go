package backend

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// PullRequestBrief is what the AI reads to write up a review of a pull
// request: the PR itself, and what you said on it.
type PullRequestBrief struct {
	Title string
	Body  string
	Files []string
	// Reviews are your own reviews of it ("APPROVED: lgtm"), and Comments
	// your inline comments ("path: text").
	Reviews  []string
	Comments []string
}

var viewerLogin struct {
	sync.Once
	login string
}

// ghLogin is the signed-in GitHub user, asked once.
func ghLogin() string {
	viewerLogin.Do(func() {
		if out, err := Gh([]string{"api", "user", "--jq", ".login"}); err == nil {
			viewerLogin.login = strings.TrimSpace(out)
		}
	})
	return viewerLogin.login
}

// FetchPullRequestBrief reads a PR's title, description and changed files,
// with your reviews and inline comments on it. A variable so tests can stand
// in for gh.
var FetchPullRequestBrief = func(prURL string) (PullRequestBrief, error) {
	out, err := Gh([]string{"pr", "view", prURL, "--json", "title,body,files,reviews"})
	if err != nil {
		return PullRequestBrief{}, err
	}
	var got struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Files []struct {
			Path      string `json:"path"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
		} `json:"files"`
		Reviews []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			State string `json:"state"`
			Body  string `json:"body"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		return PullRequestBrief{}, GhErr("could not read the PR: %v", err)
	}
	brief := PullRequestBrief{Title: got.Title, Body: got.Body}
	for _, f := range got.Files {
		brief.Files = append(brief.Files, fmt.Sprintf("%s (+%d -%d)", f.Path, f.Additions, f.Deletions))
	}
	me := ghLogin()
	for _, r := range got.Reviews {
		if me != "" && strings.EqualFold(r.Author.Login, me) {
			brief.Reviews = append(brief.Reviews, strings.TrimSpace(r.State+": "+clip(r.Body, 400)))
		}
	}
	// Inline comments are not in pr view; a failure here only loses them.
	if me != "" {
		if m := githubPathRe.FindStringSubmatch(prURL); m != nil {
			out, err := Gh([]string{"api", fmt.Sprintf("repos/%s/%s/pulls/%s/comments?per_page=100", m[1], m[2], m[4])})
			var comments []struct {
				Path string `json:"path"`
				Body string `json:"body"`
				User struct {
					Login string `json:"login"`
				} `json:"user"`
			}
			if err == nil && json.Unmarshal([]byte(out), &comments) == nil {
				for _, c := range comments {
					if strings.EqualFold(c.User.Login, me) && len(brief.Comments) < 15 {
						brief.Comments = append(brief.Comments, c.Path+": "+clip(strings.ReplaceAll(c.Body, "\n", " "), 300))
					}
				}
			}
		}
	}
	return brief, nil
}

// clip cuts s to n bytes at most, on a character boundary.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}

// PRNotesPrompt asks for a few points on reviewing a PR, written as the
// reviewer's own worklog rather than as a summary of the PR.
func PRNotesPrompt(prURL string, brief PullRequestBrief) string {
	var b strings.Builder
	b.WriteString("Write 2 or 3 short bullet points for my code-review worklog. They record my review, " +
		"not a summary of the pull request, so do not list what the PR adds file by file.\n" +
		"1. What I reviewed: the change in a few words, without its issue ref, which the worklog " +
		"already links (\"Reviewed the Cons/Note column in Line Items\").\n" +
		"2. What I checked, verified or flagged in it (\"Checked it stays hidden unless the setting is on\").\n" +
		"3. The outcome, from my review below: \"Approved, no blocking issues\" when I approved without comments, " +
		"\"Approved after ...\", \"Requested changes: ...\" or \"Flagged ...\" when I raised something. " +
		"Leave the outcome out if I have not reviewed it yet.\n" +
		"Start each with \"- \", keep each under 110 characters, no headings, names or closing remarks. " +
		"Reply with the bullet points only.\n\n")
	fmt.Fprintf(&b, "Pull request: %s\nTitle: %s\n", prURL, strings.TrimSpace(brief.Title))
	if len(brief.Reviews) > 0 {
		fmt.Fprintf(&b, "\nMy reviews:\n%s\n", strings.Join(brief.Reviews, "\n"))
	}
	if len(brief.Comments) > 0 {
		fmt.Fprintf(&b, "\nMy inline comments:\n%s\n", strings.Join(brief.Comments, "\n"))
	}
	if body := strings.TrimSpace(brief.Body); body != "" {
		if len(body) > 4000 {
			body = body[:4000]
		}
		fmt.Fprintf(&b, "\nDescription:\n%s\n", body)
	}
	if len(brief.Files) > 0 {
		files := brief.Files
		if len(files) > 40 {
			files = files[:40]
		}
		fmt.Fprintf(&b, "\nChanged files:\n%s\n", strings.Join(files, "\n"))
	}
	return b.String()
}

// SuggestPRNotes asks the AI for a reviewed PR's worklog points, one per
// line without the bullet.
func SuggestPRNotes(command, prURL string) ([]string, error) {
	brief, err := FetchPullRequestBrief(prURL)
	if err != nil {
		return nil, err
	}
	out, label, err := askCLI(command, PRNotesPrompt(prURL, brief))
	if err != nil {
		return nil, err
	}
	notes := CleanBullets(out)
	if len(notes) == 0 {
		return nil, fmt.Errorf("%s gave no points for %s", label, prURL)
	}
	return notes, nil
}

// CleanBullets keeps an answer's bullet lines, without their bullets. An
// answer with no bullets at all is taken a line at a time.
func CleanBullets(out string) []string {
	var bullets, lines []string
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "```") {
			continue
		}
		if note, ok := reviewNoteLine(l); ok {
			if note = strings.Trim(note, "*` "); note != "" {
				bullets = append(bullets, note)
			}
			continue
		}
		lines = append(lines, l)
	}
	if len(bullets) > 0 {
		return bullets
	}
	return lines
}

// RemarksPrompt asks for long notes cut down to Worklog remarks.
func RemarksPrompt(text string) string {
	return "Condense these worklog notes into Worklog remarks of at most 950 characters. " +
		"One point per line, each starting with \"- \". Keep the concrete facts: what was done, " +
		"names of things, numbers and outcomes. Drop filler. Reply with the points only.\n\n" +
		"Notes:\n" + strings.TrimSpace(text) + "\n"
}

// SummarizeRemarks asks the AI to cut text down to Worklog remarks that fit
// RemarkCap. An answer still too long loses whole points from the end.
func SummarizeRemarks(command, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("nothing to summarise yet")
	}
	out, label, err := askCLI(command, RemarksPrompt(text))
	if err != nil {
		return "", err
	}
	points := CleanBullets(out)
	if len(points) == 0 {
		return "", fmt.Errorf("%s gave no remarks", label)
	}
	return FitRemarks(points), nil
}

// FitRemarks writes points as "- " lines, keeping as many whole points as
// fit RemarkCap.
func FitRemarks(points []string) string {
	var kept []string
	size := 0
	for _, p := range points {
		line := "- " + p
		add := len(line)
		if len(kept) > 0 {
			add++ // the newline before it
		}
		if size+add > RemarkCap {
			break
		}
		kept = append(kept, line)
		size += add
	}
	if len(kept) == 0 && len(points) > 0 {
		line := "- " + points[0]
		return strings.ToValidUTF8(line[:RemarkCap], "")
	}
	return strings.Join(kept, "\n")
}
