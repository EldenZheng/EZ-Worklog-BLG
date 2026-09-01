package main

import "testing"

// The meeting issue is titled the way the ones already on the board are titled,
// and the name comes off the worklog owner unless it is spelled out.
func TestMeetingTitleAndDisplayName(t *testing.T) {
	for _, c := range []struct {
		cfg  Config
		want string
	}{
		{Config{WorklogOwner: "blg-elden"}, "Elden"},
		{Config{WorklogOwner: "blg_elden"}, "Elden"},
		{Config{WorklogOwner: "elden"}, "Elden"},
		// Set explicitly for anyone whose handle is not their name.
		{Config{WorklogOwner: "blg-elden", DisplayName: "Elden Zheng"}, "Elden Zheng"},
		{Config{DisplayName: "  Elden  "}, "Elden"},
		// Nothing to go on: the caller must refuse rather than title an issue
		// " Meeting & ad hocs: …".
		{Config{}, ""},
	} {
		if got := displayName(c.cfg); got != c.want {
			t.Fatalf("displayName(%+v) = %q, want %q", c.cfg, got, c.want)
		}
	}

	cfg := Config{WorklogOwner: "blg-elden"}
	// The exact shape of the issues already filed — #9509 is
	// "Elden Meeting & ad hocs: 2026-08-25".
	if got := meetingTitle(cfg, "2026-08-25"); got != "Elden Meeting & ad hocs: 2026-08-25" {
		t.Fatalf("meeting title = %q", got)
	}
}

// An issue is pasted as often as it is typed, and both are readable.
func TestIssueRefFromAnyTakesLinksOrRefs(t *testing.T) {
	for _, c := range []struct {
		in, want string
	}{
		{"https://github.com/bigledger/blg-intranet/issues/5760", "bigledger/blg-intranet#5760"},
		{"  https://github.com/BigLedger-Support/tuhu-finance/issues/48  ", "BigLedger-Support/tuhu-finance#48"},
		{"bigledger/blg-intranet#5760", "bigledger/blg-intranet#5760"},
	} {
		got, err := issueRefFromAny(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("%q = %q, want %q", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "   ", "just some words", "bigledger/blg-intranet", "#123"} {
		if _, err := issueRefFromAny(bad); err == nil {
			t.Fatalf("%q should not be accepted as an issue", bad)
		}
	}
}

// refFromIssueURL is what turns a created issue back into a ref, so it has to
// cope with the URL forms gh actually prints.
func TestRefFromIssueURL(t *testing.T) {
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"https://github.com/bigledger/blg-int-general-task/issues/9509",
			"bigledger/blg-int-general-task#9509", true},
		{"https://github.com/o/r/pull/12", "o/r#12", true}, // a PR is still numbered
		{"https://github.com/o/r/issues", "", false},
		{"not a url", "", false},
	} {
		got, err := refFromIssueURL(c.in)
		if c.ok && (err != nil || got != c.want) {
			t.Fatalf("%q = (%q, %v), want %q", c.in, got, err, c.want)
		}
		if !c.ok && err == nil {
			t.Fatalf("%q should not parse, got %q", c.in, got)
		}
	}
}

// A row whose issue is decided at push time still has to look pushable, or the
// button is greyed out on the two kinds built to work that way.
func TestPushableWithoutIssue(t *testing.T) {
	for _, c := range []struct {
		name string
		row  Row
		want bool
	}{
		{"meeting", Row{"type": kindMeeting}, true},
		{"independent with a parent named",
			Row{"type": kindIndependent, "parent_repo": "o/r", "parent_title": "Tidy up"}, true},
		{"independent with no title", Row{"type": kindIndependent, "parent_repo": "o/r"}, false},
		{"independent with no repo", Row{"type": kindIndependent, "parent_title": "Tidy up"}, false},
		{"other", Row{"type": kindOther}, false},
		{"commit", Row{"type": kindCommit}, false},
	} {
		if got := pushableWithoutIssue(c.row); got != c.want {
			t.Fatalf("%s: pushableWithoutIssue = %v, want %v", c.name, got, c.want)
		}
	}
}

// ensureIssueRef decides what a row needs before it can be pushed. It must not
// go near GitHub for the cases that are already settled, or that cannot be.
func TestEnsureIssueRefLeavesSettledRowsAlone(t *testing.T) {
	cfg := Config{WorklogOwner: "blg-elden"}

	// Already filed: nothing to do, whatever kind it is.
	for _, r := range []Row{
		{"type": kindMeeting, "issue": "bigledger/blg-int-general-task#9509"},
		{"type": kindIndependent, "issue": "o/r#1", "parent_repo": "o/r", "parent_title": "x"},
		{"type": kindCommit, "issue": "o/r#1"},
	} {
		patch, err := ensureIssueRef(cfg, r)
		if err != nil || len(patch) != 0 {
			t.Fatalf("%v: expected no work, got patch=%v err=%v", r, patch, err)
		}
	}

	// A commit or an "other" with no ref is simply unfiled — not something to
	// invent an issue for.
	for _, r := range []Row{{"type": kindCommit}, {"type": kindOther}} {
		patch, err := ensureIssueRef(cfg, r)
		if err != nil || len(patch) != 0 {
			t.Fatalf("%v: expected no work, got patch=%v err=%v", r, patch, err)
		}
	}

	// An independent entry that names nothing has nothing to create, and says so
	// rather than opening an issue called "".
	if _, err := ensureIssueRef(cfg, Row{"type": kindIndependent}); err == nil {
		t.Fatal("an independent entry with no parent named should be refused")
	}

	// A meeting with no name to title the issue with is refused before it can
	// create "  Meeting & ad hocs: 2026-08-30".
	if _, err := ensureIssueRef(Config{}, Row{"type": kindMeeting, "date": "2026-08-30"}); err == nil {
		t.Fatal("a meeting with no display name should be refused")
	}
}
