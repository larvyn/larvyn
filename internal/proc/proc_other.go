//go:build !windows

package proc

import (
	"os"
	"os/exec"
	"strconv"
)

// HideWindow keeps a console window from flashing up (Windows only).
func HideWindow(cmd *exec.Cmd) {}

// DefaultShell is the login shell for a new terminal.
func DefaultShell() []string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return []string{sh, "-l"}
	}
	return []string{"/bin/bash", "-l"}
}

// ShellCommand runs a command line through the login shell.
func ShellCommand(command string) []string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return []string{sh, "-lc", command}
}

// KillTree stops a process's children.
func KillTree(pid int) {
	_ = exec.Command("pkill", "-TERM", "-P", strconv.Itoa(pid)).Run()
}

// UnwrapShim resolves an npm .cmd shim to its .exe (Windows only).
func UnwrapShim(path string) string { return path }
