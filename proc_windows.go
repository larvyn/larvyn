//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// A GUI build has no console, so every console child (php, git, npm) would
// flash its own window unless told not to create one.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func defaultShell() []string {
	for _, sh := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(sh); err == nil {
			return []string{p, "-NoLogo"}
		}
	}
	return []string{"cmd.exe"}
}

func shellCommand(command string) []string {
	return []string{"cmd.exe", "/d", "/s", "/c", command}
}

// killTree stops a process and its children; php artisan serve and npm run
// dev both leave grandchildren behind otherwise.
func killTree(pid int) {
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", itoa(pid))
	hideWindow(cmd)
	_ = cmd.Run()
}

var shimLine = regexp.MustCompile(`(?i)^\s*@?\s*"?([a-z]:\\[^"]+\.exe)"?\s+%\*\s*$`)

// unwrapShim returns the program a one-line .bat/.cmd wrapper runs, such as
// Herd's php.bat ("C:\...\php85\php.exe" %*). Running the .exe directly means
// Ctrl+C stops the program instead of cmd asking "Terminate batch job (Y/N)?".
func unwrapShim(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".bat" && ext != ".cmd" {
		return path
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 4096 {
		return path
	}
	exe := ""
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "@echo off") || strings.HasPrefix(strings.ToLower(line), "rem ") {
			continue
		}
		m := shimLine.FindStringSubmatch(line)
		if m == nil || exe != "" {
			return path
		}
		exe = m[1]
	}
	if fi, err := os.Stat(exe); exe == "" || err != nil || fi.IsDir() {
		return path
	}
	return exe
}

// Children inherit whether Ctrl+C is ignored. Larvyn may itself have been
// started ignoring it (from a shell or an agent), and cmd.exe used to hide that
// by re-enabling Ctrl+C for what it runs; now that programs like Herd's
// php.exe run directly, enable it here so Ctrl+C in a terminal reaches them.
func init() {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler")
	_, _, _ = proc.Call(0, 0)
}
