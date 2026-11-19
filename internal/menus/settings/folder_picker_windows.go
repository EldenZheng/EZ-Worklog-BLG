package settings

/*
#cgo LDFLAGS: -lole32 -lshell32 -luuid
#define COBJMACROS
#include <windows.h>
#include <shobjidl.h>
#include <stdint.h>

// The native Common Item Dialog is the Explorer-style folder selector.
static HRESULT pick_backup_folder(uintptr_t owner, const wchar_t *initial, wchar_t **path) {
    HRESULT hr = CoInitializeEx(NULL, COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE);
    if (FAILED(hr)) return hr;
    IFileOpenDialog *picker = NULL;
    hr = CoCreateInstance(&CLSID_FileOpenDialog, NULL, CLSCTX_INPROC_SERVER,
                          &IID_IFileOpenDialog, (void **)&picker);
    if (SUCCEEDED(hr)) {
        DWORD options = 0;
        hr = IFileOpenDialog_GetOptions(picker, &options);
        if (SUCCEEDED(hr)) hr = IFileOpenDialog_SetOptions(picker,
            options | FOS_PICKFOLDERS | FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST | FOS_DONTADDTORECENT);
        if (SUCCEEDED(hr)) hr = IFileOpenDialog_SetTitle(picker, L"Choose history backup folder");
        if (SUCCEEDED(hr) && initial && initial[0]) {
            IShellItem *folder = NULL;
            if (SUCCEEDED(SHCreateItemFromParsingName(initial, NULL, &IID_IShellItem, (void **)&folder))) {
                IFileOpenDialog_SetFolder(picker, folder);
                IShellItem_Release(folder);
            }
        }
        if (SUCCEEDED(hr)) hr = IFileOpenDialog_Show(picker, (HWND)owner);
        if (SUCCEEDED(hr)) {
            IShellItem *result = NULL;
            hr = IFileOpenDialog_GetResult(picker, &result);
            if (SUCCEEDED(hr)) {
                hr = IShellItem_GetDisplayName(result, SIGDN_FILESYSPATH, path);
                IShellItem_Release(result);
            }
        }
        IFileOpenDialog_Release(picker);
    }
    CoUninitialize();
    return hr;
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"

	"fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"

	"worklog/internal/shared/shell"
)

func pickBackupFolder(ui *shell.UI, initial string, done func(string, error)) {
	var owner uintptr
	if native, ok := ui.Win.(driver.NativeWindow); ok {
		native.RunNative(func(context any) {
			if window, ok := context.(driver.WindowsWindowContext); ok {
				owner = window.HWND
			}
		})
	}
	if owner == 0 {
		done("", fmt.Errorf("the native folder picker needs an open app window"))
		return
	}
	var folder string
	var pickerErr error
	ui.Async(func() error {
		// COM's apartment and the dialog must stay on the same OS thread.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		start, err := windows.UTF16FromString(initial)
		if err != nil {
			pickerErr = err
			return nil
		}
		var selected *C.wchar_t
		hr := uint32(C.pick_backup_folder(C.uintptr_t(owner), (*C.wchar_t)(unsafe.Pointer(&start[0])), &selected))
		if selected != nil {
			defer C.CoTaskMemFree(C.LPVOID(unsafe.Pointer(selected)))
		}
		if hr == 0x800704c7 {
			return nil
		} // user cancelled
		if int32(hr) < 0 {
			pickerErr = fmt.Errorf("Windows folder picker failed (0x%08X)", hr)
			return nil
		}
		if selected != nil {
			folder = windows.UTF16PtrToString((*uint16)(unsafe.Pointer(selected)))
		}
		return nil
	}, func() { done(folder, pickerErr) })
}
