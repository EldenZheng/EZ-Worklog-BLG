package tests

import (
	"os"
	"path/filepath"
	"testing"

	"worklog/internal/shared/shell"
)

func TestDataDirPrefersDataFolderAndKeepsOldInstalls(t *testing.T) {
	fresh := t.TempDir()
	if got := shell.DataDirIn(fresh); got != filepath.Join(fresh, "data") {
		t.Fatalf("a new install should use a data folder: %s", got)
	}
	if info, err := os.Stat(filepath.Join(fresh, "data")); err != nil || !info.IsDir() {
		t.Fatal("the data folder was not created")
	}

	old := t.TempDir()
	if err := os.WriteFile(filepath.Join(old, "worklog.csv"), []byte("id\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := shell.DataDirIn(old); got != old {
		t.Fatalf("files beside the executable should still be read there: %s", got)
	}
	if _, err := os.Stat(filepath.Join(old, "data")); !os.IsNotExist(err) {
		t.Fatal("an old install must not get an empty data folder")
	}
}
