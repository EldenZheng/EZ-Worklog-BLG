package shell

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

// CardPink is the estimate and complexity ink on Kanban's cards.
var CardPink = color.NRGBA{R: 225, G: 131, B: 178, A: 255}

// CardLinkSize is the text size of a card's repo#number link, the same as on
// the Sprint Calendar's blocks, large enough to hit easily.
const CardLinkSize = float32(13)

// cardTitleLift is how far a card's title is drawn into its label's top padding.
const cardTitleLift = float32(4)

// CardOptions changes what CardBody shows. The zero value is a Kanban card.
type CardOptions struct {
	// Link and LinkLabel are the header's link and its text: the item's own
	// URL and repo#number when left empty.
	Link, LinkLabel string
	// Title replaces the item's title, as a PR's linked issue's title does.
	Title string
	// Estimate is the minutes on the footer's left.
	Estimate int
	// Badge sits at the right of the header line, such as a CountBadge.
	Badge fyne.CanvasObject
	// NoFooter leaves out the line and the estimate / complexity row.
	NoFooter bool
}

// CardBody is the inside of a Kanban card: the issue or PR mark and its
// repo#number link, the parent tag, the title, then a line and the
// estimate and complexity. The Sprint Calendar's tray cards are built from it
// too, so both menus' bubbles read alike.
func CardBody(b backend.SprintBoard, it backend.SprintItem, o CardOptions) *fyne.Container {
	link := backend.OrDefault(o.Link, it.URL)
	// repo#number without the owner: the card's colour already names it.
	label := backend.OrDefault(o.LinkLabel, backend.OrDefault(ShortIssueRef(it.URL), it.RepositoryNumber(b)))
	// The repo#number is the link to the issue or pull request itself, so
	// the card needs no separate "Open" link at the bottom.
	var repository fyne.CanvasObject
	if l, ok := NewHoverLink(label, link).(*HoverLink); ok {
		l.Wrapping = fyne.TextWrapWord
		repository = l
	} else {
		plain := widget.NewLabel(label)
		plain.Wrapping = fyne.TextWrapWord
		repository = plain
	}
	icon := container.NewThemeOverride(widget.NewIcon(it.TypeIcon()), widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	var badge fyne.CanvasObject
	if o.Badge != nil {
		badge = container.NewCenter(o.Badge)
	}
	header := container.NewBorder(nil, nil, container.NewCenter(container.NewGridWrap(fyne.NewSize(16, 16), icon)), badge,
		container.NewThemeOverride(repository, cardMetadataTheme{Theme: theme.Current(), textSize: CardLinkSize}))
	title := widget.NewLabelWithStyle(backend.OrDefault(o.Title, it.Title), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord
	// The link, the parent tag and the title sit close: the label's own padding
	// above the title is enough of a gap, and with no parent no room is left
	// for one.
	head := container.New(layout.NewCustomPaddedVBoxLayout(2), header)
	// The parent issue, if any, as a gold tag under the link: muted at rest,
	// bright under the pointer, and a click opens the parent on GitHub.
	if it.ParentTitle != "" {
		head.Add(ParentTag(it.ParentTitle, it.ParentURL))
	}
	head.Add(container.New(layout.NewCustomPaddedLayout(-cardTitleLift, 0, 0, 0), title))
	body := container.NewVBox(head)
	if !o.NoFooter {
		// A line across the middle, then the estimate on the left and the
		// complexity on the right.
		body.Add(CardDivider())
		body.Add(container.NewGridWithColumns(2,
			CardMetadata(fmt.Sprintf("Estimate: %dm", o.Estimate), CardPink, fyne.TextAlignLeading),
			CardMetadata("Complexity: "+backend.OrDefault(it.Complexity, "Not set"), CardPink, fyne.TextAlignTrailing)))
	}
	return body
}

type cardMetadataTheme struct {
	fyne.Theme
	ink      color.Color
	textSize float32
}

func (t cardMetadataTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameText {
		if t.textSize > 0 {
			return t.textSize
		}
		return theme.CaptionTextSize()
	}
	if name == theme.SizeNameInnerPadding {
		return 4
	}
	return t.Theme.Size(name)
}

func (t cardMetadataTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground && t.ink != nil {
		return t.ink
	}
	return t.Theme.Color(name, v)
}

// CardMetadata is a card's small coloured caption, such as its estimate.
func CardMetadata(text string, ink color.Color, align fyne.TextAlign) fyne.CanvasObject {
	label := widget.NewLabel(text)
	label.Alignment = align
	label.Wrapping = fyne.TextWrapWord
	return container.NewThemeOverride(label, cardMetadataTheme{Theme: theme.Current(), ink: ink})
}

// CardDivider is the plain grey line across a card, the same grey as the
// sidebar's dividers, whatever the card's tint.
func CardDivider() fyne.CanvasObject {
	line := canvas.NewRectangle(BlendColor(theme.Color(theme.ColorNameBackground), theme.Color(theme.ColorNameForeground), 0.35))
	line.SetMinSize(fyne.NewSize(0, 1))
	return line
}
