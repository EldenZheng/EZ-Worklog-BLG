package settings

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

func settingsButton(label string, tapped func()) *widget.Button {
	return settingsIconButton(label, nil, tapped)
}

func settingsIconButton(label string, icon fyne.Resource, tapped func()) *widget.Button {
	b := widget.NewButtonWithIcon(label, icon, tapped)
	if hover, ok := any(b).(interface{ WorklogEnableHoverFade() }); ok {
		hover.WorklogEnableHoverFade()
	}
	return b
}
