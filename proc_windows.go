//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// hideConsole prevents gh (a console program) from popping a cmd window on
// every invocation, which otherwise flashes hundreds of windows and can crash
// the GUI when many run at once.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

// maximizeAppWindow tells Windows to fill the working area with the window
// carrying this title — the same effect as clicking the maximise button.
//
// fyne 2.8 has no Maximise() method; SetFullScreen takes the title bar with it,
// which is not what the user asked for. FindWindow + ShowWindow(SW_MAXIMIZE)
// keeps the frame and honours the taskbar.
//
// The window has to be up before this is called, so it goes in a goroutine
// scheduled a beat after ShowAndRun. A miss (title not found yet) is silent
// rather than an error, because the app is still perfectly usable at the
// default size — this is only a nicety.
func maximizeAppWindow(title string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	findWindowW := user32.NewProc("FindWindowW")
	showWindow := user32.NewProc("ShowWindow")

	ptr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	hwnd, _, _ := findWindowW.Call(0, uintptr(unsafe.Pointer(ptr)))
	if hwnd == 0 {
		return
	}
	const swMaximize = 3
	showWindow.Call(hwnd, swMaximize)
}
