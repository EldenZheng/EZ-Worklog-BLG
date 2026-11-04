package report

import (
	"time"
	"worklog/internal/shared/backend"
)

// RemainingCycleProjection tops up today and future dates to eight hours. It
// preserves actual/draft minutes and caps payable time with the normal pay math.
func RemainingCycleProjection(cfg backend.Config, totals map[string]int, from, to, asOf string, weekdaysOnly bool) (backend.Report, int) {
	projected := make(map[string]int, len(totals))
	for day, minutes := range totals {
		projected[day] = minutes
	}
	start, a := time.Parse("2006-01-02", from)
	end, b := time.Parse("2006-01-02", to)
	now, c := time.Parse("2006-01-02", asOf)
	count := 0
	if a == nil && b == nil && c == nil {
		if start.Before(now) {
			start = now
		}
		for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
			if weekdaysOnly && (day.Weekday() == time.Saturday || day.Weekday() == time.Sunday) {
				continue
			}
			count++
			key := day.Format("2006-01-02")
			if projected[key] < backend.Target {
				projected[key] = backend.Target
			}
		}
	}
	return backend.ReportFromTotals(cfg, projected), count
}
