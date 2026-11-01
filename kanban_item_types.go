package main

import (
	"fmt"
	"image/color"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const kanbanIssues = "Issues"
const kanbanPullRequests = "Pull requests"

func (it sprintItem) itemType() string {
	if it.Kind == "PullRequest" {
		return kanbanPullRequests
	}
	// Older local snapshots predate the GraphQL type field.
	if u, err := url.Parse(it.URL); err == nil {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 4 && parts[2] == "pull" {
			return kanbanPullRequests
		}
	}
	return kanbanIssues
}

func (k *kanbanUI) visibleItemType(it sprintItem) bool {
	if it.Kind == "DraftIssue" {
		return false
	}
	if k.plan.KanbanTypes == nil {
		return true
	}
	for _, selected := range *k.plan.KanbanTypes {
		if selected == it.itemType() {
			return true
		}
	}
	return false
}

func (k *kanbanUI) itemTypeControls() fyne.CanvasObject {
	// A wide gap keeps the two filters reading as separate toggles.
	row := container.New(layout.NewCustomPaddedHBoxLayout(theme.Padding() * 8))
	for _, kind := range []string{kanbanIssues, kanbanPullRequests} {
		kind := kind
		it := sprintItem{Kind: "Issue"}
		label := "Issues"
		if kind == kanbanPullRequests {
			it.Kind = "PullRequest"
			label = "Pull Request"
		}
		ink := color.Color(logoGold)
		if !k.visibleItemType(it) {
			ink = theme.Color(theme.ColorNamePlaceHolder)
		}
		content := container.NewHBox(container.NewCenter(container.NewGridWrap(fyne.NewSize(16, 16), widget.NewIcon(it.typeIcon()))), widget.NewLabel("|"), widget.NewLabel(label))
		row.Add(newCardTappable(container.NewThemeOverride(content, statusHeaderTheme{Theme: theme.Current(), ink: ink}), func() {
			var selected []string
			for _, candidate := range []sprintItem{{Kind: "Issue"}, {Kind: "PullRequest"}} {
				visible := k.visibleItemType(candidate)
				if candidate.itemType() == kind {
					visible = !visible
				}
				if visible {
					selected = append(selected, candidate.itemType())
				}
			}
			k.setItemTypes(selected)
		}))
	}
	k.typeChoiceView = row
	return row
}

func (k *kanbanUI) setItemTypes(selected []string) {
	types := append([]string{}, selected...)
	p := k.plan
	p.KanbanTypes = &types
	if k.save(p) {
		k.draw()
	}
}

var kanbanIssueIcon = theme.NewThemedResource(fyne.NewStaticResource("kanban-issue.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><path fill="#000" fill-rule="evenodd" d="M8 1 C11.866 1 15 4.134 15 8 C15 11.866 11.866 15 8 15 C4.134 15 1 11.866 1 8 C1 4.134 4.134 1 8 1 Z M8 2.5 C4.962 2.5 2.5 4.962 2.5 8 C2.5 11.038 4.962 13.5 8 13.5 C11.038 13.5 13.5 11.038 13.5 8 C13.5 4.962 11.038 2.5 8 2.5 Z M8 6.5 C8.828 6.5 9.5 7.172 9.5 8 C9.5 8.828 8.828 9.5 8 9.5 C7.172 9.5 6.5 8.828 6.5 8 C6.5 7.172 7.172 6.5 8 6.5 Z"/></svg>`)))
var kanbanPullRequestIcon = theme.NewThemedResource(fyne.NewStaticResource("kanban-pull-request.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><path fill="#000" fill-rule="evenodd" d="M4 .5 C7.333 .5 7.333 5.5 4 5.5 C.667 5.5 .667 .5 4 .5 Z M4 2 C2.667 2 2.667 4 4 4 C5.333 4 5.333 2 4 2 Z M4 10.5 C7.333 10.5 7.333 15.5 4 15.5 C.667 15.5 .667 10.5 4 10.5 Z M4 12 C2.667 12 2.667 14 4 14 C5.333 14 5.333 12 4 12 Z M12 10.5 C15.333 10.5 15.333 15.5 12 15.5 C8.667 15.5 8.667 10.5 12 10.5 Z M12 12 C10.667 12 10.667 14 12 14 C13.333 14 13.333 12 12 12 Z"/><path fill="#000" d="M3.25 5.5 H4.75 V10.5 H3.25 Z M9 0 L6 3 L9 6 V3.75 C10.243 3.75 11.25 4.757 11.25 6 V10.5 H12.75 V6 C12.75 3.929 11.071 2.25 9 2.25 Z"/></svg>`)))

func (it sprintItem) typeIcon() fyne.Resource {
	if it.itemType() == kanbanPullRequests {
		return kanbanPullRequestIcon
	}
	return kanbanIssueIcon
}

func (it sprintItem) repositoryNumber(b sprintBoard) string {
	if u, err := url.Parse(it.URL); err == nil {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 4 && (parts[2] == "issues" || parts[2] == "pull") {
			return parts[0] + "/" + parts[1] + "#" + parts[3]
		}
	}
	return orDefault((WorklogItem{URL: it.URL}).Repo(), projectShortName(b))
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

func (k *kanbanUI) cardContents(b sprintBoard, it sprintItem) fyne.CanvasObject {
	repository := widget.NewLabel(it.repositoryNumber(b))
	repository.Wrapping = fyne.TextWrapWord
	header := container.NewBorder(nil, nil, container.NewCenter(container.NewGridWrap(fyne.NewSize(16, 16), widget.NewIcon(it.typeIcon()))), nil, container.NewThemeOverride(repository, kanbanMetadataTheme{Theme: theme.Current(), textSize: theme.CaptionTextSize() - 1}))
	title := widget.NewLabelWithStyle(it.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord
	pink := color.NRGBA{R: 225, G: 131, B: 178, A: 255}
	body := container.NewVBox(header, title,
		kanbanMetadata(fmt.Sprintf("Project estimate: %dm", sprintEstimate(it, sprintTask{})), pink),
		kanbanMetadata("Complexity: "+orDefault(it.Complexity, "Not set"), pink))
	footer := container.NewBorder(nil, nil, nil, nil)
	if it.URL != "" {
		label := "Open issue"
		if it.itemType() == kanbanPullRequests {
			label = "Open PR"
		}
		footer = container.NewBorder(nil, nil, container.NewThemeOverride(githubHyperlink(label, it.URL), kanbanMetadataTheme{Theme: theme.Current()}), nil)
	}
	if k.filter == "all" && it.SprintID != "" {
		for _, sprint := range b.iterations() {
			if sprint.ID == it.SprintID {
				footer.Add(kanbanMetadata("– "+sprint.Title, theme.Color(theme.ColorNameSuccess)))
				break
			}
		}
	}
	body.Add(footer)
	return body
}
