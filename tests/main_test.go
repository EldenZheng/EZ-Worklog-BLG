package tests

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"worklog/internal/assemble"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// liveGitHub is set by the live probes (go test -tags live), the one place
// the tests are meant to reach GitHub.
var liveGitHub = false

// TestMain connects the menus to the shared code, as main does, before any
// test builds a window.
//
// It also makes the suite run in step. Background work finishes before the
// call that started it returns, rather than drawing from another goroutine
// while the next test draws: Fyne's text shaper crashes when two goroutines
// use it at once, which failed a different test on every run. And nothing
// reaches the network: a test that needs GitHub or the exchange rate stands in
// for it, and anything that does not falls back to the offline error here.
func TestMain(m *testing.M) {
	assemble.WireHooks()
	shell.RunAsyncInline = true
	if !liveGitHub {
		backend.Gh = func(args []string, _ ...string) (string, error) {
			// Every Log Work tab lists the configured org's repos for its
			// Independent picker. Answer with none, rather than an error
			// dialog over whatever the test is about to check.
			if len(args) > 1 && args[0] == "api" && strings.HasSuffix(args[1], "/repos") {
				return "", nil
			}
			return "", backend.GhErr("GitHub is offline in tests")
		}
		backend.FetchRate = func(string, string) (float64, error) {
			return 0, errors.New("the exchange rate is offline in tests")
		}
	}
	os.Exit(m.Run())
}

// tempDir is t.TempDir with a cleanup that waits out Windows. Right after the
// planner is saved, something on Windows (Defender scanning the new file, as a
// rule) can hold it open for a moment; t.TempDir then cannot delete the folder
// and fails a test that passed. This retries for a few seconds and, if the
// folder is still held, leaves it in the system temp folder rather than fail.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "worklog-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		for os.RemoveAll(dir) != nil && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
	})
	return dir
}
