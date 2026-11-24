package container

// Worklog patch, copied into the private Fyne build by tools/patchfyne. The go tool
// skips folders that start with "_", so this file only builds there.

import "fyne.io/fyne/v2"

// WorklogOwner is the ThemeOverride a theme was applied by, so the cache can
// forget the override's records once it is no longer drawn.
func (f *featureTheme) WorklogOwner() fyne.Widget {
	if f.over == nil {
		return nil
	}
	return f.over
}

// WorklogPruned tells the override its records were forgotten. Its next
// renderer applies the theme again and refreshes the content, since anything
// in it refreshed meanwhile was drawn without the override's theme.
func (f *featureTheme) WorklogPruned() {
	if f.over != nil {
		f.over.worklogPruned = true
	}
}

// worklogRestyle refreshes the content once, on the first walk after a
// renderer was made for an override whose records had been forgotten. Not in
// CreateRenderer itself: a refresh there walks the tree, which asks for this
// renderer again before it is stored.
func (r *overrideRenderer) worklogRestyle() {
	if !r.worklogFresh {
		return
	}
	r.worklogFresh = false
	r.parent.Content.Refresh()
}
