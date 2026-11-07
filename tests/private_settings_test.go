package tests

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/settings"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestPrivateSettingsStartEmpty(t *testing.T) {
	cfg, _ := backend.NewStore(tempDir(t)).LoadConfig()
	if cfg.BaseSalary != 0 || cfg.SupportSetPay != 0 || cfg.MeetingRepo != "" ||
		cfg.BulkCodeReviewRepo != "" || len(cfg.BoardViewURLs) != 0 {
		t.Fatal("fresh installs must not inherit personal destinations or pay")
	}
	if links := backend.WeekBoardLinks(cfg, "", "2026-08-24", "2026-08-30"); len(links) != 0 {
		t.Fatalf("fresh installs must not link someone else's boards: %v", links)
	}
	if sets, bonus := backend.WeekendSupportBonus(4, 10, cfg.SupportSetPay); sets != 2 || bonus != 0 {
		t.Fatalf("unset pay must count sets without inventing payment: %d, %v", sets, bonus)
	}
	for _, ensure := range []func(backend.Config, string, string) (string, error){backend.EnsureMeetingIssue, backend.EnsureCodeReviewIssue} {
		if _, err := ensure(backend.Config{DisplayName: "Alex"}, "2026-08-24", "notes"); err == nil || !strings.Contains(err.Error(), "repository") {
			t.Fatalf("missing destination must fail before GitHub access: %v", err)
		}
	}
}

func TestConfiguredBoardLinksFallbackToOwnProjects(t *testing.T) {
	cfg := backend.Config{ProjectURL: "https://github.com/orgs/exampleorg/projects/3/views/2?visibleFields=Title"}
	links := backend.WeekBoardLinks(cfg, "team-alex", "2026-08-24", "2026-08-30")
	if len(links) != 1 {
		t.Fatalf("expected configured project fallback, got %v", links)
	}
	u, err := url.Parse(links[0])
	if err != nil || u.Path != "/orgs/exampleorg/projects/3/views/2" || u.Query().Get("visibleFields") != "Title" ||
		u.Query().Get("filterQuery") != backend.WorklogProjectFilter("team-alex", "2026-08-24", "2026-08-30") {
		t.Fatalf("project view must survive with the requested owner and dates: %v", links)
	}
}

func TestPaySettingsRejectInvalidAmounts(t *testing.T) {
	for _, input := range []string{"-1", "NaN", "+Inf", "1e999", "abc"} {
		if _, err := backend.ParsePaySetting(input); err == nil {
			t.Errorf("invalid amount accepted: %q", input)
		}
	}
	for input, want := range map[string]float64{"": 0, "0": 0, " 123.45 ": 123.45} {
		if got, err := backend.ParsePaySetting(input); err != nil || got != want {
			t.Errorf("%q: got %v, %v", input, got, err)
		}
	}
}

func TestSettingsSavePrivateDestinationsAndPay(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	cfg := backend.DefaultConfig()
	cfg.GithubAuthor = "sample-user" // no profile detection is needed
	cfg.BaseSalary, cfg.SupportSetPay = 1234, 125
	cfg.MeetingRepo, cfg.BulkCodeReviewRepo = "exampleorg/meetings", "exampleorg/reviews"
	cfg.BoardViewURLs = []string{"https://github.com/orgs/exampleorg/projects/3/views/2"}
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: a.NewWindow("settings"), Cfg: cfg}
	view := settings.BuildSettingsTab(ui)
	updates := map[string]string{
		"1234": "2345", "125": "187.50",
		cfg.MeetingRepo: "exampleorg/team-meetings", cfg.BulkCodeReviewRepo: "exampleorg/team-reviews",
		cfg.BoardViewURLs[0]: "https://github.com/orgs/exampleorg/projects/4/views/5?visibleFields=Title",
	}
	var save *widget.Button
	walk(view, func(o fyne.CanvasObject) {
		if entry, ok := o.(*widget.Entry); ok {
			if replacement, found := updates[entry.Text]; found {
				delete(updates, entry.Text)
				entry.SetText(replacement)
			}
		}
		if button, ok := o.(*widget.Button); ok && button.Text == "Save settings" {
			save = button
		}
	})
	if save == nil || len(updates) != 0 {
		t.Fatalf("configured settings must have editable fields; missing %d fields", len(updates))
	}
	save.OnTapped()
	got, exists := ui.Store.LoadConfig()
	if !exists || got.BaseSalary != 2345 || got.SupportSetPay != 187.5 ||
		got.MeetingRepo != "exampleorg/team-meetings" || got.BulkCodeReviewRepo != "exampleorg/team-reviews" ||
		!reflect.DeepEqual(got.BoardViewURLs, []string{"https://github.com/orgs/exampleorg/projects/4/views/5?visibleFields=Title"}) {
		t.Fatal("Settings must persist edited private destinations and pay")
	}
}

// Clean up starts off, saves a number of days, refuses anything else, and goes
// back off when emptied.
func TestSettingsCleanUpDays(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	cfg := backend.DefaultConfig()
	cfg.GithubAuthor = "sample-user"
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: a.NewWindow("settings"), Cfg: cfg}
	view := settings.BuildSettingsTab(ui)
	var field *widget.Entry
	var save *widget.Button
	walk(view, func(o fyne.CanvasObject) {
		if entry, ok := o.(*widget.Entry); ok && entry.PlaceHolder == "Blank keeps everything" {
			field = entry
		}
		if button, ok := o.(*widget.Button); ok && button.Text == "Save settings" {
			save = button
		}
	})
	if field == nil || save == nil || field.Text != "" {
		t.Fatal("the clean up field should be there and start blank")
	}
	field.SetText("14")
	save.OnTapped()
	if got, _ := ui.Store.LoadConfig(); got.CleanupDays != 14 {
		t.Fatalf("saved %d", got.CleanupDays)
	}
	for _, bad := range []string{"soon", "-3", "0"} {
		field.SetText(bad)
		save.OnTapped()
		if got, _ := ui.Store.LoadConfig(); got.CleanupDays != 14 {
			t.Fatalf("%q should be refused, saved %d", bad, got.CleanupDays)
		}
	}
	field.SetText("  ")
	save.OnTapped()
	if got, _ := ui.Store.LoadConfig(); got.CleanupDays != 0 {
		t.Fatalf("blank should turn clean up off, saved %d", got.CleanupDays)
	}
}
