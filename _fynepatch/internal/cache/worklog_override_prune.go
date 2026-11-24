package cache

// Worklog patch, copied into the private Fyne build by tools/patchfyne. The go tool
// skips folders that start with "_", so this file only builds there.

import "fyne.io/fyne/v2"

// worklogPruneOverrides forgets the theme of every object inside a
// ThemeOverride that is no longer drawn. Fyne records an override's theme
// against each object in it, text and shapes included, and only let go of a
// widget's record when its renderer expired. Every other record stayed, and
// held the whole override with it, so each redraw that built new overrides
// kept the old page alive for good: memory climbed with every menu opened.
//
// An override whose renderer has gone applies its theme again when it is
// next drawn and refreshes its content (see worklog_theme_owner.go in the
// container package), so dropping the
// records of one that is only hidden costs nothing.
func worklogPruneOverrides() {
	overrides.Range(func(o fyne.CanvasObject, s *overrideScope) bool {
		owned, ok := s.th.(interface {
			WorklogOwner() fyne.Widget
			WorklogPruned()
		})
		if !ok {
			return true
		}
		owner := owned.WorklogOwner()
		if owner == nil {
			return true
		}
		if _, drawn := renderers.Load(owner); !drawn {
			overrides.Delete(o)
			owned.WorklogPruned()
		}
		return true
	})
}
