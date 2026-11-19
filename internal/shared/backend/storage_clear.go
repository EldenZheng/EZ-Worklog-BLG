package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func writeStorageFile(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".storage-clear-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// ClearStorageCategory touches only the selected app-owned category. No GitHub
// calls, recursive directory removal, or general-purpose data-folder cleanup.
func (s *Store) ClearStorageCategory(cfg Config, kind StorageKind) error {
	if kind == StorageHistory {
		return ClearHistoryArchive(cfg)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	remove := func(name string) error {
		path := filepath.Join(s.Dir, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular app data file", name)
		}
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	switch kind {
	case StorageLogged, StorageDrafts:
		raw, err := readStorageFile(s.logf)
		if err != nil || len(raw) == 0 {
			return err
		}
		records, err := storageCSVRecords(raw)
		if err != nil {
			return err
		}
		kept := make([]byte, 0, len(raw))
		cursor := int64(0)
		for _, row := range records {
			kept = append(kept, raw[cursor:row.start]...)
			if row.logged != (kind == StorageLogged) {
				kept = append(kept, raw[row.start:row.end]...)
			}
			cursor = row.end
		}
		kept = append(kept, raw[cursor:]...)
		return writeStorageFile(s.logf, kept)
	case StorageIgnored:
		if err := remove("ignored-dates.json"); err != nil {
			return err
		}
		return remove("ignored.json")
	case StorageSupport:
		return remove(SupportWeeksFile)
	case StorageLegacyBackup:
		return remove("worklog.csv.pre-issue-title.bak")
	case StoragePlans, StorageIssueCache:
		path := filepath.Join(s.Dir, "sprint-planner.json")
		raw, err := readStorageFile(path)
		if err != nil || len(raw) == 0 {
			return err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		field := "Tasks"
		if kind == StorageIssueCache {
			field = "PRIssues"
		}
		delete(fields, field)
		raw, err = json.MarshalIndent(fields, "", "  ")
		if err != nil {
			return err
		}
		return writeStorageFile(path, append(raw, '\n'))
	case StorageExports:
		files, _, err := storageExportFiles(s.Dir, HistoryArchivePath(cfg))
		if err != nil {
			return err
		}
		for _, path := range files {
			if !containedPath(filepath.Join(s.Dir, "exports"), path) {
				return fmt.Errorf("export is outside its folder")
			}
			info, err := os.Lstat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("export is not a regular file")
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("this storage category cannot be cleared here")
	}
}
