//go:build !windows

package backend

import "os/exec"

// hideConsole is a no-op off Windows.
func hideConsole(cmd *exec.Cmd) {}

// MaximizeAppWindow is a no-op off Windows. Other platforms already have a
// working maximise gesture from their own window manager.
func MaximizeAppWindow(string) {}
