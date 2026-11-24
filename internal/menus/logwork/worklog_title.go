package logwork

import (
	"fmt"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// worklogTitleField is the title the entry's Worklog takes on GitHub, with a
// Suggest button when Settings names an AI CLI. A code review's title goes
// after "Name - Code Review:"; a blank one falls back to the date.
type worklogTitleField struct {
	entry *widget.Entry
	View  fyne.CanvasObject
}

// newWorklogTitleField builds the field. kind, issueTitle and text are read
// when Suggest is pressed; say reports what happened.
func newWorklogTitleField(ui *shell.UI, title string, kind func() string, issueTitle func() string,
	text func() string, say func(string)) *worklogTitleField {
	f := &worklogTitleField{entry: widget.NewEntry()}
	f.entry.SetText(title)
	f.setKind(kind())
	if strings.TrimSpace(ui.Cfg.TitleCLI) == "" {
		f.View = shell.Labeled("Worklog title", f.entry)
		return f
	}
	suggest := widget.NewButton("Suggest", func() {
		suggestWithProgress(ui, kind(), issueTitle(), text(), func(got string, err error) {
			if err != nil {
				say(err.Error())
				return
			}
			f.entry.SetText(got)
		})
	})
	f.View = shell.Labeled("Worklog title", container.NewBorder(nil, nil, nil, suggest, f.entry))
	return f
}

// setKind words the placeholder for what the title will be pushed as.
func (f *worklogTitleField) setKind(kind string) {
	if kind == backend.KindCodeReview {
		f.entry.SetPlaceHolder("What was reviewed; pushed after \"Name - Code Review:\"")
		return
	}
	f.entry.SetPlaceHolder("What you worked on, in one short sentence (blank: Worklog: <date>)")
}

// Text is the title as typed, trimmed.
func (f *worklogTitleField) Text() string { return strings.TrimSpace(f.entry.Text) }

// textForTitle is what Suggest reads: the issue description when it is kept
// apart from the remarks, since it says more, else the remarks.
func textForTitle(t *commitText) string {
	remarks, body := strings.TrimSpace(t.desc.Text), ""
	if t.Split() {
		body = remarks
		remarks = strings.TrimSpace(t.Remarks.Text)
	}
	return backend.OrDefault(body, remarks)
}

// titleTag marks an entry waiting to push with no Worklog title. With an AI
// CLI set in Settings, tapping the tag asks it for one and saves it; without
// one, the tile's own tap opens the editor, where the title is typed.
func titleTag(ui *shell.UI, r backend.Row, refresh func()) fyne.CanvasObject {
	if !backend.RowNeedsWorklogTitle(r) {
		return nil
	}
	tag := shell.TitleNotSetTag()
	if strings.TrimSpace(ui.Cfg.TitleCLI) == "" {
		return tag
	}
	return shell.NewTappable(tag, func() {
		suggestWithProgress(ui, r["type"], RowTitle(ui, r), backend.OrDefault(r["issue_body"], r["remarks"]),
			func(got string, err error) {
				if err == nil {
					err = ui.Store.UpdateRow(r["id"], backend.Row{"worklog_title": got})
				}
				if err != nil {
					ui.Errf(err)
					return
				}
				refresh()
			})
	})
}

// suggestWithProgress asks the AI for one title under a progress bar, then
// hands the answer to done on the UI thread.
func suggestWithProgress(ui *shell.UI, kind, parent, text string, done func(string, error)) {
	cli := ui.Cfg.TitleCLI
	_, stop := ui.PushSpinner("Asking " + titleCLILabel(cli) + " for a Worklog title…")
	var got string
	var err error
	ui.Async(func() error {
		got, err = backend.SuggestWorklogTitle(cli, kind, parent, text)
		return nil
	}, func() {
		stop()
		done(got, err)
	})
}

// titleCLILabel names the AI Settings picked, for progress text.
func titleCLILabel(command string) string {
	if c, ok := backend.TitleCLIByCommand(command); ok {
		return c.Label
	}
	return "the AI"
}

// suggestingTitles is set while Suggest titles runs, so a second press does
// not start another.
var suggestingTitles struct {
	sync.Mutex
	running bool
}

// titleWorkers is how many AI requests Suggest titles keeps going at once.
const titleWorkers = 3

// suggestTitlesButton asks the AI for a title for every entry waiting to push
// without one, a few at a time, and saves each as it comes. Nil when no AI is
// set in Settings or no entry needs a title.
func suggestTitlesButton(ui *shell.UI, rows []backend.Row) fyne.CanvasObject {
	if strings.TrimSpace(ui.Cfg.TitleCLI) == "" {
		return nil
	}
	var need []backend.Row
	for _, r := range rows {
		if backend.RowNeedsWorklogTitle(r) {
			need = append(need, r)
		}
	}
	if len(need) == 0 {
		return nil
	}
	return widget.NewButton(fmt.Sprintf("Suggest titles (%d)", len(need)), func() {
		suggestTitles(ui, need)
	})
}

// suggestTitles runs the AI over rows off the UI thread under a progress bar
// counting them off, then redraws and says which could not be done.
func suggestTitles(ui *shell.UI, rows []backend.Row) {
	suggestingTitles.Lock()
	if suggestingTitles.running {
		suggestingTitles.Unlock()
		return
	}
	suggestingTitles.running = true
	suggestingTitles.Unlock()

	cli := ui.Cfg.TitleCLI
	parents := make([]string, len(rows))
	for i, r := range rows {
		parents[i] = RowTitle(ui, r)
	}
	progress, stop := ui.PushSpinner("Asking " + titleCLILabel(cli) + " for Worklog titles…")
	total := len(rows)
	progress(backend.FetchProgress{Done: 0, Total: total, Stage: fmt.Sprintf("0 of %d titled", total)})

	var failed []string
	var mu sync.Mutex
	finished := 0
	ui.Async(func() error {
		jobs := make(chan int)
		var wg sync.WaitGroup
		for w := 0; w < titleWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					r := rows[i]
					got, err := backend.SuggestWorklogTitle(cli, r["type"], parents[i],
						backend.OrDefault(r["issue_body"], r["remarks"]))
					if err == nil {
						err = ui.Store.UpdateRow(r["id"], backend.Row{"worklog_title": got})
					}
					mu.Lock()
					if err != nil {
						failed = append(failed, shell.Truncate(parents[i], 60)+": "+err.Error())
					}
					finished++
					done := finished
					mu.Unlock()
					progress(backend.FetchProgress{Done: done, Total: total, Stage: fmt.Sprintf("%d of %d titled", done, total)})
				}
			}()
		}
		for i := range rows {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		return nil
	}, func() {
		suggestingTitles.Lock()
		suggestingTitles.running = false
		suggestingTitles.Unlock()
		stop()
		DrawRecent(ui)
		if len(failed) > 0 {
			shell.ShowInformation("Some titles were not suggested",
				strings.Join(failed, "\n")+"\n\nTap Title not yet set on an entry to try it again, or type it in the editor.", ui.Win)
		}
	})
}

// aiRemarks is commitText.Summarize for the AI Settings names, or nil when
// none is set (the description is then copied, as before).
func aiRemarks(ui *shell.UI) func(string, func(string)) {
	return func(text string, done func(string)) {
		cli := ui.Cfg.TitleCLI
		if strings.TrimSpace(cli) == "" {
			done("")
			return
		}
		_, stop := ui.PushSpinner("Asking " + titleCLILabel(cli) + " to fit the remarks…")
		var short string
		var err error
		ui.Async(func() error {
			short, err = backend.SummarizeRemarks(cli, text)
			return nil
		}, func() {
			stop()
			if err != nil {
				ui.Errf(err)
			}
			done(short)
		})
	}
}
