//go:build !windows

package proc

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
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

// killGrace is how long KillTree waits after SIGTERM before SIGKILL.
var killGrace = 3 * time.Second

// KillTree stops a process and everything it started, and returns once they
// are gone (after at most killGrace). Terminals start their program with
// setsid (go-pty, like creack/pty), so pid also names a process group that
// holds its descendants: npm run dev's vite, artisan serve's PHP server. The
// group gets SIGTERM and SIGHUP (what a terminal sends when its window closes;
// interactive shells ignore SIGTERM but exit on SIGHUP and hang up their
// jobs), then SIGKILL if anything is still alive after killGrace. A
// descendant that moved itself to another group or session escapes this.
// It is safe to call on a pid that has already exited.
func KillTree(pid int) {
	if pid <= 1 {
		return // kill(-1) would signal every process we may signal
	}
	signalTree(pid, syscall.SIGTERM)
	signalTree(pid, syscall.SIGHUP)
	deadline := time.Now().Add(killGrace)
	for treeAlive(pid) {
		if time.Now().After(deadline) {
			signalTree(pid, syscall.SIGKILL)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// signalTree signals pid's process group or, when pid does not lead one,
// pid and its direct children.
func signalTree(pid int, sig syscall.Signal) {
	err := syscall.Kill(-pid, sig)
	if err == nil || !(errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM)) {
		return
	}
	_ = exec.Command("pkill", "-"+strconv.Itoa(int(sig)), "-P", strconv.Itoa(pid)).Run()
	_ = syscall.Kill(pid, sig)
}

// treeAlive reports whether pid's group, or pid itself, still exists. A
// zombie counts until its parent reaps it.
func treeAlive(pid int) bool {
	return syscall.Kill(-pid, 0) == nil || syscall.Kill(pid, 0) == nil
}

// ForegroundGroup returns the foreground process group of the terminal whose
// master end is fd, or 0. In an interactive shell a command the user typed
// (npm run dev) runs in a group of its own, not in the shell's.
func ForegroundGroup(fd uintptr) int {
	pgid, err := unix.IoctlGetInt(int(fd), unix.TIOCGPGRP)
	if err != nil || pgid <= 1 {
		return 0
	}
	return pgid
}

// UnwrapShim resolves an npm .cmd shim to its .exe (Windows only).
func UnwrapShim(path string) string { return path }
