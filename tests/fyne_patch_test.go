//go:build cursors

package tests

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type wheelReach interface {
	CanScrollBy(dx, dy float32) bool
}

// The window sends the wheel to the innermost scroller that can move, so a
// text field or a list at its end must say no and let the page scroll.
func TestScrollReportsWhetherTheWheelCanMoveIt(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	tall := canvas.NewRectangle(color.Transparent)
	tall.SetMinSize(fyne.NewSize(100, 1000))
	page := container.NewVScroll(tall)
	page.Resize(fyne.NewSize(100, 200))
	reach, ok := fyne.CanvasObject(page).(wheelReach)
	if !ok {
		t.Fatal("container.Scroll has no CanScrollBy: build without the _fynepatch files")
	}
	if !reach.CanScrollBy(0, -25) {
		t.Error("a page at the top should scroll down")
	}
	if reach.CanScrollBy(0, 25) {
		t.Error("a page at the top should not take an upward wheel")
	}
	if reach.CanScrollBy(-25, 0) {
		t.Error("a vertical page should not take a sideways wheel")
	}
	page.ScrollToBottom()
	if reach.CanScrollBy(0, -25) {
		t.Error("a page at the bottom should pass a downward wheel on")
	}

	wide := canvas.NewRectangle(color.Transparent)
	wide.SetMinSize(fyne.NewSize(1000, 20))
	strip := container.NewHScroll(wide)
	strip.Resize(fyne.NewSize(200, 20))
	if strip.CanScrollBy(0, -25) {
		t.Error("a sideways strip should pass a vertical wheel on")
	}
	if !strip.CanScrollBy(-25, 0) {
		t.Error("a sideways strip should take a sideways wheel")
	}
}

// The press flash is drawn with the same rounded corners as the button.
func TestButtonPressFlashIsRounded(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	b := widget.NewButtonWithIcon("", theme.SettingsIcon(), nil)
	var rects []*canvas.Rectangle
	for _, o := range test.WidgetRenderer(b).Objects() {
		if r, ok := o.(*canvas.Rectangle); ok {
			rects = append(rects, r)
		}
	}
	if len(rects) < 2 {
		t.Fatalf("expected background and press flash rectangles, got %d", len(rects))
	}
	want := theme.Size(theme.SizeNameButtonRadius)
	if want == 0 {
		t.Skip("theme has square buttons")
	}
	if got := rects[1].CornerRadius; got != want {
		t.Errorf("press flash corner radius = %v, want %v", got, want)
	}
}
