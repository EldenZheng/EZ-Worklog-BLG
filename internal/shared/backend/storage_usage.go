package backend

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type StorageUsage struct {
	AppBytes, DataBytes, ArchiveBytes int64
	FreeBytes, TotalBytes             uint64
	ArchiveFreeBytes                  uint64
	Categories                        []StorageCategory
	StoredDataBytes                   int64
}

func (s *Store) MeasureStorage(cfg Config, executable string) (StorageUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return StorageForApp(cfg, executable, s.Dir)
}

func directoryBytes(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) && path == dir {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

func StorageForApp(cfg Config, executable, dataDir string) (StorageUsage, error) {
	var usage StorageUsage
	if info, err := os.Stat(executable); err == nil {
		usage.AppBytes = info.Size()
	} else {
		return usage, err
	}
	var err error
	if usage.DataBytes, err = directoryBytes(dataDir); err != nil {
		return usage, err
	}
	if info, err := os.Stat(HistoryArchivePath(cfg)); err == nil {
		usage.ArchiveBytes = info.Size()
	} else if !os.IsNotExist(err) {
		return usage, err
	}
	usage.FreeBytes, usage.TotalBytes, err = DiskSpace(filepath.Dir(executable))
	if err != nil {
		return usage, err
	}
	usage.ArchiveFreeBytes, _, err = DiskSpace(HistoryDirectory(cfg))
	if err != nil {
		return usage, err
	}
	usage.Categories, usage.StoredDataBytes, err = storageCategories(cfg, dataDir, usage.DataBytes, usage.ArchiveBytes)
	return usage, err
}

// DiskSpace measures the nearest existing ancestor, including before the
// user has created the selected history folder.
func DiskSpace(path string) (free, total uint64, err error) {
	path, err = filepath.Abs(path)
	if err != nil {
		return
	}
	for {
		if _, statErr := os.Stat(path); statErr == nil {
			return diskSpace(path)
		} else if !os.IsNotExist(statErr) {
			return 0, 0, statErr
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, 0, fmt.Errorf("cannot find a disk for %s", path)
		}
		path = parent
	}
}

func FormatBytes(size uint64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	for _, unit := range []string{"KiB", "MiB", "GiB", "TiB", "PiB"} {
		value /= 1024
		if value < 1024 || unit == "PiB" {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return ""
}
