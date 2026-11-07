package tests

import (
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
)

// overlayField reads an exported field of Fyne's internal overlay container.
func overlayField(o fyne.CanvasObject, name string) any {
	return reflect.ValueOf(o).Elem().FieldByName(name).Interface()
}

// A popup opens below where it settles, over a clear backdrop, and ends exactly
// in place with the backdrop at full strength.
func TestPopupsOpenRisingIntoPlace(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	w.SetContent(widget.NewLabel("behind"))
	w.Resize(fyne.NewSize(900, 700))
	shell.ShowDialog(dialog.NewInformation("Saved", "Done.", w), w)

	top := w.Canvas().Overlays().Top()
	popup := overlayField(top, "Content").(fyne.CanvasObject)
	backdrop := overlayField(top, "Background").(*fyne.Container)
	var shade *canvas.Rectangle
	for _, o := range backdrop.Objects {
		if r, ok := o.(*canvas.Rectangle); ok {
			shade = r
		}
	}
	final, full := popup.Position(), shade.FillColor
	if _, _, _, alpha := full.RGBA(); alpha == 0 {
		t.Fatal("the backdrop should end visible")
	}

	anim := shell.AnimateOpening(w.Canvas())
	if anim == nil {
		t.Fatal("a shown popup should animate")
	}
	anim.Tick(0)
	if start := popup.Position(); start.X != final.X || start.Y <= final.Y {
		t.Fatalf("the popup should start below where it settles: %v, then %v", start, final)
	}
	if _, _, _, alpha := shade.FillColor.RGBA(); alpha != 0 {
		t.Fatal("the backdrop should start clear")
	}
	anim.Tick(1)
	if popup.Position() != final || color.NRGBAModel.Convert(shade.FillColor) != color.NRGBAModel.Convert(full) {
		t.Fatal("the popup should end in place over the full backdrop")
	}
}

// Closing hides the popup at once and fades a picture of it away over the page,
// leaving nothing behind once the motion ends.
func TestPopupsCloseFadingAway(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	shell.FitScreen(w, widget.NewLabel("behind"))
	w.Resize(fyne.NewSize(900, 700))
	layer := w.Content().(*fyne.Container).Objects[1].(*fyne.Container)

	d := dialog.NewCustomWithoutButtons("Editor", widget.NewLabel("Fields"), w)
	shell.ShowDialog(d, w)
	if w.Canvas().Overlays().Top() == nil {
		t.Fatal("the dialog should be open")
	}
	if anim := shell.CloseDialog(d, w); anim == nil {
		t.Fatal("closing a dialog shown with ShowDialog should animate")
	}
	if w.Canvas().Overlays().Top() != nil {
		t.Fatal("the dialog should be hidden at once, not after the motion")
	}
	if len(layer.Objects) != 0 {
		t.Fatal("the fading picture should be gone once the motion ends")
	}

	// A stock dialog's own OK closes it with the motion too.
	shell.ShowInformation("Saved", "Done.", w)
	var ok *widget.Button
	walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if b, isButton := o.(*widget.Button); isButton && b.Text == "OK" {
			ok = b
		}
	})
	if ok == nil {
		t.Fatal("the information dialog has no OK")
	}
	test.Tap(ok)
	if w.Canvas().Overlays().Top() != nil || len(layer.Objects) != 0 {
		t.Fatal("OK should close the dialog and leave nothing behind")
	}
}

// A popup opened while another closes stays open: closing must not take the
// popups above it down with it, as hiding late would.
func TestClosingAPopupKeepsTheOneOpenedNext(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	shell.FitScreen(w, widget.NewLabel("behind"))
	w.Resize(fyne.NewSize(900, 700))
	list := dialog.NewCustomWithoutButtons("Pending work", widget.NewLabel("choices"), w)
	shell.ShowDialog(list, w)
	shell.CloseDialog(list, w)
	editor := dialog.NewCustomWithoutButtons("Editor", widget.NewLabel("form"), w)
	shell.ShowDialog(editor, w)
	if len(w.Canvas().Overlays().List()) != 1 {
		t.Fatal("the editor opened after the list should stay open")
	}
}

// Every popup goes through the motion: Fyne's dialog.Show functions skip it,
// each popup the code builds is shown by ShowDialog (or the stock variants) or
// followed by AnimateOpening, and dialogs and popups are closed by CloseDialog
// or ClosePopUp rather than their own Hide.
func TestPopupsOpenThroughTheMotion(t *testing.T) {
	stockShow := regexp.MustCompile(`\bdialog\.Show\w*\(`)
	built := regexp.MustCompile(`\bdialog\.New\w*\(|\bwidget\.NewModalPopUp\(`)
	opened := regexp.MustCompile(`\b(ShowDialog|ShowStockDialog|AnimateStockDialog|AnimateOpening)\(`)
	plainHide := regexp.MustCompile(`(^|[^.\w])(d|popup|pop)\.Hide\b`)
	files := []string{filepath.Join("..", "main.go")}
	_ = filepath.WalkDir(filepath.Join("..", "internal"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return err
	})
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var code []string
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "//") {
				code = append(code, line)
			}
		}
		src := strings.Join(code, "\n")
		if stockShow.MatchString(src) {
			t.Errorf("%s opens a popup with Fyne's dialog.Show, which skips the opening motion; use shell.ShowDialog, ShowInformation or ShowError", path)
		}
		if n, m := len(built.FindAllString(src, -1)), len(opened.FindAllString(src, -1)); n > m {
			t.Errorf("%s builds %d popups but opens %d through shell.ShowDialog or shell.AnimateOpening", path, n, m)
		}
		if strings.HasSuffix(filepath.ToSlash(path), "shell/popup_motion.go") {
			continue // where the closing motion calls the real Hide
		}
		for _, line := range code {
			if plainHide.MatchString(line) {
				t.Errorf("%s hides a popup without the closing motion; use shell.CloseDialog or shell.ClosePopUp: %s", path, strings.TrimSpace(line))
			}
		}
	}
}
