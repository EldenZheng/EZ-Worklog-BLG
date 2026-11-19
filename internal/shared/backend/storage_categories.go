package backend

import (
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func isHistoryArchiveFile(name string) bool {
	if !strings.HasPrefix(name, "worklogs-") || !strings.HasSuffix(name, ".json.gz") {
		return false
	}
	key := strings.TrimSuffix(strings.TrimPrefix(name, "worklogs-"), ".json.gz")
	if len(key) != 24 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

type StorageKind string

const (
	StorageLogged         StorageKind = "logged"
	StorageDrafts         StorageKind = "drafts"
	StorageIgnored        StorageKind = "ignored"
	StorageProjectHistory StorageKind = "project-history"
	StoragePlans          StorageKind = "plans"
	StorageIssueCache     StorageKind = "issue-cache"
	StorageSupport        StorageKind = "support"
	StorageExports        StorageKind = "exports"
	StorageHistory        StorageKind = "history"
	StorageLegacyBackup   StorageKind = "legacy-backup"
	StorageOther          StorageKind = "other"
)

type StorageCategory struct {
	Kind                    StorageKind
	Name, Unit, Consequence string
	Bytes                   int64
	Count                   int
	CanClear                bool
}

type storageCSVRecord struct {
	start, end int64
	logged     bool
}

// CSV offsets keep each untouched row byte-for-byte, including multiline notes,
// column order and any columns written by a newer or older version.
func storageCSVRecords(raw []byte) ([]storageCSVRecord, error) {
	r := csv.NewReader(strings.NewReader(string(raw)))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pushed := -1
	for i, field := range header {
		if field == "pushed_at" {
			pushed = i
		}
	}
	var records []storageCSVRecord
	for {
		start := r.InputOffset()
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if pushed < 0 {
			return nil, fmt.Errorf("worklog.csv has no pushed_at column; its rows were kept unchanged")
		}
		records = append(records, storageCSVRecord{start: start, end: r.InputOffset(), logged: pushed < len(row) && strings.TrimSpace(row[pushed]) != ""})
	}
	return records, nil
}

func readStorageFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular app data file", filepath.Base(path))
	}
	return os.ReadFile(path)
}

func containedPath(root, path string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func sameStoragePath(a, b string) bool {
	a, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	b, err = filepath.Abs(b)
	return err == nil && strings.EqualFold(a, b)
}

func archiveInDataTree(root, path string) bool {
	if !containedPath(root, path) {
		return false
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	for {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if sameStoragePath(path, root) {
			return true
		}
		path = filepath.Dir(path)
	}
}

func storageExportFiles(dataDir, archive string) ([]string, int64, error) {
	root := filepath.Join(dataDir, "exports")
	var files []string
	var bytes int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		if sameStoragePath(path, archive) || isHistoryArchiveFile(entry.Name()) {
			return nil
		}
		if !containedPath(root, path) {
			return fmt.Errorf("export path is outside its folder")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			files = append(files, path)
			bytes += info.Size()
		}
		return nil
	})
	return files, bytes, err
}

func storageCategories(cfg Config, dir string, dataBytes, archiveBytes int64) ([]StorageCategory, int64, error) {
	categories := []StorageCategory{
		{Kind: StorageLogged, Name: "Logged worklogs", Unit: "entries", CanClear: true, Consequence: "Removes local pushed entries and their logged-commit markers. GitHub worklogs and the history backup stay. Matching commits can reappear in Pending after a refresh."},
		{Kind: StorageDrafts, Name: "Drafts & partial pushes", Unit: "entries", CanClear: true, Consequence: "Permanently discards local drafts and retry links for partially pushed entries. Drafts are not included in the GitHub history backup. Any work already created on GitHub stays there."},
		{Kind: StorageIgnored, Name: "Ignored commits", Unit: "commits", CanClear: true, Consequence: "Forgets the ignored list and ignore dates. These commits can appear in Pending again."},
		{Kind: StorageProjectHistory, Name: "Project report history", Unit: "weekly snapshots", CanClear: true, Consequence: "Deletes locally recorded weekly project figures. They are not included in the worklog backup and cannot be rebuilt from GitHub. Project names, issue lists and exclusions stay. Future refreshes record new history."},
		{Kind: StoragePlans, Name: "Sprint plans", Unit: "tasks", CanClear: true, Consequence: "Deletes local scheduled blocks, estimates, notes and task completion flags. GitHub issues and board filters stay."},
		{Kind: StorageIssueCache, Name: "Issue lookup cache", Unit: "lookups", CanClear: true, Consequence: "Forgets cached pull-request-to-issue links. The app will look them up again when needed."},
		{Kind: StorageSupport, Name: "Support placeholders", Unit: "weeks", CanClear: true, Consequence: "Clears the saved weekday-support placeholders. Already logged support work stays."},
		{Kind: StorageExports, Name: "Saved exports", Unit: "files", CanClear: true, Consequence: "Deletes files in the app's exports folder. The source records and history backup stay."},
		{Kind: StorageHistory, Name: "GitHub history backup", Unit: "files", CanClear: true, Consequence: "Deletes this owner's local GitHub history backup. Historical reports will need GitHub again. Drafts, settings and GitHub records stay."},
		{Kind: StorageLegacyBackup, Name: "Legacy worklog backup", Unit: "files", CanClear: true, Consequence: "Deletes the old worklog.csv.pre-issue-title.bak migration copy. The current worklog file and GitHub history backup stay."},
		{Kind: StorageOther, Name: "Settings & other data", Unit: "", CanClear: false},
	}
	byKind := map[StorageKind]*StorageCategory{}
	for i := range categories {
		byKind[categories[i].Kind] = &categories[i]
	}
	read := func(name string) ([]byte, error) { return readStorageFile(filepath.Join(dir, name)) }
	raw, err := read("worklog.csv")
	if err != nil {
		return nil, 0, err
	}
	rows, err := storageCSVRecords(raw)
	if err != nil {
		return nil, 0, err
	}
	for _, row := range rows {
		kind := StorageDrafts
		if row.logged {
			kind = StorageLogged
		}
		category := byKind[kind]
		category.Bytes += row.end - row.start
		category.Count++
	}
	for _, name := range []string{"ignored.json", "ignored-dates.json"} {
		raw, err = read(name)
		if err != nil {
			return nil, 0, err
		}
		byKind[StorageIgnored].Bytes += int64(len(raw))
		if name == "ignored.json" && len(raw) > 0 {
			var shas []string
			if err := json.Unmarshal(raw, &shas); err != nil {
				return nil, 0, err
			}
			unique := map[string]bool{}
			for _, sha := range shas {
				if sha = strings.TrimSpace(sha); sha != "" {
					unique[sha] = true
				}
			}
			byKind[StorageIgnored].Count = len(unique)
		}
	}
	raw, err = read("projects.json")
	if err != nil {
		return nil, 0, err
	}
	if len(raw) > 0 {
		var saved struct {
			Projects []struct{ History json.RawMessage }
		}
		if err := json.Unmarshal(raw, &saved); err != nil {
			return nil, 0, err
		}
		for _, project := range saved.Projects {
			var weeks map[string]json.RawMessage
			if len(project.History) > 0 {
				if err := json.Unmarshal(project.History, &weeks); err != nil {
					return nil, 0, err
				}
			}
			if len(weeks) > 0 {
				byKind[StorageProjectHistory].Bytes += int64(len(project.History))
				byKind[StorageProjectHistory].Count += len(weeks)
			}
		}
	}
	raw, err = read("sprint-planner.json")
	if err != nil {
		return nil, 0, err
	}
	if len(raw) > 0 {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, 0, err
		}
		for field, kind := range map[string]StorageKind{"Tasks": StoragePlans, "PRIssues": StorageIssueCache} {
			var values map[string]json.RawMessage
			if len(fields[field]) > 0 {
				if err := json.Unmarshal(fields[field], &values); err != nil {
					return nil, 0, err
				}
			}
			if len(values) > 0 {
				byKind[kind].Bytes = int64(len(fields[field]))
				byKind[kind].Count = len(values)
			}
		}
	}
	raw, err = read(SupportWeeksFile)
	if err != nil {
		return nil, 0, err
	}
	byKind[StorageSupport].Bytes = int64(len(raw))
	if len(raw) > 0 {
		var weeks []string
		if err := json.Unmarshal(raw, &weeks); err != nil {
			return nil, 0, err
		}
		byKind[StorageSupport].Count = len(weeks)
	}
	files, exportBytes, err := storageExportFiles(dir, HistoryArchivePath(cfg))
	if err != nil {
		return nil, 0, err
	}
	byKind[StorageExports].Bytes, byKind[StorageExports].Count = exportBytes, len(files)
	byKind[StorageHistory].Bytes = archiveBytes
	if archiveBytes > 0 {
		byKind[StorageHistory].Count = 1
	}
	if info, err := os.Lstat(filepath.Join(dir, "worklog.csv.pre-issue-title.bak")); err == nil {
		if !info.Mode().IsRegular() {
			return nil, 0, fmt.Errorf("legacy backup is not a regular file")
		}
		byKind[StorageLegacyBackup].Bytes = info.Size()
		byKind[StorageLegacyBackup].Count = 1
	} else if !os.IsNotExist(err) {
		return nil, 0, err
	}
	known := int64(0)
	for _, category := range categories {
		if category.Kind != StorageHistory {
			known += category.Bytes
		}
	}
	archiveInside := archiveInDataTree(dir, HistoryArchivePath(cfg))
	if archiveInside {
		known += archiveBytes
	}
	byKind[StorageOther].Bytes = max(0, dataBytes-known)
	total := dataBytes
	if !archiveInside {
		total += archiveBytes
	}
	return categories, total, nil
}
