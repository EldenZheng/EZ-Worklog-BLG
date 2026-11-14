package shell

import (
	"math"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// AutoGrow returns a function that sizes a word-wrapped multi-line entry to
// its text, never below minRows nor, when maxRows is above zero, past it, and
// then calls relayout. Call it whenever the text changes. Fyne has no wrapped
// line count, so it measures each paragraph against the entry's text width.
// Log Work's remarks and the Projects editor's issue box grow by it.
func AutoGrow(e *widget.Entry, minRows, maxRows int, relayout func()) func() {
	shown := minRows
	return func() {
		rows := max(wrappedRows(e), minRows)
		if maxRows > 0 {
			rows = min(rows, max(maxRows, minRows))
		}
		if rows == shown {
			return
		}
		shown = rows
		e.SetMinRowsVisible(rows)
		if relayout != nil {
			relayout()
		}
	}
}

func wrappedRows(e *widget.Entry) int {
	paragraphs := strings.Split(e.Text, "\n")
	// The text sits inside the entry's inner padding and a little more; a
	// cautious width over-counts a row rather than leaving one hidden.
	width := e.Size().Width - 3*theme.InnerPadding()
	if width <= 0 {
		return len(paragraphs)
	}
	rows := 0
	for _, p := range paragraphs {
		n := int(math.Ceil(float64(fyne.MeasureText(p, theme.TextSize(), e.TextStyle).Width / width)))
		rows += max(n, 1)
	}
	return rows
}
