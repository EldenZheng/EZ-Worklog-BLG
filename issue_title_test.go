package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateIssueTitlesMigratesOldRowsAndKeepsBackup(t *testing.T) {
	dir := t.TempDir()
	oldHeader := fieldOrder[:len(fieldOrder)-1]
	path := filepath.Join(dir, "worklog.csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	if err := w.Write(oldHeader); err != nil {
		t.Fatal(err)
	}
	row := make([]string, len(oldHeader))
	for i, field := range oldHeader {
		switch field {
		case "id":
			row[i] = "row-1"
		case "issue":
			row[i] = "BigLedger/repo#7"
		case "remarks":
			row[i] = "keep me"
		}
	}
	if err := w.Write(row); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	store := newStore(dir)
	changed, err := store.UpdateIssueTitles(map[string]string{
		"bigledger/repo#7": "Fix invoice rounding",
	})
	if err != nil || changed != 1 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	rows, err := store.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[0]["issue_title"]; got != "Fix invoice rounding" {
		t.Fatalf("issue_title=%q", got)
	}
	if got := rows[0]["remarks"]; got != "keep me" {
		t.Fatalf("remarks changed to %q", got)
	}
	backup, err := os.ReadFile(path + ".pre-issue-title.bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(original) {
		t.Fatal("migration backup does not match the pre-migration CSV")
	}

	changed, err = store.UpdateIssueTitles(map[string]string{
		"bigledger/repo#7": "A newer title must not overwrite the saved one",
	})
	if err != nil || changed != 0 {
		t.Fatalf("idempotent retry changed=%d err=%v", changed, err)
	}
}
