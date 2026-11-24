package logwork

import (
	_ "embed"
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
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
	pendingSize = shell.NewMorph(ui.PendingBox)
	pendingRenderedAt = ui.PendingAt
	search := shell.NewSearchEntry("Search issues, titles or commits", ui.LogSearch, func(value string) {
		ui.LogSearch = value
		if ui.PendingLoaded {
			RenderPending(ui)
		}
		DrawRecent(ui)
	})
	commitPane := container.NewVBox(
		container.NewBorder(nil, nil, nil, shell.LeadGap(fetchBtn), search), pendingSize)

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
	mDate.followToday = true
	// Meetings have an editable title; their notes use the text editor below.
	mDesc := widget.NewEntry()
	mDesc.SetPlaceHolder("Meeting title (e.g. Sprint planning)")
	mRecording := widget.NewEntry()
	mRecording.SetPlaceHolder("https://… (meeting recording)")
	mMin := widget.NewEntry()
	mMin.SetPlaceHolder("minutes")
	mMsg := widget.NewLabel("")
	// The popup's remarks box, with its live count and, from remarkCap up, its
	// Separate worklog remarks tick. Here the boxes grow with what is typed, a
	// row at a time, instead of scrolling inside a box of four.
	mText := CommitTextEditor("", "")
	mText.Summarize = aiRemarks(ui)
	mText.desc.SetPlaceHolder("- one bullet per thing done; this becomes the worklog remarks")
	mText.growWithText(4, func() { shell.Relayout(ui.LogBody) })
	recordingRow := meetingRecordingField(mRecording)
	recordingRow.Hide()

	mModeVal := func() string { return "subissue" }

	// "Worklog": an issue that already exists.
	mIssue := widget.NewEntry()
	mIssue.SetPlaceHolder(IssuePlaceholder)
	otherRow := shell.Labeled("Issue link", mIssue)
	otherRow.Hide()
	// The calendar edges in gold the days already holding an entry for the
	// same issue, or the day's bulk review.
	mDate.Matches = func() (string, string) {
		switch kind {
		case backend.KindBulkReview:
			return rowPlace(backend.Row{"type": kind}), ""
		case backend.KindOther:
			ref, _ := issueFieldRef(mIssue.Text)
			return rowPlace(backend.Row{"issue": ref}), ""
		}
		return "", ""
	}

	// A single code review starts from the thing actually in hand: its PR, in
	// any spelling. The linked issue is resolved from GitHub's metadata or the
	// PR's Refs text, with a dropdown beside the link when the PR names several;
	// a direct issue link is accepted too.
	review := newReviewTarget(ui)
	singleReviewRow := review.View
	singleReviewRow.Hide()

	// "Bulk Review" is a sheet with a row per PR: its link, minutes and
	// worklog remarks. Its items feed the issue body and Worklog remarks.
	bulk := newBulkSheet(ui, func() { shell.Relayout(ui.LogBody) }, func(m string) { mMsg.SetText(m) })
	bulk.OnTotal = func(total int) {
		if total == 0 {
			mMin.SetText("")
		} else {
			mMin.SetText(strconv.Itoa(total))
		}
	}
	codeReviewRow := bulk.View
	codeReviewRow.Hide()

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

	// The Worklog's own title, for the kinds that file a Worklog sub-issue.
	mWorklogTitle := newWorklogTitleField(ui, "",
		func() string { return kind },
		func() string {
			switch kind {
			case backend.KindIndependent:
				return strings.TrimSpace(mTitle.Text)
			case backend.KindCodeReview:
				if ref, ok := review.chosen(); ok {
					return review.titles[ref]
				}
			case backend.KindOther:
				if ref, err := issueFieldRef(mIssue.Text); err == nil {
					return ui.IssueInfo[ref].Title
				}
			}
			return ""
		},
		func() string { return textForTitle(mText) },
		func(m string) { mMsg.SetText(m) })
	worklogTitleRow := mWorklogTitle.View
	worklogTitleRow.Hide()

	// Description sits on its own row so a textarea has room to breathe. Hidden
	// for the two kinds that title themselves ("Worklog: <date>") since the
	// field would only ever repeat the date they already picked.
	descRow := shell.Labeled("Meeting title", mDesc)
	descRow.Hide()

	// autoDescFor is the generated title non-meeting entries wear. Code Review
	// names its daily issue; Worklog and Independent keep the normal worklog title.
	autoDescFor := func(k, iso string) string {
		if k == backend.KindBulkReview {
			return backend.BulkReviewName(ui.Cfg, iso)
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
			ui.Cfg.BulkCodeReviewRepo, backend.BulkReviewName(ui.Cfg, iso))
	}

	// Both buttons share this closure. push=false saves a draft (like the popup
	// editor's "Save"); push=true saves and immediately pushes to GitHub via
	// resolveForPush + PushEntry, the same path the row editor uses. Reset the
	// form only after a successful save so a failure keeps the fields for a
	// second attempt.
	mSaveResolved := func(push bool, reviewIssue, issueTitle string) {
		var reviewItems []backend.CodeReviewItem
		mins, _ := strconv.Atoi(strings.TrimSpace(mMin.Text))
		if kind == backend.KindBulkReview {
			var err error
			reviewItems, err = bulk.Items()
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
		var remarksText, issueBody string
		if kind == backend.KindBulkReview {
			// Written from the sheet: the board gets the PR list, the issue
			// the list with each PR's remarks under it.
			remarksText = backend.CodeReviewList(reviewItems)
			issueBody = backend.CodeReviewBody(reviewItems)
			if len(remarksText) > backend.RemarkCap {
				mMsg.SetText(fmt.Sprintf("Remarks are %d chars — trim to %d first.", len(remarksText), backend.RemarkCap))
				return
			}
		} else {
			if why := mText.Check(); why != "" {
				mMsg.SetText(why)
				return
			}
			remarksText, issueBody = mText.Values()
		}
		row := backend.Row{
			"date": date, "minutes": strconv.Itoa(mins),
			"type": kind, "description": desc,
			"remarks": remarksText, "issue_body": issueBody,
			// Every push reads Row["owner"] into the project's Worklog Owner
			// field; a row saved with an empty owner leaves that column blank.
			// The Settings value is the same one the row editor seeds its own
			// Owner input from.
			"owner": strings.TrimSpace(ui.Cfg.WorklogOwner),
		}
		if kind == backend.KindOther || kind == backend.KindCodeReview || kind == backend.KindIndependent {
			row["worklog_title"] = mWorklogTitle.Text()
		}
		switch kind {
		case backend.KindMeeting:
			row["meeting_title"] = backend.OrDefault(desc, "Meeting: "+date)
			row["description"] = row["meeting_title"]
			row["meeting_recording"] = strings.TrimSpace(mRecording.Text)
			if _, err := backend.MeetingEntryBody(row); err != nil {
				mMsg.SetText(err.Error())
				return
			}
			row["mode"] = "issue"
		case backend.KindOther:
			ref, err := backend.IssueRefFromAny(mIssue.Text)
			if err != nil {
				mMsg.SetText(err.Error())
				return
			}
			row["issue"] = ref
			row["issue_title"] = strings.TrimSpace(issueTitle)
			row["mode"] = mModeVal()
		case backend.KindCodeReview:
			if strings.TrimSpace(reviewIssue) == "" {
				mMsg.SetText("Paste a PR or issue link for this review.")
				return
			}
			row["issue"] = reviewIssue
			row["issue_title"] = strings.TrimSpace(issueTitle)
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
			mRecording.SetText("")
			mMin.SetText("")
			mText.clear()
			mTitle.SetText("")
			mWorklogTitle.entry.SetText("")
			mIssue.SetText("")
			review.Reset()
			bulk.Reset()
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
		pushSavedEntry(ui, saved, func(updated backend.Row, res backend.PushResult, plans []backend.PullRequestPlan, perr error) {
			saved = updated
			if perr == errReviewPushCancelled {
				mMsg.SetText("Saved as draft — code reviews were not combined.")
				resetForm()
				return
			}
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
			offerPlans(ui, newPullRequests(plans))
		})
	}

	// Single reviews resolve their PR to its referenced issue before the normal
	// save path runs. Save draft does the same lookup so the draft itself is
	// already attached to the correct issue and can be pushed later unchanged.
	// The preview usually has the answer already, so a paste-then-save does
	// not ask GitHub twice; a PR naming several issues waits for the pick.
	//
	// A Worklog looks up its issue's title the same way, so its bubble says what
	// the issue is from the moment it is saved. The title is best-effort: a
	// failed lookup still saves, and the ref stands in until the next start-up
	// fills the title in. The version stops a second press, or a changed link,
	// from saving twice while the first lookup is out.
	titleLookup := 0
	saveWorklog := func(push bool) {
		source := mIssue.Text
		ref, err := backend.IssueRefFromAny(source)
		if err != nil {
			mMsg.SetText(err.Error())
			return
		}
		if title := strings.TrimSpace(ui.IssueInfo[ref].Title); title != "" {
			mSaveResolved(push, "", title)
			return
		}
		titleLookup++
		version := titleLookup
		mMsg.SetText("Finding the issue's title on GitHub…")
		var info backend.IssueInfo
		ui.Async(func() error {
			infos, _ := backend.FetchIssueInfos([]string{ref})
			info = infos[ref]
			return nil
		}, func() {
			if version != titleLookup || kind != backend.KindOther || mIssue.Text != source {
				return
			}
			if strings.TrimSpace(info.Title) != "" {
				if ui.IssueInfo == nil {
					ui.IssueInfo = map[string]backend.IssueInfo{}
				}
				ui.IssueInfo[ref] = info
			}
			mSaveResolved(push, "", info.Title)
		})
	}
	mSave := func(push bool) {
		if kind == backend.KindOther {
			saveWorklog(push)
			return
		}
		if kind != backend.KindCodeReview {
			mSaveResolved(push, "", "")
			return
		}
		review.Resolve(func(ref, title string) {
			if kind == backend.KindCodeReview {
				mSaveResolved(push, ref, title)
			}
		}, mMsg.SetText)
	}

	saveBtn := widget.NewButton("Save draft", func() { mSave(false) })
	pushBtn := widget.NewButton("Save & push", func() { mSave(true) })
	mPushView := shell.GitHubButton(pushBtn)
	// New worklogs always create sub-issues, so only the actions are needed.
	actionRow := container.NewHBox(layout.NewSpacer(), saveBtn, mPushView)

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
	remarksRow := mText.pane
	manualPane := container.NewVBox(
		dateRow,
		dayReadout,
		descRow,
		otherRow, singleReviewRow, codeReviewRow, indepRow,
		worklogTitleRow,
		remarksRow,
		recordingRow,
		actionRow, mMsg,
	)
	manualPane.Hide()

	// -- kind selector --
	// Switching kind slides the new form in from the side the choice moved to
	// and eases the panel to its new height, rather than jumping.
	kinds := []string{"Commits", "Worklog", "Code Review", "Bulk Review", "Meeting", "Independent", shell.WeekendSlateTab}
	// The weekend support slate files a week of issues rather than a worklog, so
	// it stands apart at the right end of the tabs with its own form.
	slate := ui.NewWeekendSlate()
	slatePane := slate.InlineView()
	slatePane.Hide()
	kindForm := shell.NewMorph(container.NewVBox(commitPane, manualPane, slatePane))
	var formReflow func(bool)
	shownKind := 0
	seg := shell.NewUnderlineTabs(kinds, func(s string) {
		if formReflow != nil {
			kindForm.Snapshot() // the form as it was, to slide out of the way
		}
		wasBulkReview := kind == backend.KindBulkReview
		kind = map[string]string{
			"Commits": backend.KindCommit, "Meeting": backend.KindMeeting,
			"Code Review": backend.KindCodeReview,
			"Bulk Review": backend.KindBulkReview,
			"Worklog":     backend.KindOther, "Independent": backend.KindIndependent,
		}[s]
		shell.ShowIf(slatePane, s == shell.WeekendSlateTab)
		if s == shell.WeekendSlateTab {
			commitPane.Hide()
			manualPane.Hide()
			// The days arrive once the form has slid in, as the commits do.
			if formReflow != nil {
				slate.Arrive(shell.MorphTime)
			}
		} else if kind == backend.KindCommit {
			commitPane.Show()
			manualPane.Hide()
			// Coming back to the commits brings their bubbles in again, as
			// opening the tab does.
			// The bubbles stay covered while the form slides in, then arrive.
			if formReflow != nil && ui.PendingLoaded {
				ui.ReplayArrival("pending")
				ui.ReplayArrival("ignored")
				pendingArriveDelay = shell.MorphTime
				RenderPending(ui)
			}
		} else {
			commitPane.Hide()
			manualPane.Show()
		}
		// Only the fields each kind needs, so the form never asks for an
		// issue link and a new issue's title at the same time.
		shell.ShowIf(descRow, kind == backend.KindMeeting)
		shell.ShowIf(recordingRow, kind == backend.KindMeeting)
		// The ratio layout hands the hidden Minutes share back to Date.
		shell.ShowIf(minutesField, kind != backend.KindBulkReview)
		shell.ShowIf(otherRow, kind == backend.KindOther)
		shell.ShowIf(singleReviewRow, kind == backend.KindCodeReview)
		shell.ShowIf(codeReviewRow, kind == backend.KindBulkReview)
		shell.ShowIf(indepRow, kind == backend.KindIndependent)
		shell.ShowIf(worklogTitleRow, kind == backend.KindOther || kind == backend.KindCodeReview || kind == backend.KindIndependent)
		mWorklogTitle.setKind(kind)
		shell.ShowIf(remarksRow, kind != backend.KindBulkReview)
		if kind == backend.KindBulkReview {
			mMin.Disable()
			bulk.updateTotal()
		} else {
			mMin.Enable()
			if wasBulkReview {
				mMin.SetText("")
			}
		}
		switch kind {
		case backend.KindMeeting:
			mMsg.SetText("Creates a meeting issue with your title, notes and recording link.")
		case backend.KindBulkReview:
			mMsg.SetText(bulkReviewFilingHint(backend.OrDefault(mDate.ISO(), shell.Today())))
		case backend.KindCodeReview, backend.KindOther, backend.KindIndependent:
			mMsg.SetText("Sub-issue title: " + autoDescFor(kind, backend.OrDefault(mDate.ISO(), shell.Today())) + ".")
		default:
			mMsg.SetText("")
		}
		if formReflow != nil {
			picked := slices.Index(kinds, s)
			kindForm.Change(picked-shownKind, formReflow)
			shownKind = picked
		}
	})
	seg.Trailing = 1
	// Blue like the Report's support bonus while not picked; gold once picked.
	seg.Inks = map[string]color.Color{shell.WeekendSlateTab: shell.SupportAccent()}
	// One kind is always picked: tapping the picked one again keeps it.
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
			mMsg.SetText("Creates a meeting issue with your title, notes and recording link.")
		case backend.KindBulkReview:
			mMsg.SetText(bulkReviewFilingHint(backend.OrDefault(iso, shell.Today())))
		case backend.KindCodeReview, backend.KindOther, backend.KindIndependent:
			mMsg.SetText("Sub-issue title: " + autoDescFor(kind, backend.OrDefault(iso, shell.Today())) + ".")
		}
	}

	showPushWhenDue(mPushView, mDate, false)
	mDate.watchDay()

	ui.RecentBox = container.NewVBox()
	// The week sits under the saved entries: its columns grow as tall as the
	// cards standing in them, so putting it first pushed the list itself off the
	// screen on a busy week.
	ui.WeekBox = container.NewVBox()
	// The list eases to its new height when the tick adds or drops the pushed
	// entries, as folding Saved locally does.
	recentMorph := shell.NewMorph(ui.RecentBox)
	var savedReflow func(bool)
	ui.ShowPushed = widget.NewCheck("Also show entries already pushed this month", func(bool) {
		recentMorph.Snapshot()
		DrawRecent(ui)
		recentMorph.Change(0, savedReflow)
	})
	if ui.DraftSort == "" {
		ui.DraftSort = DraftSortDuration
	}
	draftSortPick := shell.NewGoldSelect([]string{DraftSortDuration, DraftSortCreated}, nil)
	draftSortPick.SetSelected(ui.DraftSort)
	draftSortPick.OnChanged = func(selected string) {
		ui.DraftSort = selected
		DrawRecent(ui)
	}
	draftSortControl := container.NewBorder(nil, nil, shell.GoldTickChoice(ui.ShowPushed),
		container.NewHBox(widget.NewLabel("Sort bubbles"),
			container.New(layout.NewGridWrapLayout(fyne.NewSize(190, draftSortPick.MinSize().Height)), draftSortPick)))
	// Tabs, not radio buttons: the picked kind is bright with a gold line under
	// it, and the line slides to the next one picked. A row too wide for the
	// panel scrolls sideways rather than widening the page.
	// A little room under the line before the form starts.
	choices := container.New(layout.NewCustomPaddedLayout(0, theme.InnerPadding(), 0, 0), container.NewHScroll(seg))
	// The entry form is its own menu, so it sits in a rounded panel like
	// Settings; the saved list under it is part of the page.
	logCard := shell.Panel(container.NewVBox(choices, kindForm))
	// The key shares the week's controls row, to the right of This week.
	ui.LogLegend = container.NewVBox()
	// Tapping the title folds away everything but the week: the list controls
	// and the list of entries saved for other weeks. Folded to start with.
	// Lines out here take the panels' colour; lines inside a panel stay dark.
	savedList := container.NewVBox(draftSortControl, recentMorph, shell.PanelDivider())
	gap := theme.InnerPadding()
	var logScroll *container.Scroll
	var page *fyne.Container
	var recentCard *fyne.Container
	var pinWeekdays func()
	// Every frame of a slide re-lays the page from the scroll inwards, so the
	// view follows the page as it grows or shrinks.
	reflow := func(inner fyne.CanvasObject) func(bool) {
		return func(final bool) {
			shell.Reflow(final, logScroll, ui.LogBody, page, inner)
			if pinWeekdays != nil {
				pinWeekdays()
			}
		}
	}
	formReflow = reflow(logCard)
	pendingReflow = formReflow
	savedReflow = func(final bool) { reflow(recentCard)(final) }
	savedHead, savedArea := shell.CollapsibleTitle("Saved locally", savedList, false, nil, savedReflow)
	// Room above and below the title, so it reads as a heading between the
	// form's panel and its divider rather than squeezed between them.
	savedHeadRoom := container.New(layout.NewCustomPaddedLayout(gap, gap, 0, 0), savedHead)
	// No side margin of its own: the week lines up with the form panel above.
	weekInset := container.New(layout.NewCustomPaddedLayout(gap, 0, 0, 0), ui.WeekBox)
	recentCard = container.NewVBox(savedHeadRoom, shell.PanelDivider(), savedArea, weekInset)

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
	page = container.NewVBox(ui.LogProgress.View, logCard, recentCard)
	ui.LogBody = shell.PageInset(page)
	logScroll = shell.NewPageScroll(ui.LogBody, page, ui.LogBody)
	pinWeekdays = func() {
		top := page.Position().Y + recentCard.Position().Y + weekInset.Position().Y + ui.WeekBox.Position().Y
		pinWeekdayHeadings(ui, logScroll.Offset.Y-top)
	}
	ui.LogBody.Layout = &calendarPageLayout{body: ui.LogBody.Layout, pin: pinWeekdays}
	logScroll.OnScrolled = func(fyne.Position) { pinWeekdays() }
	// No shade at the scroll's top edge: it fell across the pinned weekday
	// headings once the page scrolled.
	return container.NewStack(shell.NoScrollShadow(logScroll), ui.DragLayer)
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

// pendingArriveDelay holds the next pending arrival back, while the Commits
// form slides in.
var pendingArriveDelay time.Duration

// The ignored tiles sit in a fold under their toggle, so showing or hiding them
// grows or shrinks the Commits card over a moment instead of all at once.
var (
	pendingSize       *shell.Morph
	pendingRenderedAt time.Time
	ignoredFold       *shell.Fold
	// ignoredHidden is what the fold holds: the ignored groups the last draw
	// found.
	ignoredHidden []backend.Group
	// pendingReflow re-lays the page around the card on each frame of the
	// slide; nil until the tab is built.
	pendingReflow func(bool)
)

// ignoredArrival brings the ignored tiles in one after another, as the list
// does, unless a newer list is being fetched behind this one.
func ignoredArrival(ui *shell.UI, delay time.Duration) *shell.Arrival {
	if ui.PendingLoading {
		return nil
	}
	return ui.Arrival("ignored", fmt.Sprint(ui.LogSearch, "|", ui.Pending.Range)).After(delay)
}

// toggleIgnored shows or hides the ignored tiles, sliding the card to fit.
//
// The fold goes in and comes out of the list as it stands: drawing the whole
// list again at either end of the slide was a stutter, there to put back
// exactly what was already showing.
func toggleIgnored(ui *shell.UI) {
	ui.ShowIgnored = !ui.ShowIgnored
	if ui.ShowIgnored {
		ui.ReplayArrival("ignored")
	}
	if pendingReflow == nil || ui.PendingBox == nil || len(ignoredHidden) == 0 {
		RenderPending(ui)
		return
	}
	if t := ui.IgnoredToggle; t != nil {
		from, to := "Hide ", "Show "
		if ui.ShowIgnored {
			from, to = to, from
		}
		t.Text = strings.Replace(t.Text, from, to, 1)
		t.Refresh()
	}
	fold := ignoredFold
	if ui.ShowIgnored {
		// Clicked again while still shutting: it turns round where it is.
		if fold == nil {
			fold = shell.NewFold(sections(ui, ignoredHidden, ignoredArrival(ui, 0)), false)
			ignoredFold = fold
			ui.PendingBox.Add(fold)
		}
		fold.Slide(true, pendingReflow)
		return
	}
	if fold == nil {
		RenderPending(ui)
		return
	}
	fold.Slide(false, func(final bool) {
		pendingReflow(final)
		if final && ignoredFold == fold {
			ignoredFold = nil
			ui.PendingBox.Remove(fold)
			shell.Repaint(ui.PendingBox)
		}
	})
}

func RenderPending(ui *shell.UI) {
	// Log List counts these commits in its pending column, so it is redrawn
	// whenever the list behind it changes — otherwise that column keeps saying
	// "reading your commits" after they have arrived.
	defer func() {
		if ui.TabVisible(shell.LogListTabName) {
			shell.Hooks.DrawLogList(ui)
		}
	}()

	res := ui.Pending
	arriveDelay := pendingArriveDelay // for the ignored tiles too; the list clears it
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
		// The list arrives as Kanban's cards do, one bubble after another, when
		// it first loads, when the search or filter changes it, and when the
		// tab is opened. Logging or ignoring a commit leaves the rest be.
		// Held back while a newer list is being fetched behind this one.
		var arrive *shell.Arrival
		if !ui.PendingLoading {
			arrive = ui.Arrival("pending", fmt.Sprint(ui.LogSearch, "|", res.Range)).After(pendingArriveDelay)
		}
		pendingArriveDelay = 0
		objs = append(objs, sections(ui, live, arrive))
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
		// A gold tag that fades lit under the pointer. The same one is kept
		// from draw to draw, so clicking it (which redraws the list) leaves it
		// lit rather than handing the pointer a fresh, unlit one.
		if ui.IgnoredToggle == nil {
			ui.IgnoredToggle = shell.NewGoldTag(label, func() { toggleIgnored(ui) })
			ui.IgnoredToggle.Wide = true
			ui.IgnoredToggle.Large = true
		}
		if ui.IgnoredToggle.Text != label {
			ui.IgnoredToggle.Text = label
			ui.IgnoredToggle.Refresh()
		}
		// Room above, so the toggle stands clear of the tiles over it.
		objs = append(objs, container.New(layout.NewCustomPaddedLayout(theme.InnerPadding(), 0, 0, 0),
			container.NewHBox(ui.IgnoredToggle)))
		if ui.ShowIgnored {
			ignoredFold = shell.NewFold(sections(ui, hidden, ignoredArrival(ui, arriveDelay)), true)
			objs = append(objs, ignoredFold)
		}
	}
	if !ui.ShowIgnored || len(hidden) == 0 {
		ignoredFold = nil
	}
	ignoredHidden = hidden

	ui.PendingBox.Objects = objs
	// A completed scan changes the list's height. Ease the card to fit before
	// refreshing it, preserving the height of the loading message or old list.
	if !ui.PendingLoading && !ui.PendingAt.Equal(pendingRenderedAt) {
		pendingRenderedAt = ui.PendingAt
		if pendingSize != nil && pendingSize.Content == ui.PendingBox && pendingReflow != nil {
			pendingSize.Change(0, pendingReflow)
		}
	}
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
//
// arrive, if set, brings the tiles in one after another across the sections.
func sections(ui *shell.UI, groups []backend.Group, arrive *shell.Arrival) fyne.CanvasObject {
	all := PendingSections(groups)
	delays := shell.ArriveDelays(len(groups))
	next := 0
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
		objs = append(objs, bubbleGrid(ui, s.Groups, s.Issue != "", arrive, delays[next:next+len(s.Groups)]))
		next += len(s.Groups)
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
	// The name is gold, "Other commits" included, as a panel's heading is.
	gold := container.NewThemeOverride(heading, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	return container.NewVBox(gold, detail)
}

// bubbleGrid is one block of tiles, wrapped into as many rows as the width
// needs. underHeading travels down to the tiles so one in a named section does
// not repeat the name printed above it.
func bubbleGrid(ui *shell.UI, groups []backend.Group, underHeading bool, arrive *shell.Arrival, delays []time.Duration) fyne.CanvasObject {
	bubbles := make([]fyne.CanvasObject, 0, len(groups))
	for i := range groups {
		bubbles = append(bubbles, GroupBubble(ui, groups[i], underHeading))
	}
	bubbles = arrive.Reveal(bubbles, delays, nil)
	return container.New(widgets.NewFlowGrid(BubbleMinWidth, BubbleMaxWidth, BubbleHeight).
		Compact(BubbleCompactBelow, BubbleCompactMinWidth), bubbles...)
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
		// A chore commit is ignored on its own; bringing one back has to be
		// remembered, or the next fetch would ignore it again.
		if backend.IsChoreCommit(c) {
			if err := ui.Store.SetKept(c.Sha, !ignored); err != nil {
				ui.Errf(err)
				return
			}
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
	// Below BubbleCompactBelow of page width the tiles get narrower, and only as
	// tall as their text needs, so more of them fit on a small screen.
	BubbleCompactBelow    = 1500
	BubbleCompactMinWidth = 280
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
	caption := func(s string) fyne.CanvasObject { return fitCaption(shell.Truncate(s, 56), muted) }

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
		title = ShortIssueLabel(info, g.Issue)
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
	skip := shell.NewHoverButton(action, nil, func() { SetIgnored(ui, g, !g.Ignored) })
	skip.Importance = widget.LowImportance
	// The title gets the full width. The secondary action has its own short
	// row so it cannot squeeze every line of a long title into a narrow column.
	// Its hover fades in and out as the saved cards' Delete does.
	actionRow := container.NewHBox(layout.NewSpacer(), shell.FadeHover(skip))
	for _, c := range g.Commits {
		if c.Local {
			// Only on this machine: its reference goes on before it is pushed.
			actionRow = container.NewHBox(container.NewCenter(shell.PendingPushTag()), layout.NewSpacer(), shell.FadeHover(skip))
			break
		}
	}
	alignedFooter := container.New(layout.NewCustomPaddedLayout(0, 0, theme.InnerPadding(), theme.InnerPadding()), footer)
	body := container.NewBorder(nil, container.NewVBox(alignedFooter, actionRow), nil, nil, titleLbl)

	// The card look every worklog bubble shares, the week strip's included: a
	// dark org tint at rest that lightens under the pointer, the org bar
	// sliding in down the left. It still says which organisation the tile
	// belongs to without a word. No inset here: flowGrid puts real space
	// between cells, so the tile fills the one it is given.
	accent := shell.OrgColor(ui.Cfg, shell.OrgOf(g.Issue))
	bubble := shell.NewStripeBubble(accent, body)
	// The tile presses as a button does, ignored or not: it shrinks under the
	// pointer and springs back as its editor opens.
	tile := shell.NewBubbleTappable(bubble, func() { OpenGroupEditor(ui, g) })
	tile.Press = true
	return tile
}

// ShortIssueLabel drops the org from a ref. Every repo in view belongs to the
// same org, so "sample-general-tasks #6511" says the same as the full label in
// half the tile width.
func ShortIssueLabel(info backend.IssueInfo, ref string) string {
	if info.Repo != "" {
		return fmt.Sprintf("%s#%d", info.Repo, info.Number)
	}
	if _, repo, num, err := backend.SplitIssue(ref); err == nil {
		return fmt.Sprintf("%s#%d", repo, num)
	}
	return ref
}

// OpenGroupEditor shows the full logging form for one group in a popup.
func OpenGroupEditor(ui *shell.UI, g backend.Group) {
	var closeEditor func()
	form := GroupEditor(ui, g,
		// The list behind updates as soon as the row is written…
		func(logged []backend.Commit) { DropLoggedCommits(ui, g, logged) },
		// …but the popup only closes once there is no result left to read, so
		// a push error or its URL is never hidden before it is seen.
		func() {
			if closeEditor != nil {
				closeEditor()
			}
		})
	heading := g.Date
	if info, ok := ui.IssueInfo[g.Issue]; ok && info.Title != "" {
		heading = g.Date + "  ·  " + shell.Truncate(info.Title, 100)
	} else if g.Issue != "" {
		heading = g.Date + "  ·  " + g.Issue
	}
	closeEditor = showWorklogEditor(ui, heading, form)
}

func showWorklogEditor(ui *shell.UI, heading string, form editorForm) func() {
	stopWatching := form.date.watchDay()
	var popup *widget.PopUp
	closeEditor := func() {
		if popup != nil {
			shell.ClosePopUp(popup)
			popup = nil
			stopWatching()
		}
	}
	back := shell.FadeQuietButton(widget.NewButtonWithIcon("Back", theme.NavigateBackIcon(), closeEditor))
	popup = ui.ShowEditorPopup(heading, back, form.actions, PopupShell(form))
	return closeEditor
}

// PopupShell lays out the form beneath the popup header. The status line stays
// above the scrolling fields and the remarks use the remaining height.
func PopupShell(form editorForm) fyne.CanvasObject {
	fields := container.NewVScroll(form.Body)
	view := container.NewBorder(
		form.msg.label, nil, nil, nil,
		container.New(widgets.NewTopFill(0.45, form.Body), fields, form.remarks))
	form.msg.view = view
	return view
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

// editorForm hands the popup its title bar actions, status line, fields and
// remarks pane so each can take the room it needs.
type editorForm struct {
	date    *WorklogDatePicker
	Body    fyne.CanvasObject // the fields, sized to fit
	remarks fyne.CanvasObject // stretches to the foot of the popup
	actions fyne.CanvasObject
	msg     *editorStatus
}

// All is the whole form as one object, for callers that place it themselves
// rather than splitting it across a popup shell.
func (f editorForm) All() fyne.CanvasObject {
	view := container.NewVBox(f.actions, f.msg.label, f.Body, f.remarks)
	f.msg.view = view
	return view
}

// editorStatus takes up space only when the form has feedback to show.
type editorStatus struct {
	label *widget.Label
	view  *fyne.Container
}

func newEditorStatus() *editorStatus {
	label := widget.NewLabel("")
	label.Hide()
	return &editorStatus{label: label}
}

func (s *editorStatus) SetText(text string) {
	s.label.SetText(text)
	visible := text != ""
	if s.label.Visible() != visible {
		shell.ShowIf(s.label, visible)
		if s.view != nil {
			s.view.Refresh()
		}
	}
}

// commitText is the text pane of a commit entry. Until the text outgrows
// remarkCap it is the one box it always was: what goes in it is both the
// sub-issue's description and the Worklog remarks field. Past remarkCap a
// "Separate worklog remarks" tick appears; ticking it keeps the first box as
// the description, free up to bodyCap, and opens a second box beside it for
// the remarks, with a count of its own against remarkCap.
type commitText struct {
	desc     *widget.Entry
	Remarks  *widget.Entry
	Separate *widget.Check
	pane     fyne.CanvasObject
	// Summarize, when set, writes the separate remarks from a description
	// over the cap instead of copying it; done gets "" when it could not.
	Summarize func(text string, done func(string))
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

// growWithText sizes the boxes to their text, for a form that scrolls as a
// whole rather than handing them a fixed pane. Ticking the split halves the
// width, so the rows are counted again once the pair has been laid out.
func (t *commitText) growWithText(minRows int, relayout func()) {
	var grows []func()
	for _, e := range []*widget.Entry{t.desc, t.Remarks} {
		e.SetMinRowsVisible(minRows)
		grow := shell.AutoGrow(e, minRows, 0, relayout)
		grows = append(grows, grow)
		changed := e.OnChanged
		e.OnChanged = func(text string) {
			changed(text)
			grow()
		}
	}
	toggled := t.Separate.OnChanged
	t.Separate.OnChanged = func(on bool) {
		toggled(on)
		relayout()
		for _, grow := range grows {
			grow()
		}
	}
}

// clear empties both boxes and goes back to the one box.
func (t *commitText) clear() {
	t.Separate.SetChecked(false)
	t.desc.SetText("")
	t.Remarks.SetText("")
}

func CommitTextEditor(remarks, body string) *commitText {
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
	descHead := container.NewHBox(descCaption, descCount, separateSlot)
	descPane := container.NewBorder(descHead, nil, nil, nil, descE)
	host := newCommitTextPane(descPane, remPane)
	initializing := true

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
		// A hidden tick takes no width, so the line has to make room for it.
		offer := t.Split() || len(descE.Text) >= backend.RemarkCap
		if offer != t.Separate.Visible() {
			shell.ShowIf(t.Separate, offer)
			descHead.Refresh()
		}
		host.setSplit(t.Split(), !initializing)
	}
	t.Separate.OnChanged = func(on bool) {
		// Copy the original text into a new remarks box so the user can edit
		// it, or, with an AI set, have it cut down to fit.
		if on && strings.TrimSpace(remE.Text) == "" {
			if t.Summarize != nil && len(descE.Text) > backend.RemarkCap {
				t.Summarize(descE.Text, func(short string) {
					if strings.TrimSpace(remE.Text) == "" {
						remE.SetText(backend.OrDefault(short, descE.Text))
					}
					update()
				})
			} else {
				remE.SetText(descE.Text)
			}
		}
		update()
	}
	separateSlot.Add(shell.GoldTickChoice(t.Separate))
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
	initializing = false

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
	var closeEditor func()
	form := RowEditor(ui, r, refresh, func() {
		if closeEditor != nil {
			closeEditor()
		}
	})
	// The popup is named after the issue; its date, issue and commits are
	// on the line under the heading.
	heading := RowTitle(ui, r)
	if r["type"] == backend.KindIndependent && strings.TrimSpace(r["parent_title"]) != "" {
		heading = strings.TrimSpace(r["parent_title"])
	}
	if strings.TrimSpace(heading) == "" {
		heading = r["date"]
	}
	closeEditor = showWorklogEditor(ui, heading, form)
}

// createdDate is the day an entry was saved here, from its logged_at stamp.
func createdDate(loggedAt string) string {
	at := strings.TrimSpace(loggedAt)
	if len(at) >= 10 && backend.ValidPlannerDate(at[:10]) {
		return at[:10]
	}
	return ""
}

// commitLine names the commit's date beside the entry's created date.
func commitLine(date string) string {
	if date == "" {
		return ""
	}
	return "Committed " + date
}

// RowEditor is the group editor minus the commit picker: which commits the row
// covers is settled the moment it exists, so what is left to change is the
// worklog itself. Saving rewrites the row in place — no second entry is made,
// and the commits stay accounted for.
func RowEditor(ui *shell.UI, r backend.Row, refresh func(), onFinished func()) editorForm {
	isIndependent := r["type"] == backend.KindIndependent
	createsOwnIssue := backend.RowCreatesOwnIssue(r)
	refs := backend.RowCommitRefs(r)
	commitDate := savedCommitDate(ui, r["commit_date"], refs)
	dateE := NewWorklogDatePicker(ui, r["date"])
	ownE := widget.NewEntry()
	ownE.SetText(backend.OrDefault(r["owner"], ui.Cfg.WorklogOwner))
	minE := widget.NewEntry()
	minE.SetText(r["minutes"])
	minE.SetPlaceHolder("480")
	issE := widget.NewEntry()
	issE.SetText(r["issue"])
	issE.SetPlaceHolder(IssuePlaceholder)
	dateE.Matches = func() (string, string) {
		probe := backend.Row{}
		for k, v := range r {
			probe[k] = v
		}
		if ref, err := issueFieldRef(issE.Text); err == nil {
			probe["issue"] = ref
		}
		return rowPlace(probe), r["id"]
	}
	parentRepoE := widget.NewEntry()
	parentRepoE.SetText(r["parent_repo"])
	parentRepoE.SetPlaceHolder("exampleorg/repository")
	parentTitleE := widget.NewEntry()
	parentTitleE.SetText(r["parent_title"])
	parentTitleE.SetPlaceHolder("Title for the standalone task")
	descE := widget.NewEntry()
	initialTitle := RowTitle(ui, r)
	descE.SetText(initialTitle)
	recordingE := widget.NewEntry()
	recordingE.SetPlaceHolder("https://… (meeting recording)")
	recordingE.SetText(r["meeting_recording"])

	msg := newEditorStatus()
	// Older rows carry their work in description with no remarks yet. Seed
	// from that original text, independently of the issue title shown above.
	// Every kind can keep a full issue body separate from capped remarks.
	text := CommitTextEditor(backend.OrDefault(r["remarks"], r["description"]), r["issue_body"])
	// A bulk review is edited on the same sheet as the Bulk Review form: a
	// row per PR with its minutes and points, which also sets the minutes.
	var sheet *bulkSheet
	var sheetScroll *container.Scroll
	if backend.RowIsBulkCodeReview(r) {
		if items, err := backend.MergeCodeReviewItems(r["issue_body"], r["remarks"]); err == nil && len(items) > 0 {
			sheet = newBulkSheet(ui, func() {
				if sheetScroll != nil {
					sheetScroll.Refresh()
				}
			}, func(m string) { msg.SetText(m) })
			sheet.SetItems(items)
			sheet.OnTotal = func(total int) { minE.SetText(strconv.Itoa(total)) }
			minE.Disable()
		}
	}
	text.Summarize = aiRemarks(ui)
	// Meetings and bulk reviews are titled by their day; the rest take a
	// Worklog title of their own.
	hasWorklogTitle := r["type"] != backend.KindMeeting && !backend.RowIsBulkCodeReview(r)
	titleF := newWorklogTitleField(ui, r["worklog_title"],
		func() string { return r["type"] },
		func() string { return strings.TrimSpace(descE.Text) },
		func() string { return textForTitle(text) },
		func(m string) { msg.SetText(m) })

	modeVal := func() string {
		if createsOwnIssue && !isIndependent {
			return "issue"
		}
		// Re-pushing an old direct entry must retain its existing GitHub item.
		if r["pushed_at"] != "" && r["mode"] == "issue" {
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
		if sheet != nil {
			items, err := sheet.Items()
			if err != nil {
				msg.SetText(err.Error())
				return
			}
			// The board gets the PR list; the issue, each PR with its points.
			remarks, issueBody = backend.CodeReviewList(items), backend.CodeReviewBody(items)
			minE.SetText(strconv.Itoa(backend.CodeReviewMinutes(items)))
		} else {
			if why := text.Check(); why != "" {
				msg.SetText(why)
				return
			}
			remarks, issueBody = text.Values()
		}
		mins, _ := strconv.Atoi(strings.TrimSpace(minE.Text))
		if push && mins <= 0 {
			msg.SetText("Enter the minutes before pushing.")
			return
		}
		issue, err := issueFieldRef(issE.Text)
		if err != nil {
			msg.SetText(err.Error())
			return
		}
		issE.SetText(issue)
		patch := backend.Row{
			"date": date, "minutes": strconv.Itoa(mins),
			"description": strings.TrimSpace(descE.Text),
			"issue":       issue,
			"owner":       strings.TrimSpace(ownE.Text),
			"remarks":     remarks, "issue_body": issueBody, "mode": modeVal(),
		}
		if hasWorklogTitle {
			patch["worklog_title"] = titleF.Text()
		}
		if commitDate != "" {
			patch["commit_date"] = commitDate
		}
		if r["type"] == backend.KindMeeting {
			patch["display_title"] = ""
			patch["meeting_title"] = backend.OrDefault(strings.TrimSpace(descE.Text), "Meeting: "+date)
			patch["meeting_recording"] = strings.TrimSpace(recordingE.Text)
			if _, err := backend.MeetingEntryBody(patch); err != nil {
				msg.SetText(err.Error())
				return
			}
		}
		// A local title override must survive later GitHub title lookups. Old
		// descriptions remain untouched until the user edits this field.
		if r["type"] != backend.KindMeeting {
			if title := strings.TrimSpace(descE.Text); title != initialTitle {
				patch["display_title"] = title
			} else if issue != r["issue"] {
				patch["display_title"] = ""
			}
		}
		// A title saved for the old issue would go on naming this one.
		if issue != r["issue"] {
			patch["issue_title"] = strings.TrimSpace(ui.IssueInfo[issue].Title)
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
		initialTitle = strings.TrimSpace(descE.Text)
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
		pushSavedEntry(ui, r, func(updated backend.Row, res backend.PushResult, plans []backend.PullRequestPlan, perr error) {
			r = updated
			if perr == errReviewPushCancelled {
				msg.SetText("Saved — code reviews were not combined.")
				refresh()
				return
			}
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
			offerPlans(ui, newPullRequests(plans))
		})
	}

	saveBtn := widget.NewButton("Save", func() { save(false) })
	pushBtn := widget.NewButton("Save & push", func() { save(true) })

	// When the entry was saved here, then the commit's own date, then what it
	// is for.
	created := widget.NewLabel("")
	if at := createdDate(r["logged_at"]); at != "" {
		created.SetText("Created " + at)
	} else {
		created.Hide()
	}
	commitDateLabel := widget.NewLabelWithStyle(commitLine(commitDate), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	if commitDate == "" {
		commitDateLabel.Hide()
	}
	header := container.NewHBox(created, commitDateLabel)
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
	if !createsOwnIssue {
		for _, ref := range refs {
			header.Add(shell.CommitHyperlink(backend.Commit{Repo: ref.Repo, Sha: ref.Sha}))
		}
	}
	if commitDate == "" && len(refs) > 0 && len(ui.Cfg.RepoFolders) > 0 {
		folders := append([]string(nil), ui.Cfg.RepoFolders...)
		var date string
		ui.Async(func() error {
			date = localCommitDate(folders, refs)
			return nil
		}, func() {
			if date != "" {
				commitDate = date
				commitDateLabel.SetText(commitLine(date))
				commitDateLabel.Show()
				header.Refresh()
			}
		})
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
	descriptionField := shell.Labeled("Parent issue (kept locally, not pushed)", descE)
	if hasWorklogTitle {
		descriptionField = container.New(widgets.NewRatioRow(0.5, 0.5), descriptionField, titleF.View)
	}
	remPane := text.pane
	if sheet != nil {
		sheetScroll = container.NewVScroll(sheet.View)
		remPane = sheetScroll
	}
	if r["type"] == backend.KindMeeting {
		descE.SetPlaceHolder("Meeting title (e.g. Sprint planning)")
		descriptionField = shell.Labeled("Meeting title", descE)
		remPane = container.NewBorder(nil, meetingRecordingField(recordingE), nil, nil, remPane)
	}
	pushView := shell.GitHubButton(pushBtn)
	showPushWhenDue(pushView, dateE, rowSent(r))
	actions := container.NewHBox(saveBtn, pushView)
	return editorForm{
		date: dateE,
		Body: container.NewVBox(append(notes,
			header,
			fields,
			descriptionField)...),
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
	issE.SetPlaceHolder(IssuePlaceholder)
	dateE.Matches = func() (string, string) {
		ref, _ := issueFieldRef(issE.Text)
		return rowPlace(backend.Row{"issue": ref}), ""
	}

	msg := newEditorStatus()
	text := CommitTextEditor(g.Remarks, "")
	text.Summarize = aiRemarks(ui)
	remE := text.desc
	titleF := newWorklogTitleField(ui, "",
		func() string { return backend.KindCommit },
		func() string {
			if ref, err := issueFieldRef(issE.Text); err == nil {
				return ui.IssueInfo[ref].Title
			}
			return ""
		},
		func() string { return textForTitle(text) },
		func(m string) { msg.SetText(m) })

	pickedCommits := func() []backend.Commit {
		var picked []backend.Commit
		for i, chk := range checks {
			if chk.Checked {
				picked = append(picked, g.Commits[i])
			}
		}
		return picked
	}
	modeVal := func() string { return "subissue" }

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
		issue, err := issueFieldRef(issE.Text)
		if err != nil {
			msg.SetText(err.Error())
			return
		}
		issE.SetText(issue)
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
		desc := shell.Truncate(RowLabel(ui, issue, picked), 200)
		made, err := ui.Store.AppendRows([]backend.Row{{
			"date": date, "minutes": strconv.Itoa(mins), "type": "commit",
			"commit_date": backend.OrDefault(picked[0].Date, g.Date),
			"description": desc, "refs": strings.Join(shas, ","),
			"issue": issue, "owner": ownE.Text,
			"remarks": remarks, "issue_body": issueBody, "mode": modeVal(),
			"worklog_title": titleF.Text(),
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
			checkDraftsMerged(ui)
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
		// The Worklog and its commits' references share this one progress bar.
		var finished []backend.RewriteBatch
		ui.Async(func() error {
			row = ui.Store.CheckRowMerged(row)
			res, perr = backend.PushEntry(ui.Cfg, row["issue"], row["date"], row["owner"], mins,
				row["remarks"], backend.WorklogIssueBody(row, row["remarks"]), row["mode"], row["issue_url"],
				backend.SubTitleBaseFor(ui.Cfg, row), scaledProgress(pushProgress, pushShare(row)))
			if perr == nil {
				finished = refsAfterPush(ui, row, &res, pushProgress)
			}
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
			ui.RefreshLoggedShas()
			offerPullRequests(ui, finished)
		})
	}

	saveBtn := widget.NewButton("Save only", func() { doSave(false) })
	pushBtn := widget.NewButton("Save & push", func() { doSave(true) })

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
		titleF.View,
	)
	pushView := shell.GitHubButton(pushBtn)
	showPushWhenDue(pushView, dateE, false)
	actions := container.NewHBox(saveBtn, pushView)
	return editorForm{
		date: dateE,
		Body: container.NewVBox(
			header,
			container.NewVBox(commitRows...),
			fields,
		),
		remarks: text.pane,
		actions: actions,
		msg:     msg,
	}
}

// issueFieldRef reads an editor's Issue field in any spelling IssueRefFromAny
// takes, so a pasted link is stored as owner/repo#123 like every other row.
// An empty field stays empty: a row may be saved before its issue is known.
func issueFieldRef(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	return backend.IssueRefFromAny(text)
}
