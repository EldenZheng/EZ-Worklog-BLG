package main

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestPrivateSettingsStartEmpty(t *testing.T) {
	cfg, _ := newStore(t.TempDir()).LoadConfig()
	if cfg.BaseSalary != 0 || cfg.SupportSetPay != 0 || cfg.MeetingRepo != "" ||
		cfg.BulkCodeReviewRepo != "" || len(cfg.BoardViewURLs) != 0 {
		t.Fatal("fresh installs must not inherit personal destinations or pay")
	}
	if links := weekBoardLinks(cfg, "", "2026-08-24", "2026-08-30"); len(links) != 0 {
		t.Fatalf("fresh installs must not link someone else's boards: %v", links)
	}
	if sets, bonus := weekendSupportBonus(4, 10, cfg.SupportSetPay); sets != 2 || bonus != 0 {
		t.Fatalf("unset pay must count sets without inventing payment: %d, %v", sets, bonus)
	}
	for _, ensure := range []func(Config, string, string) (string, error){ensureMeetingIssue, ensureCodeReviewIssue} {
		if _, err := ensure(Config{DisplayName: "Alex"}, "2026-08-24", "notes"); err == nil || !strings.Contains(err.Error(), "repository") {
			t.Fatalf("missing destination must fail before GitHub access: %v", err)
		}
	}
}

func TestConfiguredBoardLinksFallbackToOwnProjects(t *testing.T) {
	cfg := Config{ProjectURL: "https://github.com/orgs/exampleorg/projects/3/views/2?visibleFields=Title"}
	links := weekBoardLinks(cfg, "team-alex", "2026-08-24", "2026-08-30")
	if len(links) != 1 {
		t.Fatalf("expected configured project fallback, got %v", links)
	}
	u, err := url.Parse(links[0])
	if err != nil || u.Path != "/orgs/exampleorg/projects/3/views/2" || u.Query().Get("visibleFields") != "Title" ||
		u.Query().Get("filterQuery") != worklogProjectFilter("team-alex", "2026-08-24", "2026-08-30") {
		t.Fatalf("project view must survive with the requested owner and dates: %v", links)
	}
}

func TestPaySettingsRejectInvalidAmounts(t *testing.T) {
	for _, input := range []string{"-1", "NaN", "+Inf", "1e999", "abc"} {
		if _, err := parsePaySetting(input); err == nil {
			t.Errorf("invalid amount accepted: %q", input)
		}
	}
	for input, want := range map[string]float64{"": 0, "0": 0, " 123.45 ": 123.45} {
		if got, err := parsePaySetting(input); err != nil || got != want {
			t.Errorf("%q: got %v, %v", input, got, err)
		}
	}
}

func TestSettingsSavePrivateDestinationsAndPay(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	cfg := defaultConfig()
	cfg.GithubAuthor = "sample-user" // no profile detection is needed
	cfg.BaseSalary, cfg.SupportSetPay = 1234, 125
	cfg.MeetingRepo, cfg.BulkCodeReviewRepo = "exampleorg/meetings", "exampleorg/reviews"
	cfg.BoardViewURLs = []string{"https://github.com/orgs/exampleorg/projects/3/views/2"}
	ui := &UI{store: newStore(t.TempDir()), win: a.NewWindow("settings"), cfg: cfg}
	view := ui.buildSettingsTab()
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
	got, exists := ui.store.LoadConfig()
	if !exists || got.BaseSalary != 2345 || got.SupportSetPay != 187.5 ||
		got.MeetingRepo != "exampleorg/team-meetings" || got.BulkCodeReviewRepo != "exampleorg/team-reviews" ||
		!reflect.DeepEqual(got.BoardViewURLs, []string{"https://github.com/orgs/exampleorg/projects/4/views/5?visibleFields=Title"}) {
		t.Fatal("Settings must persist edited private destinations and pay")
	}
}
