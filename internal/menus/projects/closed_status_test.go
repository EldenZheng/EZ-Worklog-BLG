package projects

import "testing"

// The board Status wins over GitHub's closed state: work still going keeps a
// closed issue open, and a Status that ends work keeps it closed.
func TestClosedIssueFollowsItsBoardStatus(t *testing.T) {
	for _, c := range []struct {
		status     string
		notPlanned bool
		closed     bool
	}{
		{"Server Update Required", false, false},
		{"In Progress", false, false},
		{"Pending Test", false, false},
		{"In Review", false, false},
		{"Done", false, true},
		{"Duplicated", false, true},
		{"Request Denied", false, true},
		{"", false, true},
		{"In Progress", true, true}, // closed as not planned on GitHub
	} {
		it := Item{ClosedOnGitHub: true, NotPlanned: c.notPlanned, Status: c.status}
		it.SettleClosed()
		if it.Closed != c.closed {
			t.Errorf("closed issue with Status %q (not planned %v): Closed = %v, want %v", c.status, c.notPlanned, it.Closed, c.closed)
		}
	}
	open := Item{Status: "Done"}
	open.SettleClosed()
	if open.Closed {
		t.Error("an issue open on GitHub stays open")
	}
}

// Changing a closed issue's Status from the page opens or closes it again.
func TestStatusChangeSettlesClosed(t *testing.T) {
	snap := Snapshot{Parents: []Parent{{Items: []Item{{URL: "u", ClosedOnGitHub: true, Status: "Done", Closed: true}}}}}
	snap.Patch("u", func(it *Item) { it.Status = "Server Update Required" })
	if snap.Parents[0].Items[0].Closed {
		t.Fatal("moving a closed issue back to a working Status should open it")
	}
	snap.Patch("u", func(it *Item) { it.Status = "Done" })
	if !snap.Parents[0].Items[0].Closed {
		t.Fatal("moving it to Done should close it again")
	}
}
