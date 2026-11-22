package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestLocalGitReadsShareSmallWorkerBudgetAcrossScans(t *testing.T) {
	limit := localScanLimit()
	if limit < 1 || limit > 2 || (runtime.NumCPU() <= 2 && limit != 1) {
		t.Fatalf("local scan budget must leave CPU room for the UI: %d workers on %d CPUs", limit, runtime.NumCPU())
	}
	root := t.TempDir()
	const clones = 8
	for i := 0; i < clones; i++ {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("repo-%d", i), ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldGit := Git
	started, release := make(chan struct{}, 64), make(chan struct{})
	results := make(chan []Commit, 2)
	var active, peak, logs atomic.Int32
	Git = func(dir string, _ []string, _ string, args ...string) (string, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); n > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		switch {
		case args[0] == "log":
			logs.Add(1)
			return "0123456789abcdef\x1ffeat\x1f2026-11-23T00:00:00Z\x1fLocal work\x1e", nil
		case args[1] == "user.email":
			return "user@example.com", nil
		default:
			return "https://github.com/scan-budget-tests/" + filepath.Base(dir) + ".git", nil
		}
	}
	// Always let the mocked commands finish before restoring the Git function.
	defer func() {
		close(release)
		for range 2 {
			if got := <-results; len(got) != clones {
				t.Errorf("scan returned %d commits, want %d", len(got), clones)
			}
		}
		Git = oldGit
		if peak.Load() > int32(limit) {
			t.Errorf("overlapping scans ran %d Git reads at once; budget is %d", peak.Load(), limit)
		}
		if logs.Load() != 2*clones {
			t.Errorf("worker limit skipped log reads: %d", logs.Load())
		}
	}()
	cfg := Config{RepoFolders: []string{root}, Repos: []string{"scan-budget-tests"}}
	for range 2 {
		go func() { results <- LocalCommits(cfg, "2000-01-01T00:00:00Z", "2100-01-01T00:00:00Z") }()
	}
	for range limit {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("local scan workers did not start")
		}
	}
	select {
	case <-started:
		t.Error("another Git read started while the shared worker budget was full")
	case <-time.After(30 * time.Millisecond):
	}
}
