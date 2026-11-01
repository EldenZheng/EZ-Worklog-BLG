package logwork

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"time"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ============================ Log tab ============================

func BuildLogTab(ui *shell.UI) fyne.CanvasObject {
	kind := "commit"

	// -- commit pane --
	// The same button the other tabs carry, in the same corner: one way to ask
	// GitHub again, wherever you happen to be standing.
	fetchBtn := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), nil)
	ui.PendingBox = container.NewVBox(
		widget.NewLabel("Loading your commits from GitHub…"),
	)
	search := widget.NewEntry()
	search.SetPlaceHolder("Search issues, titles or commits")
	search.SetText(ui.LogSearch)
	search.OnChanged = func(value string) {
		ui.LogSearch = value
		if ui.PendingLoaded {
			RenderPending(ui)
		}
		DrawRecent(ui)
	}
	commitPane := container.NewVBox(
		container.NewBorder(nil, nil, nil, fetchBtn, search), ui.PendingBox)

	// The list loads itself on first open; this button is the manual refresh.
	// Both halves of this tab come from GitHub — the commits waiting here, and
	// the line at the foot scoring today against the project — and that line
	// reads the Status cache. Refreshing only the commits left it stale.
	fetchBtn.OnTapped = func() {
		ui.LoadPending(true)
		ui.RefreshTodayScore()
	}

	// -- manual panes --
	// Hand-logged work shares one form and reveals only the fields its kind
	// needs: meeting notes, a bulk code-review list, an existing issue link, or
	// the repo and title for an issue that does not exist yet.
	// Same date picker the row-editor popup uses, so the tab and the popup ask
	// the "which day" question the same way — with the month grid painted by
	// how full each day already is.
	mDate := NewWorklogDatePicker(ui, shell.Today())
	// Description is a textarea now — meeting write-ups run to several lines
	// and the old single-line field truncated visibly at the caret. Only shown
	// for meetings; the other kinds title themselves and keep it off screen.
	mDesc := widget.NewMultiLineEntry()
	mDesc.SetMinRowsVisible(3)
	mDesc.SetPlaceHolder("What was it? (meeting notes)")
	mMin := widget.NewEntry()
	mMin.SetPlaceHolder("minutes")
	mRemarks := widget.NewMultiLineEntry()
	mRemarks.SetMinRowsVisible(4)
	mRemarks.SetPlaceHolder("- one bullet per thing done; this becomes the worklog remarks")
	// Live character counter — same "%d / %d" reading the row editor's popup
	// uses, so a manual entry never surprises anyone with a silent trim on push.
	mRemarksCount := widget.NewLabel("")
	mRemarksCountUpdate := func() {
		mRemarksCount.SetText(fmt.Sprintf("%d / %d", len(mRemarks.Text), backend.RemarkCap))
	}
	mRemarks.OnChanged = func(string) { mRemarksCountUpdate() }
	mRemarksCountUpdate()
	mMsg := widget.NewLabel("")

	// Mode selector — the same one the row editor grows. Shown for Worklog and a
	// single Code Review, where the user is filing against an existing issue.
	// Independent has a fixed creation flow; Meeting and Bulk Review always use
	// their daily issue, and Commits carries its own mode per group.
	mMode := widget.NewSelect([]string{"Worklog sub-issue", "The issue itself"}, nil)
	if strings.EqualFold(ui.Cfg.DefaultMode, "issue") {
		mMode.SetSelected("The issue itself")
	} else {
		mMode.SetSelected("Worklog sub-issue")
	}
	mModeVal := func() string {
		if mMode.Selected == "The issue itself" {
			return "issue"
		}
		return "subissue"
	}
	// The "Push as" pair rides alongside the action buttons on the same line —
	// it lives on the row a push is committed from, not stacked above it — so a
	// wrapped VBox would sit awkwardly at the foot of the tab. Held in one box
	// so both pieces show or hide together. mMode is wrapped in a fixed-width
	// grid because widget.Select's own MinSize was tight enough to render
	// "Worklog sub-issue" as "Worklog su…".
	modeBox := container.New(layout.NewGridWrapLayout(fyne.NewSize(190, mMode.MinSize().Height)), mMode)
	mPushAs := container.NewHBox(
		widget.NewLabelWithStyle("Push as", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		modeBox,
	)
	mPushAs.Hide()

	// "Worklog": an issue that already exists.
	mIssue := widget.NewEntry()
	mIssue.SetPlaceHolder("https://github.com/exampleorg/repo/issues/123  or  exampleorg/repo#123")
	otherRow := shell.Labeled("Issue link", mIssue)
	otherRow.Hide()

	// A single code review starts from the thing actually in hand: its PR URL.
	// The linked issue is resolved from GitHub's metadata or the PR's Refs text;
	// a direct issue link remains accepted when a PR names none or several.
	mReviewPR := widget.NewEntry()
	mReviewPR.SetPlaceHolder("https://github.com/exampleorg/repo/pull/123  (or paste the issue link directly)")
	mReviewTarget := widget.NewLabel("")
	mReviewTarget.Wrapping = fyne.TextWrapWord
	reviewLookupVersion := 0
	var reviewResolvedSource, reviewResolvedRef, reviewResolvedTitle string
	startReviewLookup := func(source string, version int) {
		if version != reviewLookupVersion || mReviewPR.Text != source {
			return
		}
		mReviewTarget.SetText("Finding the referenced issue on GitHub…")
		var ref string
		var info backend.IssueInfo
		var err error
		ui.Async(func() error {
			ref, _, err = backend.ResolveSingleCodeReviewTarget(source)
			if err == nil {
				info, _ = backend.FetchIssueInfo(ref)
			}
			return nil
		}, func() {
			if version != reviewLookupVersion || mReviewPR.Text != source {
				return
			}
			if err != nil {
				mReviewTarget.SetText(err.Error())
				return
			}
			if ui.IssueInfo == nil {
				ui.IssueInfo = map[string]backend.IssueInfo{}
			}
			if strings.TrimSpace(info.Title) != "" {
				ui.IssueInfo[ref] = info
			}
			reviewResolvedSource = source
			reviewResolvedRef = ref
			reviewResolvedTitle = strings.TrimSpace(info.Title)
			mReviewTarget.SetText(backend.CodeReviewTargetLabel(ref, info.Title))
		})
	}
	singleReviewRow := container.NewVBox(
		shell.Labeled("PR or issue link", mReviewPR),
		mReviewTarget,
	)
	singleReviewRow.Hide()
	mReviewPR.OnChanged = func(source string) {
		reviewLookupVersion++
		reviewResolvedSource, reviewResolvedRef, reviewResolvedTitle = "", "", ""
		version := reviewLookupVersion
		mReviewTarget.SetText("")
		if _, ok := backend.CanonicalPullRequestURL(source); ok {
			// Wait for a paste/typing burst to settle. Without the debounce,
			// typing PR 123 would request PR 1, then 12, then 123.
			mReviewTarget.SetText("Finding the referenced issue on GitHub…")
			time.AfterFunc(350*time.Millisecond, func() {
				defer func() { _ = recover() }() // the app may close meanwhile
				fyne.Do(func() { startReviewLookup(source, version) })
			})
			return
		}
		// Direct issue links/refs need no network lookup and can show at once.
		if ref, err := backend.IssueRefFromAny(source); err == nil {
			mReviewTarget.SetText("Worklog issue: " + ref)
		}
	}

	// "Bulk Review" is a two-column growing sheet. Completing the current
	// PR + Minutes pair appends the next blank row automatically. The resulting
	// items still feed the same issue body and Worklog remarks as before.
	type bulkReviewInput struct {
		pr, mins      *widget.Entry
		issue         *widget.Label
		lookupVersion int
	}
	var bulkInputs []*bulkReviewInput
	bulkRows := container.NewVBox()
	bulkTotal := widget.NewLabelWithStyle("Total: 0 min", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})
	bulkHeader := container.New(widgets.NewRatioRow(0.82, 0.18),
		widget.NewLabelWithStyle("PR link", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Minutes", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	codeReviewRow := container.NewVBox(bulkHeader, bulkRows, bulkTotal)
	codeReviewRow.Hide()

	bulkReviewItems := func() ([]backend.CodeReviewItem, error) {
		var items []backend.CodeReviewItem
		for i, input := range bulkInputs {
			pr := strings.TrimSpace(input.pr.Text)
			minuteText := strings.TrimSpace(input.mins.Text)
			if pr == "" && minuteText == "" {
				continue
			}
			if pr == "" || minuteText == "" {
				return nil, fmt.Errorf("row %d needs both a PR link and minutes", i+1)
			}
			url, ok := backend.CanonicalPullRequestURL(pr)
			if !ok {
				return nil, fmt.Errorf("row %d is not a GitHub pull-request link", i+1)
			}
			mins, err := strconv.Atoi(minuteText)
			if err != nil || mins <= 0 {
				return nil, fmt.Errorf("row %d needs minutes greater than zero", i+1)
			}
			items = append(items, backend.CodeReviewItem{URL: url, Minutes: mins})
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("enter at least one PR link and its minutes")
		}
		return items, nil
	}

	var addBulkReviewRow func()
	updateBulkReviewTotal := func() {
		total := 0
		for _, input := range bulkInputs {
			if _, ok := backend.CanonicalPullRequestURL(input.pr.Text); !ok {
				continue
			}
			if mins, err := strconv.Atoi(strings.TrimSpace(input.mins.Text)); err == nil && mins > 0 {
				total += mins
			}
		}
		if total == 0 {
			mMin.SetText("")
		} else {
			mMin.SetText(strconv.Itoa(total))
		}
		bulkTotal.SetText(fmt.Sprintf("Total: %d min", total))
		if len(bulkInputs) == 0 {
			return
		}
		last := bulkInputs[len(bulkInputs)-1]
		if _, ok := backend.CanonicalPullRequestURL(last.pr.Text); !ok {
			return
		}
		mins, err := strconv.Atoi(strings.TrimSpace(last.mins.Text))
		if err == nil && mins > 0 {
			addBulkReviewRow()
		}
	}

	addBulkReviewRow = func() {
		input := &bulkReviewInput{
			pr: widget.NewEntry(), mins: widget.NewEntry(), issue: widget.NewLabel(""),
		}
		input.pr.SetPlaceHolder("https://github.com/exampleorg/repo/pull/123")
		input.mins.SetPlaceHolder("10")
		input.issue.Wrapping = fyne.TextWrapWord
		input.pr.OnChanged = func(source string) {
			input.lookupVersion++
			version := input.lookupVersion
			input.issue.SetText("")
			if prURL, ok := backend.CanonicalPullRequestURL(source); ok {
				input.issue.SetText("Finding referenced issue…")
				time.AfterFunc(350*time.Millisecond, func() {
					defer func() { _ = recover() }()
					fyne.Do(func() {
						if version != input.lookupVersion || input.pr.Text != source {
							return
						}
						var lookup backend.CodeReviewLookup
						var lookupErr error
						ui.Async(func() error {
							lookup, lookupErr = backend.FetchCodeReviewLookup(prURL)
							return nil
						}, func() {
							if version != input.lookupVersion || input.pr.Text != source {
								return
							}
							switch {
							case lookupErr != nil:
								input.issue.SetText(lookupErr.Error())
							case len(lookup.IssueRefs) == 0:
								input.issue.SetText("No referenced issue found")
							default:
								input.issue.SetText("Issue: " + strings.Join(lookup.IssueRefs, ", "))
							}
						})
					})
				})
			}
			updateBulkReviewTotal()
		}
		input.mins.OnChanged = func(string) { updateBulkReviewTotal() }
		bulkInputs = append(bulkInputs, input)
		bulkRows.Add(container.New(widgets.NewRatioRow(0.82, 0.18),
			container.NewVBox(input.pr, input.issue),
			container.NewVBox(input.mins, layout.NewSpacer())))
		shell.Relayout(ui.LogBody)
	}
	resetBulkReviewRows := func() {
		bulkInputs = nil
		bulkRows.Objects = nil
		bulkTotal.SetText("Total: 0 min")
		addBulkReviewRow()
		bulkRows.Refresh()
	}
	addBulkReviewRow()

	// "Independent": an issue that does not exist yet.
	repoPick := ui.NewRepoPicker()
	mTitle := widget.NewEntry()
	mTitle.SetPlaceHolder("Title for the new issue")
	indepRow := container.NewVBox(
		container.New(widgets.NewRatioRow(0.5, 0.5),
			shell.Labeled("Parent repo", repoPick.Widget()),
			shell.Labeled("New issue title", mTitle)),
		widget.NewLabelWithStyle(
			"The issue is created on the first push, not now — nothing reaches GitHub until you push.",
			fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
	)
	indepRow.Hide()

	// Description sits on its own row so a textarea has room to breathe. Hidden
	// for the two kinds that title themselves ("Worklog: <date>") since the
	// field would only ever repeat the date they already picked.
	descRow := shell.Labeled("Description", mDesc)
	descRow.Hide()

	// autoDescFor is the generated title non-meeting entries wear. Code Review
	// names its daily issue; Worklog and Independent keep the normal worklog title.
	autoDescFor := func(k, iso string) string {
		if k == backend.KindBulkReview {
			return backend.DailyCodeReviewTitle(ui.Cfg, iso)
		}
		if k == backend.KindCodeReview {
			return backend.CodeReviewTitle(iso)
		}
		return "Worklog: " + iso
	}
	bulkReviewFilingHint := func(iso string) string {
		if !backend.ValidRepo(ui.Cfg.BulkCodeReviewRepo) {
			return "Set the bulk review repository in Settings before pushing."
		}
		return fmt.Sprintf("Creates/reuses its own issue in %s: %s.",
			ui.Cfg.BulkCodeReviewRepo, backend.DailyCodeReviewTitle(ui.Cfg, iso))
	}

	// Both buttons share this closure. push=false saves a draft (like the popup
	// editor's "Save"); push=true saves and immediately pushes to GitHub via
	// resolveForPush + PushEntry, the same path the row editor uses. Reset the
	// form only after a successful save so a failure keeps the fields for a
	// second attempt.
	mSaveResolved := func(push bool, codeReviewIssue, codeReviewIssueTitle string) {
		var reviewItems []backend.CodeReviewItem
		mins, _ := strconv.Atoi(strings.TrimSpace(mMin.Text))
		if kind == backend.KindBulkReview {
			var err error
			reviewItems, err = bulkReviewItems()
			if err != nil {
				mMsg.SetText(err.Error())
				return
			}
			mins = backend.CodeReviewMinutes(reviewItems)
		}
		if mins <= 0 {
			mMsg.SetText("Minutes must be more than zero.")
			return
		}
		date := mDate.ISO()
		if date == "" {
			mMsg.SetText("Pick a date.")
			return
		}
		// Description is the visible textarea for a meeting; for Worklog, Code
		// Review and Independent the field is hidden and the auto-title is
		// written in.
		desc := strings.TrimSpace(mDesc.Text)
		if kind == backend.KindOther || kind == backend.KindCodeReview || kind == backend.KindBulkReview || kind == backend.KindIndependent {
			desc = autoDescFor(kind, date)
		}
		remarksText := strings.TrimSpace(mRemarks.Text)
		if kind == backend.KindBulkReview {
			remarksText = backend.CodeReviewBody(reviewItems)
		}
		if len(remarksText) > backend.RemarkCap {
			mMsg.SetText(fmt.Sprintf("Remarks are %d chars — trim to %d first.", len(remarksText), backend.RemarkCap))
			return
		}
		row := backend.Row{
			"date": date, "minutes": strconv.Itoa(mins),
			"type": kind, "description": desc,
			"remarks": remarksText,
			// Every push reads Row["owner"] into the project's Worklog Owner
			// field; a row saved with an empty owner leaves that column blank.
			// The Settings value is the same one the row editor seeds its own
			// Owner input from.
			"owner": strings.TrimSpace(ui.Cfg.WorklogOwner),
		}
		switch kind {
		case backend.KindMeeting:
			// No issue yet on purpose: the day's meeting issue is found or
			// created at push time, so logging two meetings on one day lands
			// both on the same issue instead of opening a second.
			row["mode"] = "issue"
		case backend.KindOther:
			ref, err := backend.IssueRefFromAny(mIssue.Text)
			if err != nil {
				mMsg.SetText(err.Error())
				return
			}
			row["issue"] = ref
			row["mode"] = mModeVal()
		case backend.KindCodeReview:
			if strings.TrimSpace(codeReviewIssue) == "" {
				mMsg.SetText("Paste a PR or issue link for this review.")
				return
			}
			row["issue"] = codeReviewIssue
			row["issue_title"] = strings.TrimSpace(codeReviewIssueTitle)
			row["mode"] = mModeVal()
		case backend.KindBulkReview:
			// The daily issue is found or created on the first push, matching
			// the meeting flow and keeping GitHub untouched for drafts.
			row["mode"] = "issue"
		case backend.KindIndependent:
			repo := repoPick.Value()
			title := strings.TrimSpace(mTitle.Text)
			if repo == "" || title == "" {
				mMsg.SetText("Pick a repo and give the new issue a title.")
				return
			}
			row["parent_repo"], row["parent_title"] = repo, title
			row["mode"] = mModeVal()
		}
		made, err := ui.Store.AppendRows([]backend.Row{row})
		if err != nil {
			ui.Errf(err)
			return
		}
		saved := made[0]

		resetForm := func() {
			mDesc.SetText("")
			mMin.SetText("")
			mRemarks.SetText("")
			mTitle.SetText("")
			mIssue.SetText("")
			mReviewPR.SetText("")
			mReviewTarget.SetText("")
			resetBulkReviewRows()
			DrawRecent(ui)
			DrawWeekStrip(ui)
		}

		if !push {
			mMsg.SetText("Saved as draft — push it from the entry below when you are ready.")
			resetForm()
			ui.ShowDraftSavedInfo()
			return
		}

		body := strings.TrimSpace(saved["remarks"])
		if body == "" {
			body = saved["description"]
		}
		if body == "" {
			mMsg.SetText("Saved as draft — nothing to push: remarks and description are both empty.")
			resetForm()
			return
		}

		mMsg.SetText("Pushing…")
		pushProgress, stop := ui.PushSpinner("Pushing to GitHub…")
		var res backend.PushResult
		var perr error
		ui.Async(func() error {
			resolved, err := resolveForPush(ui, saved)
			if err != nil {
				perr = err
				return nil
			}
			saved = resolved
			res, perr = backend.PushEntry(ui.Cfg, saved["issue"], saved["date"], saved["owner"],
				mins, body, saved["issue_body"], backend.OrDefault(saved["mode"], "issue"), saved["issue_url"],
				backend.SubTitleBaseFor(saved), pushProgress)
			return nil
		}, func() {
			stop()
			if perr != nil {
				mMsg.SetText("Saved as draft, but push failed: " + perr.Error() +
					" Retry from the entry below.")
				DrawRecent(ui)
				return
			}
			summary, _ := applyPushResult(ui, saved["id"], res)
			ui.RefreshAfterPush(saved["date"])
			mMsg.SetText(strings.ReplaceAll(summary, "\n\n", "  |  "))
			resetForm()
		})
	}

	// Single reviews resolve their PR to its referenced issue before the normal
	// save path runs. Save draft does the same lookup so the draft itself is
	// already attached to the correct issue and can be pushed later unchanged.
	mSave := func(push bool) {
		if kind != backend.KindCodeReview {
			mSaveResolved(push, "", "")
			return
		}
		source := mReviewPR.Text
		if strings.TrimSpace(source) == "" {
			mMsg.SetText("Paste the PR link (or the issue link directly).")
			return
		}
		// The debounced preview already did both GitHub reads in the common
		// paste-then-save flow. Reuse it instead of resolving the same PR and
		// issue a second time; clicking Save before the preview lands still
		// falls through to the complete lookup below.
		if source == reviewResolvedSource && reviewResolvedRef != "" {
			mReviewTarget.SetText(backend.CodeReviewTargetLabel(reviewResolvedRef, reviewResolvedTitle))
			mSaveResolved(push, reviewResolvedRef, reviewResolvedTitle)
			return
		}
		mMsg.SetText("Finding the issue referenced by this PR…")
		var ref string
		var info backend.IssueInfo
		var err error
		ui.Async(func() error {
			ref, _, err = backend.ResolveSingleCodeReviewTarget(source)
			if err == nil {
				// Title lookup is best-effort. Resolving the issue remains the
				// correctness boundary; a temporary title failure must not stop a
				// draft from being saved, and startup backfill will retry it.
				info, _ = backend.FetchIssueInfo(ref)
			}
			return nil
		}, func() {
			if kind != backend.KindCodeReview || mReviewPR.Text != source {
				return
			}
			if err != nil {
				mReviewTarget.SetText(err.Error())
				mMsg.SetText(err.Error())
				return
			}
			if ui.IssueInfo == nil {
				ui.IssueInfo = map[string]backend.IssueInfo{}
			}
			if strings.TrimSpace(info.Title) != "" {
				ui.IssueInfo[ref] = info
			}
			mReviewTarget.SetText(backend.CodeReviewTargetLabel(ref, info.Title))
			mSaveResolved(push, ref, info.Title)
		})
	}

	saveBtn := widget.NewButton("Save draft", func() { mSave(false) })
	pushBtn := widget.NewButton("Save & push", func() { mSave(true) })
	pushBtn.Importance = widget.HighImportance
	// Push-as sits at the left, actions at the right — the mode is the last
	// choice made before the button that acts on it, so they sit together on
	// one line rather than the mode stacked above.
	actionRow := container.NewBorder(nil, nil, mPushAs, container.NewHBox(saveBtn, shell.OvalPush(pushBtn)))

	// Date on its own row so the picker's month can drop under it without
	// fighting Minutes for width; Minutes rides alongside as a narrower field.
	dateField := labelledPicker("Date", mDate)
	minutesField := shell.Labeled("Minutes", mMin)
	dateRow := container.New(widgets.NewRatioRow(0.72, 0.28), dateField, minutesField)
	// Use the same day calculation shown in the commit popup. For Bulk Review,
	// mMin is the hidden sum of the individual spreadsheet rows, so the readout
	// reports GitHub minutes, pending drafts, and this review's live total without
	// asking for a second minutes value.
	dayReadout := dayFillReadout(ui, mDate, mMin, "")
	// Remarks with its counter on a right-aligned row underneath, so a wall of
	// text never surprises the pusher with a silent trim to remarkCap.
	remarksRow := container.NewVBox(
		shell.Labeled("Remarks", mRemarks),
		container.NewBorder(nil, nil, nil, mRemarksCount),
	)
	manualPane := container.NewVBox(
		dateRow,
		dayReadout,
		descRow,
		otherRow, singleReviewRow, codeReviewRow, indepRow,
		remarksRow,
		actionRow, mMsg,
	)
	manualPane.Hide()

	// -- kind selector --
	seg := widget.NewRadioGroup(
		[]string{"Commits", "Worklog", "Code Review", "Bulk Review", "Meeting", "Independent"}, func(s string) {
			wasBulkReview := kind == backend.KindBulkReview
			kind = map[string]string{
				"Commits": backend.KindCommit, "Meeting": backend.KindMeeting,
				"Code Review": backend.KindCodeReview,
				"Bulk Review": backend.KindBulkReview,
				"Worklog":     backend.KindOther, "Independent": backend.KindIndependent,
			}[s]
			if kind == backend.KindCommit {
				commitPane.Show()
				manualPane.Hide()
			} else {
				commitPane.Hide()
				manualPane.Show()
			}
			// Only the fields each kind needs, so the form never asks for an
			// issue link and a new issue's title at the same time.
			shell.ShowIf(descRow, kind == backend.KindMeeting)
			// The ratio layout hands the hidden Minutes share back to Date.
			shell.ShowIf(minutesField, kind != backend.KindBulkReview)
			shell.ShowIf(otherRow, kind == backend.KindOther)
			shell.ShowIf(singleReviewRow, kind == backend.KindCodeReview)
			shell.ShowIf(codeReviewRow, kind == backend.KindBulkReview)
			shell.ShowIf(indepRow, kind == backend.KindIndependent)
			shell.ShowIf(remarksRow, kind != backend.KindBulkReview)
			shell.ShowIf(mPushAs, kind == backend.KindOther || kind == backend.KindCodeReview)
			if kind == backend.KindBulkReview {
				mMin.Disable()
				if items, err := bulkReviewItems(); err == nil {
					mMin.SetText(strconv.Itoa(backend.CodeReviewMinutes(items)))
				} else {
					mMin.SetText("")
				}
			} else {
				mMin.Enable()
				if wasBulkReview {
					mMin.SetText("")
				}
			}
			switch kind {
			case backend.KindMeeting:
				mMsg.SetText("Files under " + backend.MeetingTitle(ui.Cfg, backend.OrDefault(mDate.ISO(), shell.Today())) + ".")
			case backend.KindBulkReview:
				mMsg.SetText(bulkReviewFilingHint(backend.OrDefault(mDate.ISO(), shell.Today())))
			case backend.KindCodeReview, backend.KindOther, backend.KindIndependent:
				mMsg.SetText("Sub-issue title: " + autoDescFor(kind, backend.OrDefault(mDate.ISO(), shell.Today())) + ".")
			default:
				mMsg.SetText("")
			}
		})
	seg.Horizontal = true
	seg.SetSelected("Commits")
	// The hint line names what the push will file under; picking a new date
	// should retitle it so it stays honest.
	prevDateChange := mDate.OnChanged
	mDate.OnChanged = func(iso string) {
		if prevDateChange != nil {
			prevDateChange(iso)
		}
		switch kind {
		case backend.KindMeeting:
			mMsg.SetText("Files under " + backend.MeetingTitle(ui.Cfg, backend.OrDefault(iso, shell.Today())) + ".")
		case backend.KindBulkReview:
			mMsg.SetText(bulkReviewFilingHint(backend.OrDefault(iso, shell.Today())))
		case backend.KindCodeReview, backend.KindOther, backend.KindIndependent:
			mMsg.SetText("Sub-issue title: " + autoDescFor(kind, backend.OrDefault(iso, shell.Today())) + ".")
		}
	}

	ui.RecentBox = container.NewVBox()
	// The week sits under the saved entries: its columns grow as tall as the
	// cards standing in them, so putting it first pushed the list itself off the
	// screen on a busy week.
	ui.WeekBox = container.NewVBox()
	ui.ShowPushed = widget.NewCheck("Also show entries already pushed this month", func(bool) {
		DrawRecent(ui)
	})
	if ui.DraftSort == "" {
		ui.DraftSort = DraftSortDuration
	}
	draftSortPick := widget.NewSelect([]string{DraftSortDuration, DraftSortCreated}, nil)
	draftSortPick.SetSelected(ui.DraftSort)
	draftSortPick.OnChanged = func(selected string) {
		ui.DraftSort = selected
		DrawRecent(ui)
	}
	draftSortControl := container.NewBorder(nil, nil, shell.GoldChoice(ui.ShowPushed),
		container.NewHBox(widget.NewLabel("Sort bubbles"),
			container.New(layout.NewGridWrapLayout(fyne.NewSize(190, draftSortPick.MinSize().Height)), draftSortPick)))
	choices := container.NewThemeOverride(widgets.WithPointerCursor(seg), shell.RadioTheme{Theme: theme.Current()})
	logCard := widget.NewCard("", "", container.NewVBox(choices, commitPane, manualPane))
	// The key sits at the foot of the tab, where the Status tab keeps its own:
	// it governs everything above it — the commits, the saved entries and the
	// week — so it belongs under the lot rather than over one of them.
	ui.LogLegend = container.NewVBox()
	recentCard := widget.NewCard("Saved locally, not pushed yet", "",
		container.NewVBox(draftSortControl, ui.RecentBox, widget.NewSeparator(),
			ui.WeekBox, ui.LogLegend))

	// The dragging layer sits above the scroll rather than inside it: the card
	// being carried has to pass over everything, and inside the scroll it would
	// be painted over by whatever the tab drew after it.
	ui.DragLayer = container.NewWithoutLayout()
	ui.LogProgress = shell.NewLoadingIndicator()
	// weekStripProgress lives with the strip below rather than at the top so
	// the calendar loading reads where the eye already is. Built here so
	// syncLoading can drive it from the moment the tab exists — drawWeekStrip
	// only needs to place the widget's view.
	if ui.WeekStripProgress == nil {
		ui.WeekStripProgress = shell.NewLoadingIndicator()
	}
	ui.LogBody = container.NewVBox(ui.LogProgress.View, logCard, recentCard)
	return container.NewStack(container.NewVScroll(ui.LogBody), ui.DragLayer)
}

// NewDateEntryISO builds a calendar-backed date field seeded from a YYYY-MM-DD
// string. Pair it with isoDate — never read .Text.
func NewDateEntryISO(iso string) *widget.DateEntry {
	e := widget.NewDateEntry()
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(iso)); err == nil {
		e.SetDate(&t)
	}
	return e
}

// IsoDate reads a DateEntry back as YYYY-MM-DD.
//
// The widget formats and parses in the system locale — "02/01/2006" by default
// — so writing its visible text into the CSV or the project's date field would
// silently record the wrong day. The picked *time.Time is the source of truth.
func IsoDate(e *widget.DateEntry) string {
	if e.Date != nil {
		return e.Date.Format("2006-01-02")
	}
	// .Date is only populated once the widget has a renderer wired up, so also
	// accept plain ISO text, which is what the rest of the app speaks.
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(e.Text)); err == nil {
		return t.Format("2006-01-02")
	}
	return ""
}

func RenderPending(ui *shell.UI) {
	// Log List counts these commits in its pending column, so it is redrawn
	// whenever the list behind it changes — otherwise that column keeps saying
	// "reading your commits" after they have arrived.
	defer shell.Hooks.DrawLogList(ui)

	res := ui.Pending
	var objs []fyne.CanvasObject
	for _, e := range res.Errors {
		objs = append(objs, shell.ColorLabel(e, theme.ColorNameError))
	}

	// Ignored commits are still fetched, so they can be handed back without a
	// second round trip. They just do not belong in the count of work to do.
	//
	// The key counts every commit fetched, including the orgs switched off: it
	// is how they get switched back on, and "exampleorg (0)" beside a list that
	// is only hiding them would be wrong twice over.
	byOrg := map[string]int{}
	var live, hidden []backend.Group
	filteredOut := 0
	for _, g := range res.Groups {
		org := strings.ToLower(shell.OrgOf(g.Issue))
		byOrg[org] += len(g.Commits)
		if !ui.PendingMatchesSearch(g) {
			continue
		}
		if !ui.OrgShown(org) {
			filteredOut += len(g.Commits)
			continue
		}
		if g.Ignored {
			hidden = append(hidden, g)
			continue
		}
		live = append(live, g)
	}
	remaining := 0
	for _, g := range live {
		remaining += len(g.Commits)
	}

	switch {
	case remaining > 0:
		rangeNote := ""
		if len(res.Range) == 2 {
			rangeNote = fmt.Sprintf(" from %s to %s", res.Range[0], res.Range[1])
		}
		objs = append(objs, widget.NewLabel(fmt.Sprintf(
			"%d unlogged commit(s)%s. Tap one to log it.", remaining, rangeNote)))
		objs = append(objs, sections(ui, live))
	// An empty list because of the filter is not an empty list. Saying there is
	// nothing to log, while the key at the foot of the tab counts commits
	// waiting under an org that was switched off, is the tab contradicting
	// itself.
	case strings.TrimSpace(ui.LogSearch) != "":
		objs = append(objs, widget.NewLabel("No pending commits match this search."))
	case filteredOut > 0:
		objs = append(objs, widget.NewLabel(fmt.Sprintf(
			"%d unlogged commit(s) hidden by the filter at the bottom of this tab.",
			filteredOut)))
	case len(res.Range) == 2:
		objs = append(objs, widget.NewLabel(fmt.Sprintf(
			"No unlogged commits between %s and %s.", res.Range[0], res.Range[1])))
	default:
		objs = append(objs, widget.NewLabel("No unlogged commits."))
	}

	// Ignoring is one click, so undoing it has to be too: the ignored tiles stay
	// one toggle away instead of only in a file on disk.
	if len(hidden) > 0 {
		label := fmt.Sprintf("Show %d ignored", len(hidden))
		if ui.ShowIgnored {
			label = fmt.Sprintf("Hide %d ignored", len(hidden))
		}
		toggle := widget.NewButton(label, func() {
			ui.ShowIgnored = !ui.ShowIgnored
			RenderPending(ui)
		})
		toggle.Importance = widget.LowImportance
		objs = append(objs, container.NewHBox(toggle))
		if ui.ShowIgnored {
			objs = append(objs, sections(ui, hidden))
		}
	}

	ui.PendingBox.Objects = objs
	ui.PendingBox.Refresh()
	shell.Relayout(ui.LogBody)
	DrawLogLegend(ui)
}

// pendingSection is one issue's worth of tiles. Every commit still gets its own
// tile — several commits on one issue are related work, not one job, and the
// minutes are logged per commit — but they belong side by side under the name
// they are all filed against instead of scattered through the list.
//
// A tile that is the only one on its issue has nothing to be grouped with, so
// heading it would be a line of chrome per tile. Those fall into the last
// section, which carries no heading.
type pendingSection struct {
	Issue  string // "" for the catch-all at the foot
	Groups []backend.Group
}

// PendingSections buckets tiles by issue, keeping the order they arrived in —
// newest first — both for the sections and for the tiles inside each one.
func PendingSections(groups []backend.Group) []pendingSection {
	var order []string
	byIssue := map[string][]backend.Group{}
	for _, g := range groups {
		if g.Issue == "" {
			continue
		}
		if _, seen := byIssue[g.Issue]; !seen {
			order = append(order, g.Issue)
		}
		byIssue[g.Issue] = append(byIssue[g.Issue], g)
	}

	var out []pendingSection
	for _, issue := range order {
		if len(byIssue[issue]) > 1 {
			out = append(out, pendingSection{Issue: issue, Groups: byIssue[issue]})
		}
	}
	var loose []backend.Group
	for _, g := range groups {
		if len(byIssue[g.Issue]) < 2 { // includes the commits with no ref at all
			loose = append(loose, g)
		}
	}
	if len(loose) > 0 {
		out = append(out, pendingSection{Groups: loose})
	}
	return out
}

// sections lays the tiles out an issue at a time: its name, its tiles, then a
// rule before the next one.
func sections(ui *shell.UI, groups []backend.Group) fyne.CanvasObject {
	all := PendingSections(groups)
	var objs []fyne.CanvasObject
	for i, s := range all {
		if i > 0 {
			objs = append(objs, container.NewPadded(widget.NewSeparator()))
		}
		// The loose tiles are "other" only when something named stands above
		// them. With nothing grouped they are the whole list, and a heading
		// over the only section says nothing.
		if s.Issue != "" || len(all) > 1 {
			objs = append(objs, sectionHeading(ui, s))
		}
		objs = append(objs, bubbleGrid(ui, s.Groups, s.Issue != ""))
	}
	return container.NewVBox(objs...)
}

// sectionHeading names the issue its tiles are filed against, with the ref and
// the tally beside it. That name is what each tile would otherwise have printed
// for itself; saying it once above them is the point of the section. The last
// section is named for what it is: whatever had nothing to group with.
func sectionHeading(ui *shell.UI, s pendingSection) fyne.CanvasObject {
	commits := 0
	for _, g := range s.Groups {
		commits += len(g.Commits)
	}
	tally := fmt.Sprintf("%d commits", commits)
	if commits == 1 {
		tally = "1 commit"
	}

	title, side := "Other commits", tally
	if s.Issue != "" {
		info := ui.IssueInfo[s.Issue]
		if title = info.Title; title == "" {
			title = s.Issue
		}
		side = fmt.Sprintf("%s  ·  %s", ShortIssueLabel(info, s.Issue), tally)
	}
	heading := widget.NewLabelWithStyle(shell.Truncate(title, BubbleTitleChars),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	heading.Wrapping = fyne.TextWrapWord
	detail := widget.NewLabelWithStyle(side, fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	detail.Wrapping = fyne.TextWrapBreak
	return container.NewVBox(heading, detail)
}

// bubbleGrid is one block of tiles, wrapped into as many rows as the width
// needs. underHeading travels down to the tiles so one in a named section does
// not repeat the name printed above it.
func bubbleGrid(ui *shell.UI, groups []backend.Group, underHeading bool) fyne.CanvasObject {
	bubbles := make([]fyne.CanvasObject, 0, len(groups))
	for i := range groups {
		bubbles = append(bubbles, GroupBubble(ui, groups[i], underHeading))
	}
	return container.New(widgets.NewFlowGrid(BubbleMinWidth, BubbleMaxWidth, BubbleHeight), bubbles...)
}

// SetIgnored hides a commit from the pending list, or brings it back. The
// cached result is patched in place: a refetch costs seconds and would return
// exactly the same commits.
func SetIgnored(ui *shell.UI, g backend.Group, ignored bool) {
	touched := map[string]bool{}
	for _, c := range g.Commits {
		if err := ui.Store.SetIgnored(c.Sha, ignored); err != nil {
			ui.Errf(err)
			return
		}
		touched[c.Sha] = true
	}
	for i := range ui.Pending.Groups {
		cur := &ui.Pending.Groups[i]
		if len(cur.Commits) > 0 && touched[cur.Commits[0].Sha] {
			cur.Ignored = ignored
		}
	}
	live := 0
	for _, cur := range ui.Pending.Groups {
		if !cur.Ignored {
			live += len(cur.Commits)
		}
	}
	ui.Pending.Count = live
	RenderPending(ui)
}

// Pending tiles size themselves off the window: bubbleMinWidth only decides how
// many fit per row, and flowGrid then stretches them to use the full width, so
// there is no leftover strip on the right at an awkward window size.
// bubbleMaxWidth keeps a single pending tile from spanning a maximised screen.
const (
	BubbleMinWidth = 340
	BubbleMaxWidth = 560
	BubbleHeight   = 152
)

// BubbleTitleChars is what fits in two wrapped lines of the *narrowest* tile.
// Past that the title is elided, because fyne containers do not clip: a tile
// that overflows its height prints on top of the one below it. A wider tile
// simply runs out of text early — the full subject is in the popup.
const BubbleTitleChars = 72

// A pending tile gives part of its first line to the Ignore button, so its
// heading is capped shorter than a saved row's.
const pendingTitleChars = 60

// GroupBubble is the compact summary of one pending group — one commit, since
// FetchPending no longer merges them. The issue's title leads: that is the name
// the work is filed and pushed under, and it is the same name the saved row
// will carry. The commit's own line sits under it, where it still tells two
// tiles on the same issue and day apart.
//
// underHeading flips that around: in a section already headed by the issue, the
// title would be the same on every tile, so the commit leads instead — that is
// the only thing telling those tiles apart.
func GroupBubble(ui *shell.UI, g backend.Group, underHeading bool) fyne.CanvasObject {
	info := ui.IssueInfo[g.Issue]
	muted := theme.Color(theme.ColorNamePlaceHolder)
	caption := func(s string) *canvas.Text {
		t := canvas.NewText(shell.Truncate(s, 56), muted)
		t.TextSize = theme.CaptionTextSize()
		return t
	}

	// Heading: the issue this is filed against. The ref stands in while the
	// title is still loading, and the commit speaks for the tile only when
	// there is no issue at all.
	title := "No issue ref found"
	switch {
	case underHeading && len(g.Commits) == 1:
		title = backend.CommitSubject(g.Commits[0])
	case info.Title != "":
		title = info.Title
	case g.Issue != "":
		title = g.Issue
	case len(g.Commits) == 1:
		title = backend.CommitSubject(g.Commits[0])
	}
	titleLbl := widget.NewLabelWithStyle(
		shell.Truncate(title, pendingTitleChars), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	titleLbl.Wrapping = fyne.TextWrapWord

	// What the commit did, then where it came from. Stacked, not side by side:
	// on one row a long repo name and the date printed over each other.
	commitLine := ""
	if len(g.Commits) > 0 {
		commitLine = backend.CommitSubject(g.Commits[0])
	}
	foot := ShortIssueLabel(info, g.Issue)
	if len(g.Commits) == 1 {
		foot += "  ·  " + g.Commits[0].Sha
	} else {
		plural := "commits"
		if len(g.Commits) == 1 {
			plural = "commit"
		}
		foot += fmt.Sprintf("  ·  %d %s", len(g.Commits), plural)
	}
	var lines []fyne.CanvasObject
	if commitLine != "" && commitLine != title { // in a section the heading is already the commit
		lines = append(lines, caption(commitLine))
	}
	lines = append(lines, caption(foot+"  ·  "+g.Date))
	footer := container.NewVBox(lines...)

	// Ignore takes a commit out of the list without inventing minutes for it —
	// a revert, a formatting sweep, work already covered by another entry. It
	// is reversible, so it asks nothing before acting.
	action := "Ignore"
	if g.Ignored {
		action = "Restore"
	}
	skip := widget.NewButton(action, func() { SetIgnored(ui, g, !g.Ignored) })
	skip.Importance = widget.LowImportance
	// The title gets the full width. The secondary action has its own short
	// row so it cannot squeeze every line of a long title into a narrow column.
	actionRow := container.NewHBox(layout.NewSpacer(), skip)
	alignedFooter := container.New(layout.NewCustomPaddedLayout(0, 0, theme.InnerPadding(), theme.InnerPadding()), footer)
	body := container.NewBorder(nil, container.NewVBox(alignedFooter, actionRow), nil, nil, titleLbl)

	// A plain Card is near-invisible on the dark theme — dark text on a dark
	// surface against a dark window. Tint the surface towards the org's colour
	// and give it a matching stripe, so each tile reads as its own tappable
	// thing and says which organisation it belongs to without a word.
	accent := shell.OrgColor(ui.Cfg, shell.OrgOf(g.Issue))
	bg := canvas.NewRectangle(shell.BlendColor(
		theme.Color(theme.ColorNameInputBackground), accent, 0.16))
	bg.StrokeColor = shell.BlendColor(theme.Color(theme.ColorNameInputBorder), accent, 0.5)
	bg.StrokeWidth = 1
	bg.CornerRadius = 10

	stripe := canvas.NewRectangle(accent)
	stripe.SetMinSize(fyne.NewSize(4, 0))
	stripe.CornerRadius = 2

	// No inset here: flowGrid puts real space between cells, so the tile fills
	// the one it is given rather than shrinking inside it.
	inner := container.NewBorder(nil, nil, stripe, nil, container.NewPadded(body))
	tile := container.NewStack(bg, container.NewPadded(inner))
	return shell.NewCardTappable(tile, func() { OpenGroupEditor(ui, g) })
}

// ShortIssueLabel drops the org from a ref. Every repo in view belongs to the
// same org, so "sample-general-tasks #6511" says the same as the full label in
// half the tile width.
func ShortIssueLabel(info backend.IssueInfo, ref string) string {
	if info.Repo != "" {
		return fmt.Sprintf("%s #%d", info.Repo, info.Number)
	}
	if _, repo, num, err := backend.SplitIssue(ref); err == nil {
		return fmt.Sprintf("%s #%d", repo, num)
	}
	return ref
}

// OpenGroupEditor shows the full logging form for one group in a popup.
func OpenGroupEditor(ui *shell.UI, g backend.Group) {
	var d dialog.Dialog
	form := GroupEditor(ui, g,
		// The list behind updates as soon as the row is written…
		func(logged []backend.Commit) { DropLoggedCommits(ui, g, logged) },
		// …but the popup only closes once there is no result left to read, so
		// a push error or its URL is never hidden before it is seen.
		func() {
			if d != nil {
				d.Hide()
			}
		})
	heading := g.Date
	if info, ok := ui.IssueInfo[g.Issue]; ok && info.Title != "" {
		heading = g.Date + "  ·  " + shell.Truncate(info.Title, 100)
	} else if g.Issue != "" {
		heading = g.Date + "  ·  " + g.Issue
	}
	// No dismiss button: dialog.NewCustom parks it centred at the bottom, past
	// the end of a long form. The back arrow rides at the top left instead,
	// where it is reachable without scrolling.
	back := widget.NewButtonWithIcon("Back", theme.NavigateBackIcon(), func() {
		if d != nil {
			d.Hide()
		}
	})
	back.Importance = widget.LowImportance
	d = dialog.NewCustomWithoutButtons(heading, PopupShell(back, form), ui.Win)
	d.Resize(ui.PopupSize())
	d.Show()
}

// PopupShell puts Back and the form's actions on one line above the body, with
// the result line under them. Both stay put while the form scrolls, so a push
// result is readable from wherever the remarks were left.
//
// Below the bar the fields take the height they need and the remarks take the
// rest, down to the bottom edge. The remarks used to sit inside the one scroll
// that held the whole form, which nested the entry's own scroller inside
// another — the wheel was swallowed by whichever one the pointer happened to be
// over, and over the remarks that meant the form would not move at all. Now no
// scroller contains another. topFill caps the fields at a share of the popup so
// a group with a long commit list scrolls rather than eating the remarks.
func PopupShell(back fyne.CanvasObject, form editorForm) fyne.CanvasObject {
	fields := container.NewVScroll(form.Body)
	bar := container.NewBorder(nil, nil, back, form.actions)
	return container.NewBorder(
		container.NewVBox(bar, form.msg), nil, nil, nil,
		container.New(widgets.NewTopFill(0.45, form.Body), fields, form.remarks))
}

// DropLoggedCommits removes the commits just written from the cached result, so
// the list updates instantly instead of paying for another fetch. Only the
// ticked commits go, since the rest are still unlogged.
func DropLoggedCommits(ui *shell.UI, g backend.Group, logged []backend.Commit) {
	gone := map[string]bool{}
	for _, c := range logged {
		gone[c.Sha] = true
	}
	var groups []backend.Group
	for _, cur := range ui.Pending.Groups {
		if cur.Date == g.Date && cur.Issue == g.Issue {
			var kept []backend.Commit
			for _, c := range cur.Commits {
				if !gone[c.Sha] {
					kept = append(kept, c)
				}
			}
			if len(kept) == 0 {
				continue
			}
			cur.Commits = kept
		}
		groups = append(groups, cur)
	}
	ui.Pending.Groups = groups
	ui.Pending.Count = 0
	for _, cur := range groups {
		if cur.Ignored {
			continue // the count is work still to do, and this is not
		}
		ui.Pending.Count += len(cur.Commits)
	}
	RenderPending(ui)
	DrawRecent(ui)
}

// editorForm is what a popup editor hands back in pieces, so the popup can
// place them itself. The action bar and the result line used to sit at the foot
// of the form, where a full set of remarks pushed them under the fold and left
// Back holding a line of its own at the top. Now Back, the buttons and the mode
// dropdown share that one line, and neither the buttons nor the push result can
// scroll out of sight. The remarks come out separately because they are the one
// part sized to the popup rather than to its content.
type editorForm struct {
	Body    fyne.CanvasObject // the fields, sized to fit
	remarks fyne.CanvasObject // stretches to the foot of the popup
	actions fyne.CanvasObject
	msg     fyne.CanvasObject
}

// All is the whole form as one object, for callers that place it themselves
// rather than splitting it across a popup shell.
func (f editorForm) All() fyne.CanvasObject {
	return container.NewVBox(f.actions, f.msg, f.Body, f.remarks)
}

// remarksEditor builds the remarks box both editors share: a multi-line entry
// that fills the foot of the popup, a live count against remarkCap, and the AI
// compaction button. The count hangs off OnChanged rather than being poked by
// every caller, so text written from code — a rebuilt set of remarks, an AI
// result — updates it too. The middle return is the whole pane, caption and
// box, ready to be handed the bottom of the popup.
func remarksEditor(ui *shell.UI, text string, msg *widget.Label) (*widget.Entry, fyne.CanvasObject, *widget.Button) {
	remE := widget.NewMultiLineEntry()
	// A floor, not a height: the pane hands the box whatever room is left below
	// the divider, which on any normal window is far more than six rows. Asking
	// for sixteen here would only stop the divider being dragged up.
	remE.SetMinRowsVisible(6)
	// A multi-line entry clips by default, so a long bullet ran off to the right
	// and scrolling down dragged the view sideways with it. Wrap instead: every
	// line stays inside the box and the only scrolling left is vertical.
	remE.Wrapping = fyne.TextWrapWord
	counter := widget.NewLabel("")
	update := func() { counter.SetText(fmt.Sprintf("%d / %d", len(remE.Text), backend.RemarkCap)) }
	remE.OnChanged = func(string) { update() }
	remE.SetText(text)
	update() // empty text is not a change, so OnChanged never fired

	aiBtn := widget.NewButton("Compact with AI", func() {
		before := len(remE.Text)
		text := remE.Text
		msg.SetText("Compacting…")
		var out string
		ui.Async(func() error {
			o, err := backend.AiCompact(ui.Cfg, text)
			out = o
			return err
		}, func() {
			remE.SetText(out)
			msg.SetText(fmt.Sprintf("Compacted %d chars to %d. Check before pushing.", before, len(out)))
		})
	})
	// Border, not VBox: the caption keeps its own line and the box takes every
	// pixel under it, however tall the pane turns out to be.
	pane := container.NewBorder(
		container.NewHBox(widget.NewLabel("Worklog remarks"), counter),
		nil, nil, nil, remE)
	return remE, pane, aiBtn
}

// commitText is the text pane of a commit entry. Until the text outgrows
// remarkCap it is the one box it always was: what goes in it is both the
// sub-issue's description and the Worklog remarks field. Past remarkCap a
// "Separate worklog remarks" tick appears; ticking it keeps the first box as
// the description, free up to bodyCap, and opens a second box beside it for
// the remarks, with a count of its own against remarkCap. That way a long
// commit write-up lands on the issue in full instead of being cut or
// compacted to fit a project field.
type commitText struct {
	desc     *widget.Entry
	Remarks  *widget.Entry
	Separate *widget.Check
	pane     fyne.CanvasObject
	aiBtn    *widget.Button
}

// Split reports whether the remarks are their own text.
func (t *commitText) Split() bool { return t.Separate.Checked }

// Values returns the remarks and the issue body as the row stores them. The
// body is empty unless split, which keeps the two as one text on the row.
func (t *commitText) Values() (remarks, body string) {
	if !t.Split() {
		return strings.TrimSpace(t.desc.Text), ""
	}
	return strings.TrimSpace(t.Remarks.Text), strings.TrimSpace(t.desc.Text)
}

// Check returns why the text cannot be saved, or "" when it can.
func (t *commitText) Check() string {
	remarks, body := t.Values()
	if !t.Split() {
		if len(remarks) > backend.RemarkCap {
			return fmt.Sprintf("Remarks are %d chars — trim to %d, or tick Separate worklog remarks.", len(remarks), backend.RemarkCap)
		}
		return ""
	}
	switch {
	case remarks == "":
		return "Worklog remarks are empty — write them or untick Separate worklog remarks."
	case len(remarks) > backend.RemarkCap:
		return fmt.Sprintf("Worklog remarks are %d chars — trim to %d first.", len(remarks), backend.RemarkCap)
	case len(body) > backend.BodyCap:
		return fmt.Sprintf("Description is %d chars — trim to %d first.", len(body), backend.BodyCap)
	}
	return ""
}

func CommitTextEditor(ui *shell.UI, remarks, body string, msg *widget.Label) *commitText {
	t := &commitText{}
	descE := widget.NewMultiLineEntry()
	descE.SetMinRowsVisible(6)
	descE.Wrapping = fyne.TextWrapWord
	remE := widget.NewMultiLineEntry()
	remE.SetMinRowsVisible(6)
	remE.Wrapping = fyne.TextWrapWord
	t.desc, t.Remarks = descE, remE

	descCaption := widget.NewLabel("Worklog remarks")
	descCount := widget.NewLabel("")
	remCount := widget.NewLabel("")
	remPane := container.NewBorder(
		container.NewHBox(widget.NewLabel("Worklog remarks"), remCount),
		nil, nil, nil, remE)
	t.Separate = widget.NewCheck("Separate worklog remarks", nil)
	separateSlot := container.NewStack()
	descPane := container.NewBorder(
		container.NewHBox(descCaption, descCount, separateSlot),
		nil, nil, nil, descE)
	// Beside, not under: the pane is the foot of the popup, and halving its
	// width costs less than halving a height already shared with the fields.
	// The host swaps between the lone box and the pair rather than hiding a
	// grid column, which would still hold its half of the width.
	pair := container.NewGridWithColumns(2)
	host := container.NewStack(descPane)

	update := func() {
		if t.Split() {
			descCaption.SetText("Issue description")
			descCount.SetText(fmt.Sprintf("%d / %d", len(descE.Text), backend.BodyCap))
		} else {
			descCaption.SetText("Worklog remarks")
			descCount.SetText(fmt.Sprintf("%d / %d", len(descE.Text), backend.RemarkCap))
		}
		remCount.SetText(fmt.Sprintf("%d / %d", len(remE.Text), backend.RemarkCap))
		// Offered once the text reaches the cap, and kept on screen while
		// ticked so it can be unticked after the description is trimmed.
		shell.ShowIf(t.Separate, t.Split() || len(descE.Text) >= backend.RemarkCap)
		want := fyne.CanvasObject(descPane)
		if t.Split() {
			want = pair
		}
		if len(host.Objects) != 1 || host.Objects[0] != want {
			pair.Objects = nil
			if t.Split() {
				pair.Objects = []fyne.CanvasObject{descPane, remPane}
			}
			host.Objects = []fyne.CanvasObject{want}
			host.Refresh()
		}
	}
	t.Separate.OnChanged = func(on bool) {
		// The remarks box starts from the description cut to fit, a bullet
		// at a time, so there is something to edit rather than a blank box.
		if on && strings.TrimSpace(remE.Text) == "" {
			remE.SetText(backend.CompactLocally(strings.Split(strings.TrimSpace(descE.Text), "\n")))
		}
		update()
	}
	separateSlot.Add(shell.GoldChoice(t.Separate))
	descE.OnChanged = func(string) { update() }
	remE.OnChanged = func(string) { update() }
	if strings.TrimSpace(body) != "" {
		descE.SetText(body)
		remE.SetText(remarks)
		t.Separate.SetChecked(true)
	} else {
		descE.SetText(remarks)
	}
	update()

	// Split, the AI writes the remarks from the description and leaves the
	// description alone; otherwise it compacts the one box as before.
	t.aiBtn = widget.NewButton("Compact with AI", func() {
		src := descE.Text
		dst := descE
		if t.Split() {
			dst = remE
		}
		before := len(src)
		msg.SetText("Compacting…")
		var out string
		ui.Async(func() error {
			o, err := backend.AiCompact(ui.Cfg, src)
			out = o
			return err
		}, func() {
			dst.SetText(out)
			msg.SetText(fmt.Sprintf("Compacted %d chars to %d. Check before pushing.", before, len(out)))
		})
	})

	t.pane = host
	return t
}

// resolveForPush fills in the issue a row goes to, for the kinds whose issue is
// only decided when it is pushed: a meeting's day issue, and the parent an
// independent entry needs created.
//
// The row is patched on disk before the push runs, so a push that then fails
// does not create the same issue twice on the retry — the second attempt sees
// the ref already there and goes straight to filing against it.
//
// Runs off the UI thread, so it must not touch widgets.
func resolveForPush(ui *shell.UI, r backend.Row) (backend.Row, error) {
	patch, err := backend.EnsureIssueRef(ui.Cfg, r)
	if err != nil {
		return r, err
	}
	if patch == nil {
		patch = backend.Row{}
	}
	// A blank owner on the row makes setFields skip the project's Worklog Owner
	// field, so pushed entries turn up unassigned on the board. Legacy rows
	// saved before the manual pane stamped an owner still fall through here;
	// the Settings value is the correct default, and it is what the row editor
	// would have written in.
	if strings.TrimSpace(r["owner"]) == "" {
		if owner := strings.TrimSpace(ui.Cfg.WorklogOwner); owner != "" {
			patch["owner"] = owner
		}
	}
	if len(patch) == 0 {
		return r, nil
	}
	if err := ui.Store.UpdateRow(r["id"], patch); err != nil {
		return r, err
	}
	out := backend.Row{}
	for k, v := range r {
		out[k] = v
	}
	for k, v := range patch {
		out[k] = v
	}
	return out, nil
}

// applyPushResult records what a push achieved on the row and returns a summary
// worth showing. Only a push with nothing outstanding sets pushed_at — a
// partial one has to stay retryable — so the caller is told which it was.
func applyPushResult(ui *shell.UI, id string, res backend.PushResult) (string, bool) {
	patch := backend.Row{"issue_url": res.URL, "item_id": res.ItemID}
	if res.Complete() {
		patch["pushed_at"] = time.Now().Format("2006-01-02T15:04:05")
	}
	if err := ui.Store.UpdateRow(id, patch); err != nil {
		ui.Errf(err)
	}
	summary := "Pushed: " + res.URL
	if len(res.FieldsSet) > 0 {
		summary += "\n\nSet: " + strings.Join(res.FieldsSet, ", ")
	}
	if len(res.Notes) > 0 {
		summary += "\n\n" + strings.Join(res.Notes, "\n")
	}
	if !res.Complete() {
		summary += "\n\nNot finished:\n" + strings.Join(res.Problems, "\n") +
			"\n\nThe row keeps its push button so you can retry."
	}
	return summary, res.Complete()
}

// openRowEditor opens a saved row in the same popup shell as a pending group,
// so an entry can be corrected before it is pushed — or after, if the wrong
// minutes went up.
func openRowEditor(ui *shell.UI, r backend.Row, refresh func()) {
	var d dialog.Dialog
	form := RowEditor(ui, r, refresh, func() {
		if d != nil {
			d.Hide()
		}
	})
	heading := r["date"]
	if r["type"] == backend.KindIndependent && strings.TrimSpace(r["parent_title"]) != "" {
		heading = "Standalone task  ·  " + strings.TrimSpace(r["parent_title"])
	} else if r["issue"] != "" {
		heading += "  ·  " + r["issue"]
	}
	back := widget.NewButtonWithIcon("Back", theme.NavigateBackIcon(), func() {
		if d != nil {
			d.Hide()
		}
	})
	back.Importance = widget.LowImportance
	d = dialog.NewCustomWithoutButtons(heading, PopupShell(back, form), ui.Win)
	d.Resize(ui.PopupSize())
	d.Show()
}

// RowEditor is the group editor minus the commit picker: which commits the row
// covers is settled the moment it exists, so what is left to change is the
// worklog itself. Saving rewrites the row in place — no second entry is made,
// and the commits stay accounted for.
func RowEditor(ui *shell.UI, r backend.Row, refresh func(), onFinished func()) editorForm {
	isIndependent := r["type"] == backend.KindIndependent
	createsOwnIssue := backend.RowCreatesOwnIssue(r)
	dateE := NewWorklogDatePicker(ui, r["date"])
	ownE := widget.NewEntry()
	ownE.SetText(backend.OrDefault(r["owner"], ui.Cfg.WorklogOwner))
	minE := widget.NewEntry()
	minE.SetText(r["minutes"])
	minE.SetPlaceHolder("480")
	issE := widget.NewEntry()
	issE.SetText(r["issue"])
	issE.SetPlaceHolder("owner/repo#123")
	parentRepoE := widget.NewEntry()
	parentRepoE.SetText(r["parent_repo"])
	parentRepoE.SetPlaceHolder("exampleorg/repository")
	parentTitleE := widget.NewEntry()
	parentTitleE.SetText(r["parent_title"])
	parentTitleE.SetPlaceHolder("Title for the standalone task")
	descE := widget.NewEntry()
	descE.SetText(r["description"])

	msg := widget.NewLabel("")
	// A manual entry carries its text in description and has no remarks yet;
	// seeding from it beats handing over an empty box that pushes as nothing.
	// A commit entry gets the pane that can split the issue description off
	// from the remarks; the other kinds keep the single box.
	var text *commitText
	var remE *widget.Entry
	var remPane fyne.CanvasObject
	var aiBtn *widget.Button
	if r["type"] == "commit" {
		text = CommitTextEditor(ui, r["remarks"], r["issue_body"], msg)
		remPane, aiBtn = text.pane, text.aiBtn
	} else {
		remE, remPane, aiBtn = remarksEditor(ui, backend.OrDefault(r["remarks"], r["description"]), msg)
	}

	modeSel := widget.NewSelect([]string{"Worklog sub-issue", "The issue itself"}, nil)
	if r["mode"] == "issue" {
		modeSel.SetSelected("The issue itself")
	} else {
		modeSel.SetSelected("Worklog sub-issue")
	}
	modeVal := func() string {
		if createsOwnIssue {
			if isIndependent {
				return "subissue"
			}
			return "issue"
		}
		if modeSel.Selected == "The issue itself" {
			return "issue"
		}
		return "subissue"
	}

	save := func(push bool) {
		date := dateE.ISO()
		if date == "" {
			msg.SetText("Pick a worklog date.")
			return
		}
		var remarks, issueBody string
		if text != nil {
			if why := text.Check(); why != "" {
				msg.SetText(why)
				return
			}
			remarks, issueBody = text.Values()
		} else {
			remarks = strings.TrimSpace(remE.Text)
			if len(remarks) > backend.RemarkCap {
				msg.SetText(fmt.Sprintf("Remarks are %d chars — trim to %d first.", len(remarks), backend.RemarkCap))
				return
			}
		}
		mins, _ := strconv.Atoi(strings.TrimSpace(minE.Text))
		if push && mins <= 0 {
			msg.SetText("Enter the minutes before pushing.")
			return
		}
		patch := backend.Row{
			"date": date, "minutes": strconv.Itoa(mins),
			"description": strings.TrimSpace(descE.Text),
			"issue":       strings.TrimSpace(issE.Text),
			"owner":       strings.TrimSpace(ownE.Text),
			"remarks":     remarks, "mode": modeVal(),
		}
		if text != nil {
			patch["issue_body"] = issueBody
		}
		if isIndependent {
			patch["parent_repo"] = strings.TrimSpace(parentRepoE.Text)
			patch["parent_title"] = strings.TrimSpace(parentTitleE.Text)
		}
		if err := ui.Store.UpdateRow(r["id"], patch); err != nil {
			ui.Errf(err)
			return
		}
		// Keep the copy this popup holds in step with the file, so a push that
		// follows a save uses the edited values and not the ones it opened with.
		for k, v := range patch {
			r[k] = v
		}
		refresh()
		if !push {
			msg.SetText("Saved.")
			if onFinished != nil {
				onFinished()
			}
			ui.ShowDraftSavedInfo()
			return
		}
		if r["issue"] == "" && !createsOwnIssue {
			msg.SetText("Saved — no issue ref to push to.")
			return
		}
		body := remarks
		if body == "" {
			body = r["description"]
		}
		if body == "" {
			msg.SetText("Saved — nothing to push: remarks and description are both empty.")
			return
		}
		msg.SetText("Pushing…")
		pushProgress, stop := ui.PushSpinner("Pushing to GitHub…")
		var res backend.PushResult
		var perr error
		ui.Async(func() error {
			resolved, err := resolveForPush(ui, r)
			if err != nil {
				perr = err
				return nil
			}
			r = resolved
			res, perr = backend.PushEntry(ui.Cfg, r["issue"], r["date"], r["owner"],
				mins, body, r["issue_body"], backend.OrDefault(r["mode"], "issue"), r["issue_url"],
				backend.SubTitleBaseFor(r), pushProgress)
			return nil // handle the push error inline so the edit is not lost
		}, func() {
			stop()
			if perr != nil {
				msg.SetText("Saved, but push failed: " + perr.Error() + " Retry from this popup or the entry's push button.")
				refresh()
				return
			}
			summary, complete := applyPushResult(ui, r["id"], res)
			refresh()
			ui.RefreshAfterPush(r["date"])
			msg.SetText(strings.ReplaceAll(summary, "\n\n", "  |  "))
			if complete && onFinished != nil {
				onFinished()
			}
		})
	}

	saveBtn := widget.NewButton("Save", func() { save(false) })
	pushBtn := widget.NewButton("Save & push", func() { save(true) })
	pushBtn.Importance = widget.HighImportance

	header := container.NewHBox(
		widget.NewLabelWithStyle(r["date"], fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	if createsOwnIssue {
		label := "Standalone issue"
		switch r["type"] {
		case backend.KindMeeting:
			label = "Meeting issue"
		case backend.KindBulkReview:
			label = "Bulk review issue"
		case backend.KindIndependent:
			label = "Standalone task"
		}
		header.Add(widget.NewLabel(label))
	} else {
		header.Add(shell.IssueHyperlink(r["issue"]))
	}
	// Each sha carries the repo it is in, so it links to the commit itself. Rows
	// written before that was stored have the sha alone; for those the issue's
	// repo is the only guess available, which is what the link used to be built
	// from for every row — and why it so often 404'd. Without either, plain text.
	fallback := ""
	if owner, name, _, err := backend.SplitIssue(r["issue"]); err == nil {
		fallback = owner + "/" + name
	}
	if !createsOwnIssue {
		for _, ref := range backend.ParseCommitRefs(r["refs"]) {
			header.Add(shell.CommitHyperlink(backend.Commit{
				Repo: backend.OrDefault(ref.Repo, fallback), Sha: ref.Sha}))
		}
	}

	var notes []fyne.CanvasObject
	if r["pushed_at"] != "" {
		notes = append(notes, shell.ColorLabel(
			"Already pushed on "+r["pushed_at"]+". Saving changes this copy only — "+
				"Save & push writes the edits over the same worklog item on GitHub.",
			theme.ColorNameWarning))
	}

	var fieldRow fyne.CanvasObject
	if isIndependent {
		fieldRow = container.NewVBox(
			container.New(widgets.NewRatioRow(0.34, 0.33, 0.33),
				labelledPicker("Worklog date", dateE),
				shell.Labeled("Worklog owner", ownE),
				shell.Labeled("Worklog mins", minE)),
			container.New(widgets.NewRatioRow(0.38, 0.62),
				shell.Labeled("Repository", parentRepoE),
				shell.Labeled("Standalone issue title", parentTitleE)))
	} else if createsOwnIssue {
		fieldRow = container.New(widgets.NewRatioRow(0.34, 0.33, 0.33),
			labelledPicker("Worklog date", dateE),
			shell.Labeled("Worklog owner", ownE),
			shell.Labeled("Worklog mins", minE))
	} else {
		// Weighted, not quartered: an owner/repo#1234 ref needs the room a
		// minutes box does not, and the split follows the window.
		fieldRow = container.New(widgets.NewRatioRow(0.22, 0.22, 0.14, 0.42),
			labelledPicker("Worklog date", dateE),
			shell.Labeled("Worklog owner", ownE),
			shell.Labeled("Worklog mins", minE),
			shell.Labeled("Issue", issE))
	}
	fields := container.NewVBox(fieldRow, dayFillReadout(ui, dateE, minE, r["id"]))
	// Caption beside the dropdown rather than stacked over it: the bar shares a
	// line with Back, and a two-row label would drag that whole line taller.
	actionItems := []fyne.CanvasObject{aiBtn, saveBtn, shell.OvalPush(pushBtn)}
	if !createsOwnIssue {
		actionItems = append([]fyne.CanvasObject{
			widget.NewLabel("Push as"), shell.WideSelect(modeSel)}, actionItems...)
	}
	actions := container.NewHBox(actionItems...)
	return editorForm{
		Body: container.NewVBox(append(notes,
			header,
			fields,
			shell.Labeled("Description (kept locally, not pushed)", descE))...),
		remarks: remPane,
		actions: actions,
		msg:     msg,
	}
}

// GroupEditor is the logging form for one group. onLogged fires with the
// commits written to the store; onFinished fires when nothing is left on screen
// worth reading and the popup can close.
func GroupEditor(ui *shell.UI, g backend.Group, onLogged func([]backend.Commit), onFinished func()) editorForm {
	// commit checkboxes
	checks := make([]*widget.Check, len(g.Commits))
	var commitRows []fyne.CanvasObject
	for i, c := range g.Commits {
		chk := widget.NewCheck("", nil)
		chk.SetChecked(true)
		checks[i] = chk
		row := container.NewBorder(nil, nil,
			container.NewHBox(shell.GoldChoice(chk), shell.CommitHyperlink(c)),
			widget.NewLabel(c.Repo),
			widget.NewLabel(shell.Truncate(c.Message, 70)))
		commitRows = append(commitRows, row)
	}

	dateE := NewWorklogDatePicker(ui, g.Date)
	ownE := widget.NewEntry()
	ownE.SetText(ui.Cfg.WorklogOwner)
	minE := widget.NewEntry()
	minE.SetPlaceHolder("480")
	issE := widget.NewEntry()
	issE.SetText(g.Issue)
	issE.SetPlaceHolder("owner/repo#123")

	msg := widget.NewLabel("")
	text := CommitTextEditor(ui, g.Remarks, "", msg)
	remE, remPane, aiBtn := text.desc, text.pane, text.aiBtn

	modeSel := widget.NewSelect([]string{"Worklog sub-issue", "The issue itself"}, nil)
	if ui.Cfg.DefaultMode == "issue" {
		modeSel.SetSelected("The issue itself")
	} else {
		modeSel.SetSelected("Worklog sub-issue")
	}

	pickedCommits := func() []backend.Commit {
		var picked []backend.Commit
		for i, chk := range checks {
			if chk.Checked {
				picked = append(picked, g.Commits[i])
			}
		}
		return picked
	}
	modeVal := func() string {
		if modeSel.Selected == "The issue itself" {
			return "issue"
		}
		return "subissue"
	}

	doSave := func(push bool) {
		picked := pickedCommits()
		if len(picked) == 0 {
			msg.SetText("Tick at least one commit.")
			return
		}
		if strings.TrimSpace(remE.Text) == "" {
			remE.SetText(backend.BuildRemarks(picked))
		}
		if strings.TrimSpace(remE.Text) == "" {
			msg.SetText("Could not build remarks from the selected commit messages.")
			return
		}
		if why := text.Check(); why != "" {
			msg.SetText(why)
			return
		}
		remarks, issueBody := text.Values()
		date := dateE.ISO()
		if date == "" {
			msg.SetText("Pick a worklog date.")
			return
		}
		// Each sha is written with the repo it came from. The issue's repo is not
		// that repo — the issue is in a tracker, the commit is in the applet that
		// changed — so a sha stored bare could only ever be linked by guessing.
		shas := make([]string, 0, len(picked))
		mins, _ := strconv.Atoi(strings.TrimSpace(minE.Text))
		for _, c := range picked {
			shas = append(shas, backend.FormatCommitRef(c.Repo, c.Sha))
		}
		// The row's label is the issue it belongs to. It used to be every picked
		// commit's subject joined with semicolons, which read as noise in the
		// list and repeated what the remarks already say properly.
		desc := shell.Truncate(RowLabel(ui, strings.TrimSpace(issE.Text), picked), 200)
		made, err := ui.Store.AppendRows([]backend.Row{{
			"date": date, "minutes": strconv.Itoa(mins), "type": "commit",
			"description": desc, "refs": strings.Join(shas, ","),
			"issue": strings.TrimSpace(issE.Text), "owner": ownE.Text,
			"remarks": remarks, "issue_body": issueBody, "mode": modeVal(),
		}})
		if err != nil {
			ui.Errf(err)
			return
		}
		row := made[0]
		if onLogged != nil {
			onLogged(picked)
		}
		if !push {
			msg.SetText("Saved locally.")
			DrawRecent(ui)
			if onFinished != nil {
				onFinished()
			}
			ui.ShowDraftSavedInfo()
			return
		}
		if row["issue"] == "" {
			// Nothing to push and nothing to read, so treat it as done.
			msg.SetText("Saved locally — no issue ref to push to.")
			DrawRecent(ui)
			if onFinished != nil {
				onFinished()
			}
			return
		}
		msg.SetText("Pushing…")
		pushProgress, stop := ui.PushSpinner("Pushing to GitHub…")
		var res backend.PushResult
		var perr error
		ui.Async(func() error {
			res, perr = backend.PushEntry(ui.Cfg, row["issue"], row["date"], row["owner"], mins,
				row["remarks"], row["issue_body"], row["mode"], row["issue_url"],
				backend.SubTitleBaseFor(row), pushProgress)
			return nil // handle push error inline so the saved row is not lost
		}, func() {
			stop()
			if perr != nil {
				msg.SetText("Saved locally, but push failed: " + perr.Error() + " Use the push button on the entry to retry.")
				DrawRecent(ui)
				return
			}
			// Only a clean push marks the row done; otherwise it stays
			// retryable from Recent entries.
			patch := backend.Row{"issue_url": res.URL, "item_id": res.ItemID}
			if res.Complete() {
				patch["pushed_at"] = time.Now().Format("2006-01-02T15:04:05")
			}
			_ = ui.Store.UpdateRow(row["id"], patch)
			ui.RefreshAfterPush(row["date"])
			note := "Pushed: " + res.URL
			if len(res.FieldsSet) > 0 {
				note += "  |  Set: " + strings.Join(res.FieldsSet, ", ")
			}
			if len(res.Notes) > 0 {
				note += "  |  " + strings.Join(res.Notes, " · ")
			}
			if !res.Complete() {
				note += "  |  NOT FINISHED: " + strings.Join(res.Problems, " · ") +
					"  |  Retry from Recent entries."
			}
			msg.SetText(note)
			DrawRecent(ui)
			if res.Complete() && onFinished != nil {
				onFinished()
			}
		})
	}

	saveBtn := widget.NewButton("Save only", func() { doSave(false) })
	pushBtn := widget.NewButton("Save & push", func() { doSave(true) })
	pushBtn.Importance = widget.HighImportance

	header := container.NewHBox(
		widget.NewLabelWithStyle(g.Date, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		shell.IssueHyperlink(g.Issue),
	)
	if info, ok := ui.IssueInfo[g.Issue]; ok && info.Title != "" {
		header.Add(widget.NewLabel("·"))
		header.Add(widget.NewLabel(shell.Truncate(info.Title, 100)))
	}
	// Weighted, not quartered: an owner/repo#1234 ref needs the room a minutes
	// box does not, and the split holds as the popup follows the window.
	fields := container.NewVBox(
		container.New(widgets.NewRatioRow(0.22, 0.22, 0.14, 0.42),
			labelledPicker("Worklog date", dateE),
			shell.Labeled("Worklog owner", ownE),
			shell.Labeled("Worklog mins", minE),
			shell.Labeled("Issue", issE),
		),
		dayFillReadout(ui, dateE, minE, ""),
	)
	actions := container.NewHBox(
		widget.NewLabel("Push as"), shell.WideSelect(modeSel), aiBtn, saveBtn, shell.OvalPush(pushBtn),
	)
	return editorForm{
		Body: container.NewVBox(
			header,
			container.NewVBox(commitRows...),
			fields,
		),
		remarks: remPane,
		actions: actions,
		msg:     msg,
	}
}
