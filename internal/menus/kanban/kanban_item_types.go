package kanban

import (
	"fmt"
	"image/color"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
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
		content := container.NewHBox(container.NewCenter(container.NewGridWrap(fyne.NewSize(16, 16), widget.NewIcon(it.TypeIcon()))), widget.NewLabel("|"), widget.NewLabel(label))
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

func kanbanMetadata(text string, ink color.Color) fyne.CanvasObject {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	return container.NewThemeOverride(label, kanbanMetadataTheme{Theme: theme.Current(), ink: ink})
}

func CardContents(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem) fyne.CanvasObject {
	repository := widget.NewLabel(it.RepositoryNumber(b))
	repository.Wrapping = fyne.TextWrapWord
	header := container.NewBorder(nil, nil, container.NewCenter(container.NewGridWrap(fyne.NewSize(16, 16), widget.NewIcon(it.TypeIcon()))), nil, container.NewThemeOverride(repository, kanbanMetadataTheme{Theme: theme.Current(), textSize: theme.CaptionTextSize() - 1}))
	title := widget.NewLabelWithStyle(it.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord
	pink := color.NRGBA{R: 225, G: 131, B: 178, A: 255}
	body := container.NewVBox(header, title,
		kanbanMetadata(fmt.Sprintf("Project estimate: %dm", backend.SprintEstimate(it, backend.SprintTask{})), pink),
		kanbanMetadata("Complexity: "+backend.OrDefault(it.Complexity, "Not set"), pink))
	footer := container.NewBorder(nil, nil, nil, nil)
	if it.URL != "" {
		label := "Open issue"
		if it.ItemType() == backend.KanbanPullRequests {
			label = "Open PR"
		}
		footer = container.NewBorder(nil, nil, container.NewThemeOverride(shell.GithubHyperlink(label, it.URL), kanbanMetadataTheme{Theme: theme.Current()}), nil)
	}
	if k.Filter == "all" && it.SprintID != "" {
		for _, sprint := range b.Iterations() {
			if sprint.ID == it.SprintID {
				footer.Add(kanbanMetadata("– "+sprint.Title, theme.Color(theme.ColorNameSuccess)))
				break
			}
		}
	}
	body.Add(footer)
	return body
}
