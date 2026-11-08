package kanban

import (
	"image/color"

	"fyne.io/fyne/v2"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func itemTypeControls(k *shell.KanbanUI) fyne.CanvasObject {
	row := shell.ItemTypeControls(k.VisibleItemType, func(selected []string) { SetItemTypes(k, selected) })
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

// CardContents is a Kanban card's body; the Sprint Calendar's bubbles share
// its look (shell.CardBody).
func CardContents(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem) fyne.CanvasObject {
	body := shell.CardBody(b, it, shell.CardOptions{Estimate: backend.SprintEstimate(it, backend.SprintTask{})})
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
