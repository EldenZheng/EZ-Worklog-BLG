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
		"Each entry is pushed as a Worklog issue under the issue it belongs to. Its minutes go in Actual Worklog (mins).",
		"A Worklog's date is the day it is created, so an entry is pushed on its own day only. Push shows on that day; an entry for a later day waits for it, and a past day cannot be pushed.",
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
