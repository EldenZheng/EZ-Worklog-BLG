package main

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func settingsHeading() fyne.CanvasObject {
	name := canvas.NewText("Worklog", theme.Color(theme.ColorNameForeground))
	name.TextSize = 26
	name.TextStyle.Bold = true
	settings := canvas.NewText("Settings", logoGold)
	settings.TextSize = 22
	settings.TextStyle.Bold = true
	return container.NewPadded(container.NewHBox(appLogo(68), container.NewVBox(name, settings)))
}

type githubProfile struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	URL       string `json:"html_url"`
	AvatarURL string `json:"avatar_url"`
}

// One profile request per session, on first opening Settings. An avatar is
// optional; a failed image request must not hide the account or block editing.
func (ui *UI) loadGitHubProfile(force bool) {
	if ui.profileBox == nil || ui.profileLoading || (ui.profileLoaded && !force) {
		return
	}
	ui.profileLoading = true
	progress, finish := ui.beginFetch("profile", "Loading GitHub profile")
	var profile githubProfile
	var avatar image.Image
	var profileErr error
	ui.async(func() error {
		progress(fetchProgress{Total: 2, Stage: "profile steps"})
		out, err := gh([]string{"api", "user"})
		if err == nil {
			err = json.Unmarshal([]byte(out), &profile)
		}
		profileErr = err
		if err != nil {
			return nil
		}
		progress(fetchProgress{Done: 1, Total: 2, Stage: "profile steps"})
		if u, err := url.Parse(profile.AvatarURL); err == nil && u.Scheme == "https" {
			client := &http.Client{Timeout: 10 * time.Second}
			if resp, err := client.Get(u.String()); err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					if data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)); err == nil {
						avatar, _, _ = image.Decode(bytes.NewReader(data))
					}
				}
			}
		}
		progress(fetchProgress{Done: 2, Total: 2, Stage: "profile steps"})
		return nil
	}, func() {
		defer finish()
		ui.profileLoading = false
		ui.profileLoaded = true
		retry := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() { ui.loadGitHubProfile(true) })
		if profileErr != nil {
			ui.profileBox.Objects = []fyne.CanvasObject{container.NewHBox(widget.NewLabel("Could not load GitHub profile."), retry)}
		} else {
			var portrait fyne.CanvasObject = widget.NewIcon(theme.AccountIcon())
			if avatar != nil {
				img := canvas.NewImageFromImage(avatar)
				img.FillMode = canvas.ImageFillContain
				img.SetMinSize(fyne.NewSquareSize(48))
				portrait = container.NewCenter(img)
			}
			u, _ := url.Parse(profile.URL)
			name := strings.TrimSpace(profile.Name)
			if name == "" {
				name = profile.Login
			}
			ui.profileBox.Objects = []fyne.CanvasObject{container.NewHBox(portrait,
				container.NewVBox(bold(name), widget.NewHyperlink("@"+profile.Login, u)), layout.NewSpacer(), retry)}
		}
		ui.profileBox.Refresh()
	})
}
