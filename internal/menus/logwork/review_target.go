package logwork

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// reviewPickWidth is how wide the issue dropdown beside the link is. Wide
// enough for a repo, number and the start of a title; the full title shows
// under the field once one is picked.
const reviewPickWidth = 340

// reviewTarget is the single Code Review field: a PR or issue in any spelling,
// with the issue it will be filed under worked out as it is typed. When the PR
// names more than one issue a dropdown appears beside the link and nothing is
// filed until one is picked.
type reviewTarget struct {
	ui     *shell.UI
	Entry  *widget.Entry
	Pick   *widget.Select
	pickIn fyne.CanvasObject // the dropdown with its fixed width, hidden for one issue
	Status *widget.Label
	View   fyne.CanvasObject

	version int
	source  string   // the input refs belong to; refs are stale once it changes
	refs    []string // candidate issues, in the order the PR gives them
	titles  map[string]string
}

func newReviewTarget(ui *shell.UI) *reviewTarget {
	t := &reviewTarget{ui: ui, Entry: widget.NewEntry(), Status: widget.NewLabel("")}
	t.Entry.SetPlaceHolder(ReviewPlaceholder)
	t.Status.Wrapping = fyne.TextWrapWord
	t.Pick = widget.NewSelect(nil, func(string) { t.showPicked() })
	t.Pick.PlaceHolder = "Pick the issue"
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(reviewPickWidth, 0))
	t.pickIn = container.NewStack(floor, t.Pick)
	t.pickIn.Hide()
	t.View = container.NewVBox(
		shell.Labeled("PR or issue link", container.NewBorder(nil, nil, nil, t.pickIn, t.Entry)),
		t.Status)
	t.Entry.OnChanged = t.changed
	return t
}

// changed starts a lookup once typing settles. Without the wait, typing PR 123
// would ask GitHub about PR 1, then 12, then 123.
func (t *reviewTarget) changed(source string) {
	t.version++
	t.clearCandidates()
	t.Status.SetText("")
	if !reviewInputReadable(source) {
		return
	}
	version := t.version
	if ref, err := backend.IssueRefFromAny(source); err == nil && !isShorthand(source) {
		t.Status.SetText("Worklog issue: " + ref) // shown at once; the title follows
	} else {
		t.Status.SetText("Finding the referenced issue on GitHub…")
	}
	time.AfterFunc(350*time.Millisecond, func() {
		defer func() { _ = recover() }() // the app may close meanwhile
		fyne.Do(func() {
			if version == t.version && t.Entry.Text == source {
				t.lookup(source, version, nil)
			}
		})
	})
}

// lookup asks GitHub which issues the input points at and their titles, then
// shows them. then, when given, runs afterwards with the outcome, for a save
// waiting on it.
func (t *reviewTarget) lookup(source string, version int, then func(error)) {
	var lookup backend.CodeReviewLookup
	var infos map[string]backend.IssueInfo
	var err error
	t.ui.Async(func() error {
		lookup, err = backend.CodeReviewTargets(source)
		if err == nil {
			// Titles are best-effort: the ref is what files the time.
			infos, _ = backend.FetchIssueInfos(lookup.IssueRefs)
		}
		return nil
	}, func() {
		if version != t.version || t.Entry.Text != source {
			return
		}
		if err != nil {
			t.Status.SetText(err.Error())
		} else {
			t.setCandidates(source, lookup.IssueRefs, infos)
		}
		if then != nil {
			then(err)
		}
	})
}

func (t *reviewTarget) setCandidates(source string, refs []string, infos map[string]backend.IssueInfo) {
	t.source, t.refs, t.titles = source, refs, map[string]string{}
	if t.ui.IssueInfo == nil {
		t.ui.IssueInfo = map[string]backend.IssueInfo{}
	}
	labels := make([]string, len(refs))
	for i, ref := range refs {
		info := infos[ref]
		title := strings.TrimSpace(info.Title)
		if title != "" {
			t.titles[ref] = title
			t.ui.IssueInfo[ref] = info
		}
		labels[i] = reviewPickLabel(ref, title)
	}
	if len(refs) == 1 {
		t.Status.SetText(backend.CodeReviewTargetLabel(refs[0], t.titles[refs[0]]))
		return
	}
	t.Pick.Options = labels
	t.Pick.ClearSelected()
	t.pickIn.Show()
	t.Status.SetText(fmt.Sprintf("This PR references %d issues. Pick the one to log under from the list beside the link.", len(refs)))
	shell.Relayout(t.ui.LogBody)
}

func (t *reviewTarget) clearCandidates() {
	t.source, t.refs, t.titles = "", nil, nil
	t.Pick.Options = nil
	t.Pick.ClearSelected()
	if t.pickIn.Visible() {
		t.pickIn.Hide()
		shell.Relayout(t.ui.LogBody)
	}
}

func (t *reviewTarget) showPicked() {
	if ref, ok := t.chosen(); ok {
		t.Status.SetText(backend.CodeReviewTargetLabel(ref, t.titles[ref]))
	}
}

// chosen is the issue to file under, when there is one: the only candidate,
// or the one picked from several.
func (t *reviewTarget) chosen() (string, bool) {
	if t.source != t.Entry.Text {
		return "", false
	}
	switch {
	case len(t.refs) == 1:
		return t.refs[0], true
	case len(t.refs) > 1 && t.Pick.SelectedIndex() >= 0:
		return t.refs[t.Pick.SelectedIndex()], true
	}
	return "", false
}

// Resolve hands the issue to save under to done, looking it up first when the
// preview has not landed yet. say gets a line to show while it looks, or when
// there is nothing to save under yet: several issues waiting on a pick.
func (t *reviewTarget) Resolve(done func(ref, title string), say func(string)) {
	source := t.Entry.Text
	if strings.TrimSpace(source) == "" {
		say("Paste the PR link (or the issue link directly).")
		return
	}
	finish := func(err error) {
		if err != nil {
			say(err.Error())
			return
		}
		if ref, ok := t.chosen(); ok {
			done(ref, t.titles[ref])
			return
		}
		say(fmt.Sprintf("This PR references %d issues. Pick the one to log under from the list beside the link.", len(t.refs)))
	}
	if t.source == source && len(t.refs) > 0 {
		finish(nil)
		return
	}
	say("Finding the issue referenced by this PR…")
	t.version++
	t.lookup(source, t.version, finish)
}

func (t *reviewTarget) Reset() {
	t.Entry.SetText("")
	t.Status.SetText("")
}

// reviewInputReadable says whether the field holds something worth asking
// GitHub about: a PR or an issue in any spelling.
func reviewInputReadable(s string) bool {
	if _, ok := backend.CanonicalPullRequestURL(s); ok {
		return true
	}
	_, err := backend.IssueRefFromAny(s)
	return err == nil
}

// isShorthand reports owner/repo#123, which may name a PR or an issue and so
// cannot be previewed until GitHub says which.
func isShorthand(s string) bool {
	s = strings.TrimSpace(s)
	return strings.Contains(s, "#") && !strings.Contains(s, "/issues/") && !strings.Contains(s, "/pull")
}

// reviewPickLabel is one line in the issue dropdown: the repo and number
// first, so issues with similar titles still tell apart, then the title.
func reviewPickLabel(ref, title string) string {
	label := ref
	if _, repo, num, err := backend.SplitIssue(ref); err == nil {
		label = fmt.Sprintf("%s#%d", repo, num)
	}
	if title != "" {
		label += "  ·  " + shell.Truncate(title, 80)
	}
	return label
}

// What the GitHub fields say they take. Every one reads the shorthand, the
// path and the full link alike, and says so, so nobody has to guess.
const (
	IssuePlaceholder       = "owner/repo#123, owner/repo/issues/123 or its GitHub link"
	PullRequestPlaceholder = "owner/repo#123, owner/repo/pull/123 or its GitHub link"
	ReviewPlaceholder      = "PR or issue: owner/repo#123, owner/repo/pull/123 or its GitHub link"
)
