package backend

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const HistoryStartDate = "2025-07-01"
const HistoryCutoff = "2026-11-20"

type HistoryArchive struct {
	Version   int           `json:"version"`
	Owner     string        `json:"owner"`
	Projects  []string      `json:"projects"`
	Through   string        `json:"through"`
	From      string        `json:"from,omitempty"`
	FetchedAt string        `json:"fetched_at"`
	Items     []WorklogItem `json:"items"`
}

var historyMu sync.Mutex

func HistoryDirectory(cfg Config) string {
	if strings.TrimSpace(cfg.ArchiveDir) != "" {
		return filepath.Clean(cfg.ArchiveDir)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base, _ = os.UserHomeDir()
	}
	return filepath.Join(base, "EZ-Worklog", "history")
}

func historyProjects(cfg Config) []string {
	var keys []string
	for _, raw := range ProjectURLs(cfg) {
		if ref, err := ParseProjectRef(raw); err == nil {
			keys = append(keys, worklogFieldsKey(ref))
		}
	}
	sort.Strings(keys)
	return keys
}

func HistoryArchivePath(cfg Config) string {
	key := strings.ToLower(strings.TrimSpace(cfg.WorklogOwner)) + "\n" + strings.Join(historyProjects(cfg), "\n")
	hash := sha256.Sum256([]byte(key))
	return filepath.Join(HistoryDirectory(cfg), "worklogs-"+hex.EncodeToString(hash[:12])+".json.gz")
}

// FetchHistoryArchive is read-only on GitHub. A failed board never produces a
// partial archive, and today's fetch cannot claim to include tomorrow's work.
func FetchHistoryArchive(cfg Config, progress ...func(FetchProgress)) (HistoryArchive, error) {
	if strings.TrimSpace(cfg.WorklogOwner) == "" {
		return HistoryArchive{}, GhErr("Set your Worklog owner in Settings before backing up history.")
	}
	through := min(WorkDate(time.Now()), HistoryCutoff)
	items, err := FetchProjectWorklogs(cfg, cfg.WorklogOwner, HistoryStartDate, through, progress...)
	if err != nil {
		return HistoryArchive{}, err
	}
	for _, it := range items {
		if !ValidPlannerDate(it.Date) || it.Date < HistoryStartDate || it.Date > through || !strings.EqualFold(strings.TrimSpace(it.Owner), strings.TrimSpace(cfg.WorklogOwner)) {
			return HistoryArchive{}, GhErr("GitHub returned a worklog outside the history backup's owner or date range.")
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Date == items[j].Date {
			return items[i].URL < items[j].URL
		}
		return items[i].Date < items[j].Date
	})
	return HistoryArchive{Version: 1, Owner: strings.TrimSpace(cfg.WorklogOwner), Projects: historyProjects(cfg), From: HistoryStartDate, Through: through, FetchedAt: time.Now().UTC().Format(time.RFC3339), Items: items}, nil
}

func validateHistory(cfg Config, a HistoryArchive) error {
	if a.From != "" && (!ValidPlannerDate(a.From) || a.From > a.Through) {
		return GhErr("The history backup has an invalid start date.")
	}
	if a.Version != 1 || !strings.EqualFold(a.Owner, strings.TrimSpace(cfg.WorklogOwner)) || strings.Join(a.Projects, "\n") != strings.Join(historyProjects(cfg), "\n") || !ValidPlannerDate(a.Through) || a.Through > HistoryCutoff {
		return GhErr("This history backup does not match the configured owner, boards or cutoff.")
	}
	for _, it := range a.Items {
		if !ValidPlannerDate(it.Date) || it.Date < historyFrom(a) || it.Date > a.Through || !strings.EqualFold(strings.TrimSpace(it.Owner), a.Owner) {
			return GhErr("The history backup contains an invalid worklog.")
		}
	}
	return nil
}

func LoadHistoryArchive(cfg Config) (HistoryArchive, error) {
	historyMu.Lock()
	defer historyMu.Unlock()
	return loadHistoryArchive(cfg)
}

func loadHistoryArchive(cfg Config) (HistoryArchive, error) {
	f, err := os.Open(HistoryArchivePath(cfg))
	if err != nil {
		return HistoryArchive{}, err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return HistoryArchive{}, err
	}
	defer z.Close()
	var a HistoryArchive
	if err := json.NewDecoder(z).Decode(&a); err != nil {
		return a, err
	}
	// Consume the trailer too: gzip reports a damaged checksum at EOF.
	if _, err := io.Copy(io.Discard, z); err != nil {
		return a, err
	}
	return a, validateHistory(cfg, a)
}

func SaveHistoryArchive(cfg Config, a HistoryArchive) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	if err := validateHistory(cfg, a); err != nil {
		return err
	}
	// Refreshes retain records already backed up if the old board fields have
	// since disappeared under the new SOP. A clear is always an explicit action.
	previous, err := loadHistoryArchive(cfg)
	if err == nil {
		if historyFrom(previous) < historyFrom(a) {
			a.From = previous.From
		}
		byURL := map[string]int{}
		for i, it := range a.Items {
			byURL[historyItemKey(it)] = i
		}
		for _, it := range previous.Items {
			key := historyItemKey(it)
			if _, exists := byURL[key]; !exists {
				byURL[key] = len(a.Items)
				a.Items = append(a.Items, it)
			}
		}
		a.Through = max(a.Through, previous.Through)
		sort.SliceStable(a.Items, func(i, j int) bool {
			if a.Items[i].Date == a.Items[j].Date {
				return a.Items[i].URL < a.Items[j].URL
			}
			return a.Items[i].Date < a.Items[j].Date
		})
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("existing history backup could not be read; it was kept unchanged: %w", err)
	}
	if err := os.MkdirAll(HistoryDirectory(cfg), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(HistoryDirectory(cfg), ".history-write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	z := gzip.NewWriter(f)
	if err := json.NewEncoder(z).Encode(a); err != nil {
		z.Close()
		f.Close()
		return err
	}
	if err := z.Close(); err != nil {
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
	return os.Rename(name, HistoryArchivePath(cfg))
}

func historyItemKey(it WorklogItem) string {
	if it.URL != "" {
		return it.URL
	}
	if it.ID != "" {
		return "id:" + it.ID
	}
	raw, _ := json.Marshal(it)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// Clear removes only this owner's app-owned archive file, never a directory,
// local worklogs, configuration, exports or anything on GitHub.
func ClearHistoryArchive(cfg Config) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	err := os.Remove(HistoryArchivePath(cfg))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// CopyHistoryArchive verifies the new copy before callers save the new location
// and clear the old file. Keeping that order preserves history if saving fails.
func CopyHistoryArchive(cfg Config, destination string) (Config, error) {
	if !filepath.IsAbs(destination) {
		return cfg, GhErr("Choose an absolute folder path for the history backup.")
	}
	next := cfg
	next.ArchiveDir = filepath.Clean(destination)
	if strings.EqualFold(HistoryArchivePath(cfg), HistoryArchivePath(next)) {
		return next, nil
	}
	if _, err := os.Lstat(HistoryArchivePath(next)); err == nil {
		return cfg, GhErr("That folder already contains this history backup. Choose another folder.")
	} else if !os.IsNotExist(err) {
		return cfg, err
	}
	a, err := LoadHistoryArchive(cfg)
	if os.IsNotExist(err) {
		return next, nil
	}
	if err != nil {
		return cfg, err
	}
	info, err := os.Stat(HistoryArchivePath(cfg))
	if err != nil {
		return cfg, err
	}
	free, _, err := DiskSpace(destination)
	if err != nil {
		return cfg, err
	}
	if uint64(info.Size()) > free {
		return cfg, GhErr("The destination has %s free; the backup needs %s.", FormatBytes(free), FormatBytes(uint64(info.Size())))
	}
	if err := SaveHistoryArchive(next, a); err != nil {
		return cfg, err
	}
	if _, err := LoadHistoryArchive(next); err != nil {
		_ = ClearHistoryArchive(next)
		return cfg, err
	}
	return next, nil
}

// Historical ranges remain available after the SOP changes. Only the portion
// not covered by the completed backup is requested from GitHub.
func FetchWorklogsWithHistory(cfg Config, from, to string, progress ...func(FetchProgress)) ([]WorklogItem, error) {
	if !ValidPlannerDate(from) || !ValidPlannerDate(to) || to < from {
		return nil, GhErr("Invalid worklog date range.")
	}
	a, err := LoadHistoryArchive(cfg)
	if os.IsNotExist(err) {
		return FetchProjectWorklogs(cfg, cfg.WorklogOwner, from, to, progress...)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read history backup: %w", err)
	}
	var items []WorklogItem
	if from < historyFrom(a) {
		older, err := FetchProjectWorklogs(cfg, cfg.WorklogOwner, from, min(to, AddDays(historyFrom(a), -1)), progress...)
		if err != nil {
			return nil, err
		}
		items = older
	}
	// Today's records can still change after a backup was fetched. Keep that
	// day live until it closes; completed history stays available offline.
	through := min(a.Through, AddDays(WorkDate(time.Now()), -1))
	for _, it := range a.Items {
		if it.Date >= from && it.Date <= min(to, through) {
			items = append(items, it)
		}
	}
	if to <= through {
		return items, nil
	}
	liveFrom := max(from, AddDays(through, 1))
	live, err := FetchProjectWorklogs(cfg, cfg.WorklogOwner, liveFrom, to, progress...)
	if err != nil {
		return nil, err
	}
	return append(items, live...), nil
}

// A backdated push updates its archived day too, so a completed backup never
// hides work subsequently added or edited through the app.
func RefreshHistoryDate(cfg Config, date string) error {
	a, err := LoadHistoryArchive(cfg)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if date > a.Through || date > HistoryCutoff {
		return nil
	}
	if date < historyFrom(a) {
		return nil
	}
	items, err := FetchProjectWorklogs(cfg, cfg.WorklogOwner, date, date)
	if err != nil {
		return err
	}
	a.Items = items
	return SaveHistoryArchive(cfg, a)
}

func historyFrom(a HistoryArchive) string {
	// Backups written before the explicit July start scanned all history.
	if a.From == "" {
		return "0001-01-01"
	}
	return a.From
}
