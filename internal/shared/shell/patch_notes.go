package shell

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// PatchNotesID names the notes below. Change it with the notes, and they open
// again for everyone, including those who ticked "Don't show this again".
const PatchNotesID = "worklog-sop-2026-11-21"

// patchNoteSections are the notes, a heading and its points each.
var patchNoteSections = []struct {
	Heading string
	Points  []string
}{
	{"The new worklog SOP (from 21 Nov 2026)", []string{
		"Each entry is pushed as a Worklog issue under the issue it belongs to. Its minutes go in Worklog Reported(mins).",
		"Today's and past entries can be pushed. Future entries wait until their date. A late Worklog's description starts with 'Logged for YYYY-MM-DD', a blank line, then the work details; GitHub's creation date remains the day it is pushed.",
		"Entries dated before 21 Nov 2026 keep the old method.",
	}},
	{"Commits name their Worklog", []string{
		"After a push, the app adds the Worklog's reference (Refs owner/repo#N) to the entry's commits, rewriting them in your clone and force-pushing the branch with a lease.",
		"Set your clones under Settings > Repo folders first.",
		"Commits already merged cannot be changed. The app checks GitHub before the push, tags the entry Already merged and has its Worklog list them instead.",
		"Commits not pushed yet are read from your clones too and tagged Pending Push. On the log's day the app adds the reference before they reach GitHub, then pushes the branch: no force-push.",
		"Once every commit of yours on a branch is logged, the app offers a pull request into the repository's default branch (customization/senwave on the senwave forks), titled after the issue.",
	}},
	{"Entries saved before the SOP", []string{
		"Entries that were waiting to be pushed, with commits already on GitHub, are tagged Legacy. Their Worklog lists those commits instead.",
	}},
	{"Reading your time", []string{
		"Status, Report and Log List read Worklog issues from 21 Nov 2026, and the project board before it.",
		"Late Worklogs count on their first-line 'Logged for' date, including plain descriptions created outside the app and pushes after the selected month or payroll period. Older app footer dates still work. Worklogs without a date note use their Malaysia creation day.",
		"The SOP allows up to 15 hours logged per day. Each Worklog's 'Logged for' date backfills that day before pay is capped at 480 minutes per day. Additional time stays visible without increasing that day's pay; unmarked excess stays on its creation day.",
		"Status and Report each have a Worklog dropdown beside Refresh from GitHub, switching their totals between Worklog Reported(mins) and Worklog Approved(mins). Reported is the default; an unset approval counts as zero. The menu stays open after selection while the check, calendars, chart and figures animate.",
	}},
}

// ShowPatchNotes opens the patch notes unless the user turned them off for
// this PatchNotesID. Ticking "Don't show this again" before OK keeps them shut.
func ShowPatchNotes(ui *UI) {
	if ui == nil || ui.Win == nil || ui.Cfg.PatchNotesSeen == PatchNotesID {
		return
	}
	notes := container.NewVBox()
	for i, section := range patchNoteSections {
		if i > 0 {
			notes.Add(VGap(theme.InnerPadding()))
		}
		notes.Add(SectionTitle(section.Heading))
		for _, point := range section.Points {
			line := widget.NewLabel("•  " + point)
			line.Wrapping = fyne.TextWrapWord
			notes.Add(line)
		}
	}
	hide := widget.NewCheck("Don't show this again", nil)
	var popup *widget.PopUp
	ok := widget.NewButton("OK, understood", func() {
		ClosePopUp(popup)
		if hide.Checked {
			ui.Cfg.PatchNotesSeen = PatchNotesID
			if ui.Store != nil {
				if err := ui.Store.SaveConfig(ui.Cfg); err != nil {
					ui.Errf(err)
				}
			}
		}
	})
	footer := container.NewBorder(nil, nil, GoldTickChoice(hide), nil, DialogButtons(ConfirmButton(ok)))
	popup = ui.ShowLongNotice("What's new", notes, footer)
}
