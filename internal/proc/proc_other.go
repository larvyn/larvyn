//go:build !windows

package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
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

// Cwd is the current folder of a running process, or "" when it cannot be
// read.
func Cwd(pid int) string {
	if pid <= 1 {
		return ""
	}
	if runtime.GOOS == "linux" {
		p, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
		return p
	}
	for _, f := range lsofNames("-a", "-p", strconv.Itoa(pid), "-d", "cwd") {
		return f
	}
	return ""
}

// GroupOpenFiles lists the files that the processes of group pgid have open
// and match keeps. ok is false where open files cannot be listed.
func GroupOpenFiles(pgid int, keep func(path string) bool) (files []string, ok bool) {
	if pgid <= 1 {
		return nil, false
	}
	var all []string
	if runtime.GOOS == "linux" {
		procs, err := os.ReadDir("/proc")
		if err != nil {
			return nil, false
		}
		for _, d := range procs {
			pid, err := strconv.Atoi(d.Name())
			if err != nil {
				continue
			}
			if g, err := unix.Getpgid(pid); err != nil || g != pgid {
				continue
			}
			fds, _ := os.ReadDir("/proc/" + d.Name() + "/fd")
			for _, fd := range fds {
				if p, err := os.Readlink("/proc/" + d.Name() + "/fd/" + fd.Name()); err == nil {
					all = append(all, p)
				}
			}
		}
	} else {
		if _, err := exec.LookPath("lsof"); err != nil {
			return nil, false
		}
		all = lsofNames("-g", strconv.Itoa(pgid))
	}
	for _, p := range all {
		if keep(p) {
			files = append(files, p)
		}
	}
	return files, true
}

// lsofNames runs lsof with args and returns the file names it reports.
func lsofNames(args ...string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", append([]string{"-w", "-Fn"}, args...)...).Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") && len(line) > 1 {
			names = append(names, line[1:])
		}
	}
	return names
}
