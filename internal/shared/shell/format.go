package shell

import (
	_ "embed"
	"fmt"
	"image/color"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
)

func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// UnfiledLabel names a row whose issue is only decided when it is pushed, so it
// is grouped as the thing it will become rather than as nothing.
func (ui *UI) UnfiledLabel(r backend.Row) string {
	switch r["type"] {
	case backend.KindBulkReview:
		return backend.DailyCodeReviewTitle(ui.Cfg, r["date"])
	case backend.KindCodeReview:
		if _, err := backend.ParseCodeReviewItems(r["remarks"]); err == nil {
			return backend.DailyCodeReviewTitle(ui.Cfg, r["date"])
		}
		return backend.CodeReviewTitle(r["date"])
	case backend.KindMeeting:
		return backend.MeetingTitle(ui.Cfg, r["date"])
	case backend.KindIndependent:
		if repo := strings.TrimSpace(r["parent_repo"]); repo != "" {
			return "new issue in " + repo
		}
	}
	return "(no issue ref)"
}

func Labeled(label string, obj fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(widget.NewLabelWithStyle(label, fyne.TextAlignLeading,
		fyne.TextStyle{}), obj)
}

// LabeledHint is labeled plus a small italic caption underneath the bold
// label — the label stays one line even in a narrow column, and the
// explanation rides below it in a lighter voice instead of wrapping the label.
func LabeledHint(label, hint string, obj fyne.CanvasObject) fyne.CanvasObject {
	head := widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	head.Truncation = fyne.TextTruncateEllipsis
	sub := widget.NewLabelWithStyle(hint, fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	sub.Wrapping = fyne.TextWrapWord
	return container.NewVBox(head, sub, obj)
}

// BlendColor mixes b into a by fraction f, returning an opaque colour. Used to
// tint a theme background towards the accent without hard-coding either.
func BlendColor(a, b color.Color, f float32) color.NRGBA {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	mix := func(x, y uint32) uint8 {
		v := float32(x>>8)*(1-f) + float32(y>>8)*f
		switch {
		case v < 0:
			v = 0
		case v > 255:
			v = 255
		}
		return uint8(v)
	}
	return color.NRGBA{R: mix(ar, br), G: mix(ag, bg), B: mix(ab, bb), A: 0xff}
}

func IssueTag(issue string) string {
	if issue == "" {
		return "no issue ref found"
	}
	return issue
}

// IssueLinkLabel names an issue or pull request link by its repo#number, or
// "Open issue" when raw is neither.
func IssueLinkLabel(raw string) string {
	return backend.OrDefault(ShortIssueRef(raw), "Open issue")
}

// ShortIssueRef is an issue or pull request link as repo#number, without the
// owner; "" when raw is neither.
func ShortIssueRef(raw string) string {
	ref, err := backend.IssueRefFromAny(raw)
	if err != nil {
		ref = backend.SprintItem{URL: raw}.RepositoryNumber(backend.SprintBoard{})
		if !strings.Contains(ref, "#") {
			return ""
		}
	}
	if i := strings.Index(ref, "/"); i >= 0 {
		ref = ref[i+1:]
	}
	return ref
}

func GithubHyperlink(label, rawURL string) fyne.CanvasObject {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return widget.NewLabel(label)
	}
	return widget.NewHyperlink(label, u)
}

// CommitHyperlink turns a commit's sha into a link to it on GitHub. The short
// sha resolves fine, so there is no need to carry the full one around.
func CommitHyperlink(c backend.Commit) fyne.CanvasObject {
	if c.Repo == "" || c.Sha == "" {
		return widget.NewLabel(c.Sha)
	}
	return GithubHyperlink(c.Sha, fmt.Sprintf("https://github.com/%s/commit/%s", c.Repo, c.Sha))
}

func IssueHyperlink(issue string) fyne.CanvasObject {
	return issueHyperlinkWithLabel(issue, IssueTag(issue))
}

func issueHyperlinkWithLabel(issue, label string) fyne.CanvasObject {
	if issue == "" {
		return widget.NewLabel(label)
	}
	owner, repo, number, err := backend.SplitIssue(issue)
	if err != nil {
		return widget.NewLabel(label)
	}
	return GithubHyperlink(label, fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, number))
}

// StateRank orders the load states by how much they need saying: a week that
// straddles two months reports the worse of the two.
func StateRank(s string) int {
	switch s {
	case "ok":
		return 0
	case "loading":
		return 1
	case "error":
		return 2
	default: // "off"
		return 3
	}
}

// PercentOf scores minutes against the daily target.
func PercentOf(mins int) int { return int(float64(mins) / float64(backend.Target) * 100) }

// DayFill is the colour behind a day. A finished day is green so the week can
// be read without counting: full green once GitHub holds the target, a paler
// green while the day only reaches it by counting what is still waiting.
func DayFill(mins, waiting int) color.Color {
	bg := theme.Color(theme.ColorNameBackground)
	switch {
	case mins >= backend.Target:
		return BlendColor(bg, theme.Color(theme.ColorNameSuccess), backend.WeekDoneTint)
	case mins+waiting >= backend.Target:
		return BlendColor(bg, theme.Color(theme.ColorNameSuccess), backend.WeekDraftTint)
	default:
		return color.Transparent
	}
}

// shortTint is how much warning colour a day worked but left short of the
// target takes. Lighter than the finished green: "not done yet" should not
// shout louder than "done".
const shortTint = 0.18

// ShortFill is the wash behind a day that has work on it and did not reach the
// target. It is the same warning colour the chart draws its target line in, so
// "short of 480" looks the same wherever it turns up — a row on the report, a
// cell on the calendar, the line a bar failed to reach.
//
// A day with nothing on it is not short, it is untouched, and painting the two
// alike would make an empty period look like a failed one.
func ShortFill() color.Color {
	return BlendColor(theme.Color(theme.ColorNameBackground),
		theme.Color(theme.ColorNameWarning), shortTint)
}

// IsShortDay reports whether a day was worked and left under the target.
func IsShortDay(mins int) bool { return mins > 0 && mins < backend.Target }

// IsDraftComplete reports whether a day misses the target on the board but makes
// it once the entries saved on this machine are counted.
//
// This is not a short day, and colouring it like one is wrong about the work:
// the hours were done, it is only the sending that is outstanding. Yellow says
// "you owe this day more time", which would send you looking for work you have
// already finished.
func IsDraftComplete(mins, draft int) bool {
	return draft > 0 && mins < backend.Target && mins+draft >= backend.Target
}

// DraftDoneFill is the wash behind a day that only reaches the target by
// counting what is waiting to be pushed. Green, at the same weight as the
// short-day yellow it replaces, and paler than the week strip's finished green
// for the same reason that one is: a day finished on GitHub is stated, one
// finished by counting drafts is a claim with a step left in it.
func DraftDoneFill() color.Color {
	return BlendColor(theme.Color(theme.ColorNameBackground),
		theme.Color(theme.ColorNameSuccess), shortTint)
}

// captionsIn is the captions with something to say. An empty one takes no room
// and forces no second line.
func captionsIn(objs []fyne.CanvasObject) []fyne.CanvasObject {
	var out []fyne.CanvasObject
	for _, o := range objs {
		if t, ok := o.(*canvas.Text); ok && strings.TrimSpace(t.Text) == "" {
			continue
		}
		out = append(out, o)
	}
	return out
}

func WeekCaption(s string) *canvas.Text {
	t := canvas.NewText(s, theme.Color(theme.ColorNamePlaceHolder))
	t.TextSize = theme.CaptionTextSize()
	return t
}
