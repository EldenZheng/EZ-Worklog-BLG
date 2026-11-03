package kanban

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
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func itemTypeControls(k *shell.KanbanUI) fyne.CanvasObject {
	// A wide gap keeps the two filters reading as separate toggles.
	row := container.New(layout.NewCustomPaddedHBoxLayout(theme.Padding() * 8))
	for _, kind := range []string{backend.KanbanIssues, backend.KanbanPullRequests} {
		kind := kind
		it := backend.SprintItem{Kind: "Issue"}
		label := "Issues"
		if kind == backend.KanbanPullRequests {
			it.Kind = "PullRequest"
			label = "Pull Request"
		}
		ink := color.Color(widgets.LogoGold)
		if !k.VisibleItemType(it) {
			ink = theme.Color(theme.ColorNamePlaceHolder)
		}
		// Plain text, not labels: a label's own inset left a wide gap either
		// side of the bar. The padding goes round the whole chip instead, so
		// the hover outline sits clear of the icon and the words.
		bar := canvas.NewText("|", ink)
		name := canvas.NewText(label, ink)
		content := container.NewPadded(container.NewHBox(
			container.NewCenter(container.NewGridWrap(fyne.NewSize(16, 16), widget.NewIcon(it.TypeIcon()))),
			container.NewCenter(bar), container.NewCenter(name)))
		row.Add(shell.NewCardTappable(container.NewThemeOverride(content, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: ink}), func() {
			var selected []string
			for _, candidate := range []backend.SprintItem{{Kind: "Issue"}, {Kind: "PullRequest"}} {
				visible := k.VisibleItemType(candidate)
				if candidate.ItemType() == kind {
					visible = !visible
				}
				if visible {
					selected = append(selected, candidate.ItemType())
				}
			}
			SetItemTypes(k, selected)
		}))
	}
	k.TypeChoiceView = row
	return row
}

func SetItemTypes(k *shell.KanbanUI, selected []string) {
	types := append([]string{}, selected...)
	p := k.Plan
	p.KanbanTypes = &types
	if k.Save(p) {
		Draw(k)
	}
}

type kanbanMetadataTheme struct {
	fyne.Theme
	ink      color.Color
	textSize float32
}

func (t kanbanMetadataTheme) Size(name fyne.ThemeSizeName) float32 {
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

func (t kanbanMetadataTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground && t.ink != nil {
		return t.ink
	}
	return t.Theme.Color(name, v)
}

func kanbanMetadata(text string, ink color.Color, align fyne.TextAlign) fyne.CanvasObject {
	label := widget.NewLabel(text)
	label.Alignment = align
	label.Wrapping = fyne.TextWrapWord
	return container.NewThemeOverride(label, kanbanMetadataTheme{Theme: theme.Current(), ink: ink})
}

func CardContents(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem) fyne.CanvasObject {
	// The owner/repo#number is the link to the issue or pull request itself, so
	// the card needs no separate "Open" link at the bottom.
	var repository fyne.CanvasObject
	if link, ok := shell.NewHoverLink(it.RepositoryNumber(b), it.URL).(*shell.HoverLink); ok {
		link.Wrapping = fyne.TextWrapWord
		repository = link
	} else {
		label := widget.NewLabel(it.RepositoryNumber(b))
		label.Wrapping = fyne.TextWrapWord
		repository = label
	}
	icon := container.NewThemeOverride(widget.NewIcon(it.TypeIcon()), widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	header := container.NewBorder(nil, nil, container.NewCenter(container.NewGridWrap(fyne.NewSize(16, 16), icon)), nil, container.NewThemeOverride(repository, kanbanMetadataTheme{Theme: theme.Current(), textSize: theme.CaptionTextSize() - 1}))
	title := widget.NewLabelWithStyle(it.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord
	pink := color.NRGBA{R: 225, G: 131, B: 178, A: 255}
	body := container.NewVBox(header)
	// The parent issue, if any, as a gold tag under the link: muted at rest,
	// bright under the pointer, and a click opens the parent on GitHub.
	if it.ParentTitle != "" {
		body.Add(shell.ParentTag(it.ParentTitle, it.ParentURL))
	}
	body.Add(title)
	// A line across the middle, then the estimate on the left and the
	// complexity on the right; the sprint, when shown, has the row under it.
	body.Add(cardDivider())
	body.Add(container.NewGridWithColumns(2,
		kanbanMetadata(fmt.Sprintf("Estimate: %dm", backend.SprintEstimate(it, backend.SprintTask{})), pink, fyne.TextAlignLeading),
		kanbanMetadata("Complexity: "+backend.OrDefault(it.Complexity, "Not set"), pink, fyne.TextAlignTrailing)))
	if k.Filter == "all" && it.SprintID != "" {
		for _, sprint := range b.Iterations() {
			if sprint.ID == it.SprintID {
				body.Add(shell.HugTag(shell.NewHoverTag(sprint.Title, shell.GitHubGreen, shell.GitHubGreen, color.White, color.White, nil)))
				break
			}
		}
	}
	return body
}

// cardDivider is the plain grey line across a card, the same grey as the
// sidebar's dividers, whatever the card's tint.
func cardDivider() fyne.CanvasObject {
	line := canvas.NewRectangle(shell.BlendColor(theme.Color(theme.ColorNameBackground), theme.Color(theme.ColorNameForeground), 0.35))
	line.SetMinSize(fyne.NewSize(0, 1))
	return line
}
