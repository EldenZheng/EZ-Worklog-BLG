//go:build !windows

package backend

import "syscall"

func diskSpace(path string) (free, total uint64, err error) {
	var stats syscall.Statfs_t
	err = syscall.Statfs(path, &stats)
	if err == nil {
		free, total = uint64(stats.Bavail)*uint64(stats.Bsize), uint64(stats.Blocks)*uint64(stats.Bsize)
	}
	return
}
