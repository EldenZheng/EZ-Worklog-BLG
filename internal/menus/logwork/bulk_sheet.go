package logwork

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// bulkReviewInput is one PR row of a bulk review sheet.
type bulkReviewInput struct {
	pr, mins      *widget.Entry
	notes         *widget.Entry         // the PR's own worklog remarks, a point per line
	issue         *widget.Label         // the PR's issue, as a caption
	issues        []backend.ReviewIssue // the issues the PR is for, written in the description
	view          fyne.CanvasObject
	lookupVersion int
}

// bulkSheet is the Bulk Review sheet: a row per PR with its minutes and its
// worklog remarks, Add PR and a bin per row, Suggest per row and Suggest all.
// The Bulk Review form and the editor of a saved bulk review both use it.
type bulkSheet struct {
	ui     *shell.UI
	View   fyne.CanvasObject
	rows   *fyne.Container
	morph  *shell.Morph
	total  *widget.Label
	inputs []*bulkReviewInput
	// relayout lays the page or popup out again as rows grow; say shows a
	// line to the user; OnTotal hears the minutes total whenever it changes.
	relayout func()
	say      func(string)
	OnTotal  func(int)
}

func newBulkSheet(ui *shell.UI, relayout func(), say func(string)) *bulkSheet {
	s := &bulkSheet{ui: ui, relayout: relayout, say: say, rows: container.NewVBox()}
	// The sheet eases open and shut as rows are added and removed.
	s.morph = shell.NewMorph(s.rows)
	s.total = widget.NewLabelWithStyle("Total: 0 min", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})
	header := container.New(widgets.NewRatioRow(0.82, 0.18),
		widget.NewLabelWithStyle("PR link", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Minutes", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	addPR := widget.NewButtonWithIcon("Add PR", theme.ContentAddIcon(), func() { s.grow(func() { s.addRow() }) })
	// Every row with a PR and no points yet, three PRs at a time.
	suggestAll := widget.NewButton("Suggest all", func() {
		var empty []*bulkReviewInput
		for _, in := range s.inputs {
			if strings.TrimSpace(in.notes.Text) == "" {
				empty = append(empty, in)
			}
		}
		s.suggest(empty)
	})
	s.View = container.NewVBox(header, s.morph,
		container.NewBorder(nil, nil, container.NewHBox(addPR, suggestAll), nil, s.total))
	s.addRow()
	return s
}

func (s *bulkSheet) grow(change func()) {
	s.morph.Snapshot()
	change()
	s.morph.Change(0, func(bool) { s.relayout() })
}

// addRow adds an empty row: no PR yet, ten minutes.
func (s *bulkSheet) addRow() *bulkReviewInput {
	in := &bulkReviewInput{
		pr: widget.NewEntry(), mins: widget.NewEntry(), issue: widget.NewLabel(""),
		notes: widget.NewMultiLineEntry(),
	}
	in.notes.SetPlaceHolder("- what you checked on this PR, a point per line")
	in.notes.Wrapping = fyne.TextWrapWord
	in.notes.SetMinRowsVisible(2)
	// The remarks box grows with what is typed, a row at a time, as the
	// other forms' remarks do.
	growNotes := shell.AutoGrow(in.notes, 2, 0, s.relayout)
	in.notes.OnChanged = func(string) { growNotes() }
	in.pr.SetPlaceHolder(PullRequestPlaceholder)
	in.mins.SetPlaceHolder("10")
	in.mins.SetText("10") // a PR is ten minutes unless said otherwise
	in.issue.Wrapping = fyne.TextWrapWord
	in.issue.SizeName = theme.SizeNameCaptionText
	in.pr.OnChanged = func(source string) { s.prChanged(in, source) }
	in.mins.OnChanged = func(string) { s.updateTotal() }
	suggest := widget.NewButton("Suggest", func() { s.suggest([]*bulkReviewInput{in}) })
	del := shell.FadeDeleteIcon(func() { s.removeRow(in) })
	// The PR and its minutes; under them the issue with Suggest beside it;
	// then the PR's remarks across the full width.
	link := container.New(widgets.NewRatioRow(0.82, 0.18),
		in.pr, container.NewBorder(nil, nil, nil, del, in.mins))
	issue := container.NewBorder(nil, nil, nil, container.NewVBox(suggest, layout.NewSpacer()), in.issue)
	// A divider under each PR, so one PR's points do not run into the next.
	in.view = container.NewVBox(link, issue, in.notes, widget.NewSeparator())
	s.inputs = append(s.inputs, in)
	s.rows.Add(in.view)
	s.relayout()
	return in
}

// prChanged reads what was typed or pasted into a row's PR field. A line
// copied from a bulk review ("…/pull/12 (10m)", with its points) fills the
// row, and further rows for any further PRs, instead of sitting there unread.
func (s *bulkSheet) prChanged(in *bulkReviewInput, source string) {
	if pasted := backend.ParsePastedReviews(source); len(pasted) > 0 {
		if _, plain := backend.PullRequestURLFromAny(source); !plain {
			s.fill(in, pasted)
			return
		}
	}
	in.lookupVersion++
	version := in.lookupVersion
	in.issues = nil
	in.issue.SetText("")
	if prURL, ok := backend.PullRequestURLFromAny(source); ok {
		in.issue.SetText("Finding referenced issue…")
		shell.AfterTyping(func() {
			if version != in.lookupVersion || in.pr.Text != source {
				return
			}
			var lookup backend.CodeReviewLookup
			var lookupErr error
			var infos map[string]backend.IssueInfo
			s.ui.Async(func() error {
				lookup, lookupErr = backend.FetchCodeReviewLookup(prURL)
				if lookupErr == nil && len(lookup.IssueRefs) > 0 {
					infos, _ = backend.FetchIssueInfos(lookup.IssueRefs)
				}
				return nil
			}, func() {
				if version != in.lookupVersion || in.pr.Text != source {
					return
				}
				switch {
				case lookupErr != nil && len(in.issues) > 0:
					// A saved row keeps the issues it was saved with.
					in.issue.SetText(issueCaption(in.issues))
				case lookupErr != nil:
					in.issue.SetText(lookupErr.Error())
				case len(lookup.IssueRefs) == 0:
					in.issue.SetText("No referenced issue found")
				default:
					in.issues = nil
					for _, ref := range lookup.IssueRefs {
						in.issues = append(in.issues, backend.ReviewIssue{Ref: ref, Title: infos[ref].Title})
					}
					in.issue.SetText(issueCaption(in.issues))
				}
				s.relayout()
			})
		})
	}
	s.updateTotal()
}

// fill puts items into in and then into new rows after it.
func (s *bulkSheet) fill(in *bulkReviewInput, items []backend.CodeReviewItem) {
	s.grow(func() {
		for i, item := range items {
			target := in
			if i > 0 {
				target = s.addRow()
			}
			s.setRow(target, item)
		}
	})
	s.updateTotal()
}

// issueCaption names a PR's issues by title where the title is known.
func issueCaption(issues []backend.ReviewIssue) string {
	lines := make([]string, 0, len(issues))
	for _, issue := range issues {
		if title := strings.TrimSpace(issue.Title); title != "" {
			lines = append(lines, title+"  ·  "+issue.Ref)
		} else {
			lines = append(lines, "Issue: "+issue.Ref)
		}
	}
	return strings.Join(lines, "\n")
}

func (s *bulkSheet) setRow(in *bulkReviewInput, item backend.CodeReviewItem) {
	in.pr.SetText(item.URL)
	if len(item.Issues) > 0 {
		in.issues = item.Issues
	}
	in.mins.SetText(strconv.Itoa(item.Minutes))
	if len(item.Notes) > 0 {
		in.notes.SetText("- " + strings.Join(item.Notes, "\n- "))
	} else {
		in.notes.SetText("")
	}
}

func (s *bulkSheet) removeRow(gone *bulkReviewInput) {
	for i, in := range s.inputs {
		if in == gone {
			s.inputs = append(s.inputs[:i], s.inputs[i+1:]...)
			s.grow(func() { s.rows.Remove(in.view) })
			break
		}
	}
	if len(s.inputs) == 0 {
		s.addRow()
	}
	s.updateTotal()
	s.relayout()
}

// Total is the minutes of every row with a PR in it.
func (s *bulkSheet) Total() int {
	total := 0
	for _, in := range s.inputs {
		if _, ok := backend.PullRequestURLFromAny(in.pr.Text); !ok {
			continue
		}
		if mins, err := strconv.Atoi(strings.TrimSpace(in.mins.Text)); err == nil && mins > 0 {
			total += mins
		}
	}
	return total
}

func (s *bulkSheet) updateTotal() {
	total := s.Total()
	s.total.SetText(fmt.Sprintf("Total: %d min", total))
	if s.OnTotal != nil {
		s.OnTotal(total)
	}
}

// Items reads the sheet. Every PR needs minutes and its worklog remarks.
func (s *bulkSheet) Items() ([]backend.CodeReviewItem, error) {
	var items []backend.CodeReviewItem
	for i, in := range s.inputs {
		pr := strings.TrimSpace(in.pr.Text)
		minuteText := strings.TrimSpace(in.mins.Text)
		// Minutes start at 10, so a row without a PR is just an empty row.
		if pr == "" {
			continue
		}
		if minuteText == "" {
			return nil, fmt.Errorf("row %d needs both a PR link and minutes", i+1)
		}
		url, ok := backend.PullRequestURLFromAny(pr)
		if !ok {
			return nil, fmt.Errorf("row %d is not a pull request (owner/repo#123 or its link)", i+1)
		}
		mins, err := strconv.Atoi(minuteText)
		if err != nil || mins <= 0 {
			return nil, fmt.Errorf("row %d needs minutes greater than zero", i+1)
		}
		var notes []string
		for _, line := range strings.Split(in.notes.Text, "\n") {
			line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-*•"))
			if line != "" {
				notes = append(notes, line)
			}
		}
		if len(notes) == 0 {
			return nil, fmt.Errorf("row %d needs its worklog remarks: a point or two on what you reviewed", i+1)
		}
		items = append(items, backend.CodeReviewItem{URL: url, Minutes: mins, Issues: in.issues, Notes: notes})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("enter at least one PR link and its minutes")
	}
	return items, nil
}

// Reset empties the sheet to one blank row.
func (s *bulkSheet) Reset() {
	s.inputs = nil
	s.rows.Objects = nil
	s.addRow()
	s.rows.Refresh()
	s.updateTotal()
}

// SetItems fills the sheet from a saved bulk review, a row per PR.
func (s *bulkSheet) SetItems(items []backend.CodeReviewItem) {
	s.inputs = nil
	s.rows.Objects = nil
	for _, item := range items {
		s.setRow(s.addRow(), item)
	}
	if len(s.inputs) == 0 {
		s.addRow()
	}
	s.rows.Refresh()
	s.updateTotal()
}

// suggest asks the AI for each row's points from its PR, a few at a time
// under one progress bar, and fills them in when all are back.
func (s *bulkSheet) suggest(targets []*bulkReviewInput) {
	cli := s.ui.Cfg.TitleCLI
	if strings.TrimSpace(cli) == "" {
		s.say("Pick an AI in Settings (AI for titles) to suggest a PR's points.")
		return
	}
	var urls []string
	var rows []*bulkReviewInput
	for _, in := range targets {
		if url, ok := backend.PullRequestURLFromAny(in.pr.Text); ok {
			urls = append(urls, url)
			rows = append(rows, in)
		}
	}
	if len(urls) == 0 {
		s.say("Paste a PR link first; its points are written from the PR.")
		return
	}
	progress, stop := s.ui.PushSpinner("Asking " + titleCLILabel(cli) + " about the PRs…")
	progress(backend.FetchProgress{Done: 0, Total: len(urls), Stage: fmt.Sprintf("0 of %d PRs", len(urls))})
	notes := make([][]string, len(urls))
	errs := make([]error, len(urls))
	s.ui.Async(func() error {
		var mu sync.Mutex
		finished := 0
		jobs := make(chan int)
		var wg sync.WaitGroup
		for w := 0; w < titleWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					notes[i], errs[i] = backend.SuggestPRNotes(cli, urls[i])
					mu.Lock()
					finished++
					done := finished
					mu.Unlock()
					progress(backend.FetchProgress{Done: done, Total: len(urls), Stage: fmt.Sprintf("%d of %d PRs", done, len(urls))})
				}
			}()
		}
		for i := range urls {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		return nil
	}, func() {
		stop()
		var failed []string
		for i, in := range rows {
			if errs[i] != nil {
				failed = append(failed, errs[i].Error())
				continue
			}
			in.notes.SetText("- " + strings.Join(notes[i], "\n- "))
		}
		if len(failed) > 0 {
			s.say(strings.Join(failed, "  |  "))
		}
		s.relayout()
	})
}
