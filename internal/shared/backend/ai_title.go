package backend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// TitleCLI is a command-line AI assistant the app can ask for a Worklog
// title. Each runs on the account it is signed in with: Claude Code on a
// Claude subscription uses that plan, not API credit, unless it was set up
// with an API key.
type TitleCLI struct {
	Command string // what Settings saves and what is run
	Label   string
	args    []string // the prompt itself goes in on stdin
}

// TitleCLIs are the assistants the app knows how to ask, in the order
// Settings lists them. The prompt goes in on stdin, never as an argument: an
// npm-installed CLI is a .cmd shim on Windows, and cmd.exe mangles arguments
// that span lines.
var TitleCLIs = []TitleCLI{
	// Not saved as a session: a suggestion is never resumed.
	{Command: "claude", Label: "Claude Code", args: []string{"-p", "--no-session-persistence"}},
	{Command: "codex", Label: "Codex", args: []string{"exec", "--skip-git-repo-check", "-"}},
	{Command: "gemini", Label: "Gemini CLI", args: nil},
}

// lookPath is exec.LookPath, held in a variable so tests can stand in for it.
var lookPath = exec.LookPath

// AvailableTitleCLIs lists the known assistants installed on this machine.
func AvailableTitleCLIs() []TitleCLI {
	var found []TitleCLI
	for _, c := range TitleCLIs {
		if _, err := lookPath(c.Command); err == nil {
			found = append(found, c)
		}
	}
	return found
}

// TitleCLIByCommand finds a known assistant by the command Settings saved.
func TitleCLIByCommand(command string) (TitleCLI, bool) {
	for _, c := range TitleCLIs {
		if strings.EqualFold(c.Command, strings.TrimSpace(command)) {
			return c, true
		}
	}
	return TitleCLI{}, false
}

// TitlePrompt asks for one short sentence naming the work in remarks. A code
// review is asked what was reviewed: the app puts "Name - Code Review:" in
// front itself.
func TitlePrompt(kind, issueTitle, remarks string) string {
	what := "the work done"
	if kind == KindCodeReview {
		what = "what was reviewed (the app adds \"Code Review:\" in front, so do not)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Write a GitHub issue title for a worklog. Summarise %s in one short sentence: "+
		"past tense where it fits, at most 80 characters, no trailing full stop, no quotes, no names. "+
		"Reply with the title only, on one line.\n\n", what)
	if t := strings.TrimSpace(issueTitle); t != "" {
		fmt.Fprintf(&b, "Parent issue: %s\n\n", t)
	}
	fmt.Fprintf(&b, "Worklog remarks:\n%s\n", strings.TrimSpace(remarks))
	return b.String()
}

// runTitleCLI runs the assistant; a variable so tests can stand in for it.
// A version too old for one of its flags is asked again without that flag.
var runTitleCLI = func(ctx context.Context, cli TitleCLI, prompt string) (string, error) {
	out, err := runCLIOnce(ctx, cli, cli.args, prompt)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unknown option") {
		var plain []string
		for _, a := range cli.args {
			if a != "--no-session-persistence" {
				plain = append(plain, a)
			}
		}
		return runCLIOnce(ctx, cli, plain, prompt)
	}
	return out, err
}

// runCLIOnce runs the assistant with args, the prompt on stdin. Nothing is
// written by the app: the prompt goes in on stdin and the answer comes back
// on stdout.
func runCLIOnce(ctx context.Context, cli TitleCLI, args []string, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, cli.Command, args...)
	// Out of any project, so no repository instructions steer the answer.
	cmd.Dir = os.TempDir()
	cmd.Stdin = strings.NewReader(prompt)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	hideConsole(cmd)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s took too long to answer", cli.Label)
		}
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s failed: %s", cli.Label, lastLine(msg))
	}
	return out.String(), nil
}

// SuggestWorklogTitle asks the assistant Settings names for a title.
func SuggestWorklogTitle(command, kind, issueTitle, remarks string) (string, error) {
	if strings.TrimSpace(remarks) == "" {
		return "", fmt.Errorf("write the remarks first; the title is made from them")
	}
	out, label, err := askCLI(command, TitlePrompt(kind, issueTitle, remarks))
	if err != nil {
		return "", err
	}
	title := CleanSuggestedTitle(out)
	if title == "" {
		return "", fmt.Errorf("%s gave no title", label)
	}
	return title, nil
}

// askCLI puts prompt to the assistant Settings names and returns its answer
// and its name.
func askCLI(command, prompt string) (string, string, error) {
	cli, ok := TitleCLIByCommand(command)
	if !ok {
		return "", "", fmt.Errorf("pick an AI CLI in Settings first (AI for titles)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := runTitleCLI(ctx, cli, prompt)
	return out, cli.Label, err
}

// CleanSuggestedTitle keeps the answer's last non-empty line (Codex prints
// its working before it) without quotes, a "Title:" label or a full stop.
func CleanSuggestedTitle(out string) string {
	var line string
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
		}
	}
	if rest, ok := cutPrefixFold(line, "title:"); ok {
		line = strings.TrimSpace(rest)
	}
	line = strings.Trim(line, "\"'`*“”")
	line = strings.TrimSpace(strings.TrimSuffix(line, "."))
	if r := []rune(line); len(r) > 120 {
		line = strings.TrimSpace(string(r[:120]))
	}
	return line
}

// RowNeedsWorklogTitle reports whether an entry still waiting to push should
// get a Worklog title first. Meetings and bulk reviews are titled by their
// day, so they never need one.
func RowNeedsWorklogTitle(r Row) bool {
	return r["pushed_at"] == "" && SOPApplies(r["date"]) && strings.TrimSpace(r["worklog_title"]) == "" &&
		r["type"] != KindMeeting && r["type"] != KindBulkReview && !RowIsBulkCodeReview(r)
}
