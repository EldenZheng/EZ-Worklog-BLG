package backend

import "golang.org/x/sys/windows"

func diskSpace(path string) (free, total uint64, err error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	var allFree uint64
	err = windows.GetDiskFreeSpaceEx(pointer, &free, &total, &allFree)
	return
}
