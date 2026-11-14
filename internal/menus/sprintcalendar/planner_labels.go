package sprintcalendar

import (
	"fmt"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// blockTitle names one of an issue's blocks on a day. An issue planned more
// than once on the same day, as meetings often are, gets #1, #2, … in start
// order so the blocks can be told apart.
func blockTitle(title string, n, count int) string {
	if count > 1 {
		return fmt.Sprintf("%s#%d", title, n)
	}
	return title
}

// blockNumbers numbers the blocks of key on date by position: index to
// (n, count). Blocks on other days are left out.
func blockNumbers(blocks []backend.PlanBlock, date string) map[int][2]int {
	var on []int
	for i, b := range blocks {
		if b.Date == date {
			on = append(on, i)
		}
	}
	sort.SliceStable(on, func(i, j int) bool { return backend.PlanBlockBefore(blocks[on[i]], blocks[on[j]]) })
	out := map[int][2]int{}
	for n, i := range on {
		out[i] = [2]int{n + 1, len(on)}
	}
	return out
}

// plannedLine is how the Log Work tooltip names a block: the issue's title
// with its repo#number in its project's colour, or the meeting's numbered name
// with its description. A pull request shows the issue it works on.
func plannedLine(k *shell.KanbanUI, c backend.SprintCandidate, block backend.PlanBlock, n, count int) shell.PlannedLine {
	line := shell.PlannedLine{Title: blockTitle(issueTitle(k, c.Item), n, count), Detail: block.Note, Minutes: block.Minutes}
	if b, it, ok := k.BoardItem(c); ok {
		line.DetailColor = k.Accent(b, it)
	}
	if c.Key != shell.PlannerMeetingKey {
		if ref := shell.ShortIssueRef(issueLink(k, c.Item)); ref != "" {
			line.Detail = ref
		}
	}
	return line
}

// PlannedLines lists what makes up a day's purple "planned" figure: each
// scheduled block, in start order, with its allocated minutes.
func PlannedLines(ui *shell.UI, date string, logged []backend.WorklogItem, drafts []backend.Row) []shell.PlannedLine {
	k := ui.Kanban
	if k == nil {
		return nil
	}
	items := worklogPlannedItems(k)
	totals := DayTotals(k, date, items, PlannerDayWork{Logged: logged, Drafts: drafts})
	type entry struct {
		block backend.PlanBlock
		line  shell.PlannedLine
	}
	var entries []entry
	seen := map[string]bool{}
	for _, c := range items {
		if seen[c.Key] || totals.plannedBy[c.Key] <= 0 {
			continue
		}
		seen[c.Key] = true
		blocks := k.Plan.Tasks[c.Key].Blocks
		numbers := blockNumbers(blocks, date)
		for i, b := range blocks {
			if b.Date == date {
				nc := numbers[i]
				entries = append(entries, entry{b, plannedLine(k, c, b, nc[0], nc[1])})
			}
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return backend.PlanBlockBefore(entries[i].block, entries[j].block) })
	var out []shell.PlannedLine
	for _, e := range entries {
		out = append(out, e.line)
	}
	return out
}

// editMeetingNote asks what a meeting block is for. The description shows on
// the block and in Log Work's planned tooltip.
func editMeetingNote(k *shell.KanbanUI, c backend.SprintCandidate, index int) {
	blocks := k.Plan.Tasks[c.Key].Blocks
	if index < 0 || index >= len(blocks) {
		return
	}
	want := blocks[index]
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Weekly meeting, 1:1, demo…")
	entry.SetText(want.Note)
	var d dialog.Dialog
	save := func() {
		p, err := backend.SetPlannerBlockNote(k.Plan, c.Key, index, want, entry.Text)
		if err != nil {
			k.Ui.Errf(err)
			return
		}
		shell.CloseDialog(d, k.Ui.Win)
		if k.Save(p) {
			k.SetMessage("Meeting description saved locally.")
			shell.Hooks.KanbanUIDrawKeepingCalendarScroll(k)
		}
	}
	entry.OnSubmitted = func(string) { save() }
	cancel := widget.NewButton("Cancel", func() { shell.CloseDialog(d, k.Ui.Win) })
	content := container.NewVBox(entry, shell.DialogButtons(cancel, widget.NewButton("Save", save)))
	d = dialog.NewCustomWithoutButtons("Meeting description", content, k.Ui.Win)
	d.Resize(fyne.NewSize(380, d.MinSize().Height))
	shell.ShowDialog(d, k.Ui.Win)
	k.Ui.Win.Canvas().Focus(entry)
}
