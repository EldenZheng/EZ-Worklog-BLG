package shell

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// externalLinkIcon is the "opens in the browser" arrow out of a box, in the
// app's gold.
var externalLinkIcon = fyne.NewStaticResource("external-link.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><path fill="#cda236" d="M3.75 2h3.5a.75.75 0 0 1 0 1.5h-3.5a.25.25 0 0 0-.25.25v8.5c0 .138.112.25.25.25h8.5a.25.25 0 0 0 .25-.25v-3.5a.75.75 0 0 1 1.5 0v3.5A1.75 1.75 0 0 1 12.25 14h-8.5A1.75 1.75 0 0 1 2 12.25v-8.5C2 2.784 2.784 2 3.75 2Zm6.854-1h4.146a.25.25 0 0 1 .25.25v4.146a.25.25 0 0 1-.427.177L13.03 4.03 9.28 7.78a.751.751 0 0 1-1.042-.018.751.751 0 0 1-.018-1.042l3.75-3.75-1.543-1.543A.25.25 0 0 1 10.604 1Z"/></svg>`))

// ExternalLinkIcon is the icon that sits beside a link to GitHub.
func ExternalLinkIcon() fyne.Resource { return externalLinkIcon }

// linkIcon is the icon beside a hyperlink; tapping it opens the same page.
type linkIcon struct {
	widget.Icon
	u *url.URL
}

func newLinkIcon(u *url.URL) *linkIcon {
	i := &linkIcon{u: u}
	i.Resource = externalLinkIcon
	i.ExtendBaseWidget(i)
	return i
}

func (i *linkIcon) MinSize() fyne.Size {
	s := theme.TextSize()
	return fyne.NewSize(s, s)
}

func (i *linkIcon) Tapped(*fyne.PointEvent) { _ = fyne.CurrentApp().OpenURL(i.u) }

func (*linkIcon) Cursor() desktop.Cursor { return desktop.PointerCursor }

// HyperlinkWithIcon is a link and, beside it, the external-link icon.
func HyperlinkWithIcon(label string, u *url.URL) fyne.CanvasObject {
	return container.NewHBox(widget.NewHyperlink(label, u), container.NewCenter(newLinkIcon(u)))
}
