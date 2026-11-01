package tests

import (
	"os"
	"testing"

	"worklog/internal/assemble"
)

// TestMain connects the menus to the shared code, as main does, before any
// test builds a window.
func TestMain(m *testing.M) {
	assemble.WireHooks()
	os.Exit(m.Run())
}
