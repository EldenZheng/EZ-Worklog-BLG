package logwork

import (
	"encoding/json"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"sort"
	"strings"
	"unicode"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// SupportWeekSummary resolves the repositories behind this week's support
// issues once each. It does not scrape unrelated repos or copy a partial list.
func SupportWeekSummary(items []backend.WorklogItem) (string, error) {
	repos := map[string]string{}
	for _, item := range items {
		for _, raw := range []string{item.ParentURL, item.URL} {
			ref, err := backend.IssueRefFromAny(raw)
			if err != nil {
				continue
			}
			owner, repo, _, _ := backend.SplitIssue(ref)
			if !strings.EqualFold(owner, "BigLedger-Support") {
				continue
			}
			repos[strings.ToLower(owner+"/"+repo)] = owner + "/" + repo
			break
		}
	}
	names := map[string]string{}
	for _, repo := range repos {
		out, err := backend.Gh([]string{"api", "repos/" + repo, "--jq", "{name: .name}"})
		if err != nil {
			return "", fmt.Errorf("read support repository %s: %w", repo, err)
		}
		var got struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil || got.Name == "" {
			return "", fmt.Errorf("could not read support repository %s", repo)
		}
		name := supportCustomerName(got.Name)
		names[strings.ToLower(name)] = name
	}
	var list []string
	for _, name := range names {
		list = append(list, name)
	}
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i]) < strings.ToLower(list[j]) })
	prefix := "For the short summary this week:\n- "
	if len(list) == 0 {
		return prefix + "No ad hoc issues logged for BigLedger Support this week.", nil
	}
	joined := strings.Join(list, ", ")
	if len(list) == 2 {
		joined = list[0] + " and " + list[1]
	}
	if len(list) > 2 {
		joined = strings.Join(list[:len(list)-1], ", ") + ", and " + list[len(list)-1]
	}
	return prefix + "I did some adhoc issues for " + joined + ".", nil
}

func supportCustomerName(repo string) string {
	name := strings.ToLower(repo)
	for _, suffix := range []string{"-general-tasks", "-finance", "-operations", "-intranet", "-accounting", "-support", "-helpdesk"} {
		name = strings.TrimSuffix(name, suffix)
	}
	words := strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(name))
	for i, word := range words {
		if len(word) <= 4 {
			words[i] = strings.ToUpper(word)
		} else {
			r := []rune(word)
			r[0] = unicode.ToUpper(r[0])
			words[i] = string(r)
		}
	}
	return strings.Join(words, " ")
}

func copySummaryButton(ui *shell.UI, items []backend.WorklogItem, state string) *widget.Button {
	b := widget.NewButtonWithIcon("Copy summary", theme.ContentCopyIcon(), nil)
	if state == "loading" || state == "off" || state == "error" {
		b.Disable()
	}
	b.OnTapped = func() {
		b.Disable()
		b.SetText("Reading repos…")
		var text string
		var fetchErr error
		ui.Async(func() error { text, fetchErr = SupportWeekSummary(items); return nil }, func() {
			b.Enable()
			b.SetText("Copy summary")
			if fetchErr != nil {
				ui.Errf(fetchErr)
				return
			}
			fyne.CurrentApp().Clipboard().SetContent(text)
			b.SetText("Copied")
		})
	}
	return b
}
