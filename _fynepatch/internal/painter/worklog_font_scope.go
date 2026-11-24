package painter

// Worklog patch, copied into the private Fyne build by tools/patchfyne. The go tool
// skips folders that start with "_", so this file only builds there.

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// worklogFontScope is the font cache's key for text drawn in o: the font its
// theme picks for style. Fyne keyed it by the theme override around the text
// instead, and every override has a scope of its own, so each override a
// redraw built parsed the fonts again, the large system fallbacks included,
// and kept them for good: memory climbed by the hundreds of MB as the menus
// were opened. Overrides that pick the same font now share one entry.
func worklogFontScope(style fyne.TextStyle, o fyne.CanvasObject) string {
	f := theme.CurrentForWidget(o).Font(style)
	if f == nil {
		return ""
	}
	return "font:" + f.Name()
}
