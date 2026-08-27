//go:build windows

package main

import (
	"os/exec"
	"syscall"
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
