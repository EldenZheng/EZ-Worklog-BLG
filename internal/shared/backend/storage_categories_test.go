package backend

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func storageFixture(t *testing.T) (*Store, Config, string, map[string][]byte) {
	t.Helper()
	cfg, archive := archiveFixture(t)
	store := NewStore(t.TempDir())
	files := map[string][]byte{
		"worklog.csv":                     []byte("id,pushed_at,remarks,extra\r\nlogged,2026-11-19,\"two\r\nlines\",keep-logged\r\ndraft,,draft notes,keep-draft\r\npartial,,retry link,keep-partial\r\n"),
		"ignored.json":                    []byte(`["one","two"]`),
		"ignored-dates.json":              []byte(`{"one":"2026-11-18","two":"2026-11-19"}`),
		"projects.json":                   []byte(`{"projects":[{"id":"a","name":"Example","issues":["org/repo#1"],"history":{"2026-11-09":{"review":1},"2026-11-16":{"progress":2}}}]}`),
		"sprint-planner.json":             []byte(`{"Tasks":{"one":{"Notes":"keep task"},"two":{"Estimate":60}},"PRIssues":{"pr":{"URL":"issue"}},"CalendarMode":"Month","future_setting":{"preserve":true}}`),
		SupportWeeksFile:                  []byte(`["2026-11-16"]`),
		"config.json":                     []byte(`{"keep":"settings"}`),
		"worklog.csv.pre-issue-title.bak": []byte("legacy worklog copy"),
		"unknown-user-file.txt":           []byte("never delete"),
	}
	for name, raw := range files {
		if err := os.WriteFile(filepath.Join(store.Dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(store.Dir, "exports", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "exports", "one.csv"), []byte("export one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "exports", "nested", "two.csv"), []byte("export two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveHistoryArchive(cfg, archive); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "Worklog.exe")
	if err := os.WriteFile(executable, []byte("app"), 0o600); err != nil {
		t.Fatal(err)
	}
	return store, cfg, executable, files
}

func TestStorageBreakdownPartitionsActualBytesAndCounts(t *testing.T) {
	store, cfg, exe, _ := storageFixture(t)
	usage, err := store.MeasureStorage(cfg, exe)
	if err != nil {
		t.Fatal(err)
	}
	total := int64(0)
	categories := map[StorageKind]StorageCategory{}
	for _, category := range usage.Categories {
		total += category.Bytes
		categories[category.Kind] = category
	}
	if total != usage.StoredDataBytes || total != usage.DataBytes+usage.ArchiveBytes {
		t.Fatalf("bytes double-counted or missed: sum=%d stored=%d data=%d archive=%d", total, usage.StoredDataBytes, usage.DataBytes, usage.ArchiveBytes)
	}
	for kind, want := range map[StorageKind]int{StorageLogged: 1, StorageDrafts: 2, StorageIgnored: 2, StorageProjectHistory: 2, StoragePlans: 2, StorageIssueCache: 1, StorageSupport: 1, StorageExports: 2, StorageHistory: 1, StorageLegacyBackup: 1} {
		if categories[kind].Count != want {
			t.Errorf("%s count=%d want=%d", kind, categories[kind].Count, want)
		}
	}
	if categories[StorageLogged].Bytes != int64(len("logged,2026-11-19,\"two\r\nlines\",keep-logged\r\n")) {
		t.Fatal("logged storage must use actual CSV record bytes")
	}
	if categories[StorageOther].CanClear {
		t.Fatal("settings and unknown files must not have a bulk-clear action")
	}
}

func TestClearWorklogCategoriesPreservesOtherRowsByteForByte(t *testing.T) {
	for _, kind := range []StorageKind{StorageLogged, StorageDrafts} {
		t.Run(string(kind), func(t *testing.T) {
			store, cfg, _, files := storageFixture(t)
			if err := store.ClearStorageCategory(cfg, kind); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(store.Dir, "worklog.csv"))
			if err != nil {
				t.Fatal(err)
			}
			want := "id,pushed_at,remarks,extra\r\n"
			if kind == StorageLogged {
				want += "draft,,draft notes,keep-draft\r\npartial,,retry link,keep-partial\r\n"
			} else {
				want += "logged,2026-11-19,\"two\r\nlines\",keep-logged\r\n"
			}
			if string(raw) != want {
				t.Fatalf("unselected CSV rows changed: %q", raw)
			}
			for name, want := range files {
				if name == "worklog.csv" {
					continue
				}
				got, err := os.ReadFile(filepath.Join(store.Dir, name))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("clearing worklogs changed %s: %q %v", name, got, err)
				}
			}
		})
	}
}

func TestClearPlannerCategoriesKeepsOtherSettingsAndData(t *testing.T) {
	for _, kind := range []StorageKind{StoragePlans, StorageIssueCache} {
		t.Run(string(kind), func(t *testing.T) {
			store, cfg, _, files := storageFixture(t)
			if err := store.ClearStorageCategory(cfg, kind); err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(filepath.Join(store.Dir, "sprint-planner.json"))
			var before, after map[string]json.RawMessage
			if err := json.Unmarshal(files["sprint-planner.json"], &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &after); err != nil {
				t.Fatal(err)
			}
			removed := "Tasks"
			if kind == StorageIssueCache {
				removed = "PRIssues"
			}
			if _, exists := after[removed]; exists {
				t.Fatal("selected planner category was not cleared")
			}
			for field, want := range before {
				if field == removed {
					continue
				}
				var w, g any
				json.Unmarshal(want, &w)
				json.Unmarshal(after[field], &g)
				wb, _ := json.Marshal(w)
				gb, _ := json.Marshal(g)
				if !bytes.Equal(wb, gb) {
					t.Fatalf("clearing %s changed %s", kind, field)
				}
			}
		})
	}
}

func TestClearIgnoredSupportAndLegacyBackupAreScoped(t *testing.T) {
	for _, kind := range []StorageKind{StorageIgnored, StorageSupport, StorageLegacyBackup} {
		t.Run(string(kind), func(t *testing.T) {
			store, cfg, _, files := storageFixture(t)
			if err := store.ClearStorageCategory(cfg, kind); err != nil {
				t.Fatal(err)
			}
			selected := map[string]bool{}
			switch kind {
			case StorageIgnored:
				selected["ignored.json"], selected["ignored-dates.json"] = true, true
			case StorageSupport:
				selected[SupportWeeksFile] = true
			case StorageLegacyBackup:
				selected["worklog.csv.pre-issue-title.bak"] = true
			}
			for name, want := range files {
				raw, err := os.ReadFile(filepath.Join(store.Dir, name))
				if selected[name] {
					if !os.IsNotExist(err) {
						t.Fatalf("%s was not removed", name)
					}
				} else if err != nil || !bytes.Equal(raw, want) {
					t.Fatalf("unselected %s changed", name)
				}
			}
		})
	}
}

func TestExportsClearKeepsArchiveAndFilesOutsideExports(t *testing.T) {
	store, cfg, exe, files := storageFixture(t)
	cfg.ArchiveDir = filepath.Join(store.Dir, "exports")
	_, archive := archiveFixture(t)
	if err := SaveHistoryArchive(cfg, archive); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(outside, filepath.Join(store.Dir, "exports", "linked.txt"))
	usage, err := store.MeasureStorage(cfg, exe)
	if err != nil {
		t.Fatal(err)
	}
	sum := int64(0)
	for _, category := range usage.Categories {
		sum += category.Bytes
	}
	if sum != usage.StoredDataBytes || sum != usage.DataBytes {
		t.Fatal("an archive inside the data folder was counted twice")
	}
	if err := store.ClearStorageCategory(cfg, StorageExports); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHistoryArchive(cfg); err != nil {
		t.Fatal("export clearing deleted the backup", err)
	}
	if raw, err := os.ReadFile(outside); err != nil || string(raw) != "outside" {
		t.Fatal("export clearing followed a symlink outside its folder")
	}
	for name, want := range files {
		raw, err := os.ReadFile(filepath.Join(store.Dir, name))
		if err != nil || !bytes.Equal(raw, want) {
			t.Fatalf("export clearing changed %s", name)
		}
	}
	remaining, _, err := storageExportFiles(store.Dir, HistoryArchivePath(cfg))
	if err != nil || len(remaining) != 0 {
		t.Fatalf("exports remain: %v %v", remaining, err)
	}
}

func TestStorageClearRejectsUnknownCategoriesAndMalformedCSV(t *testing.T) {
	store, cfg, _, files := storageFixture(t)
	if err := store.ClearStorageCategory(cfg, StorageOther); err == nil {
		t.Fatal("settings were allowed to be cleared")
	}
	if err := store.ClearStorageCategory(cfg, StorageKind("../../config.json")); err == nil {
		t.Fatal("arbitrary paths were accepted")
	}
	raw := []byte("id,pushed_at,remarks\na,,\"unfinished")
	if err := os.WriteFile(filepath.Join(store.Dir, "worklog.csv"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearStorageCategory(cfg, StorageDrafts); err == nil {
		t.Fatal("malformed CSV was overwritten")
	}
	got, _ := os.ReadFile(filepath.Join(store.Dir, "worklog.csv"))
	if !bytes.Equal(got, raw) {
		t.Fatal("failed CSV clear changed the file")
	}
	got, _ = os.ReadFile(filepath.Join(store.Dir, "config.json"))
	if !bytes.Equal(got, files["config.json"]) {
		t.Fatal("settings changed")
	}
}
