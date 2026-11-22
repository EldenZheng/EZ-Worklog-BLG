package shell

import "worklog/internal/shared/backend"

const (
	MinutesReported = "Reported"
	MinutesApproved = "Approved"
)

// Status and Report choose a display value without altering the shared cache
// or the minutes submitted by a push. Legacy board entries have one value.
func CountedWorklogMinutes(item backend.WorklogItem, approved bool) int {
	if approved && backend.SOPApplies(item.Date) {
		return item.ApprovedMinutes
	}
	return item.Minutes
}

func CountedWorklogItems(items []backend.WorklogItem, approved bool) []backend.WorklogItem {
	if !approved {
		return items
	}
	counted := append([]backend.WorklogItem(nil), items...)
	for i := range counted {
		counted[i].Minutes = CountedWorklogMinutes(counted[i], true)
	}
	return counted
}

func NewWorklogCount(approved bool, changed func(bool)) *GoldSelect {
	pick := NewGoldSelect([]string{MinutesReported, MinutesApproved}, nil)
	pick.StayOpen = true
	mode := MinutesReported
	if approved {
		mode = MinutesApproved
	}
	pick.SetSelected(mode)
	pick.OnChanged = func(mode string) {
		next := mode == MinutesApproved
		if next != approved {
			approved = next
			changed(next)
		}
	}
	return pick
}
