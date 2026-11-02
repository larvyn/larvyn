//go:build windows

package proc

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// A GUI build has no console, so every console child (php, git, npm) would
// flash its own window unless told not to create one.
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

// DefaultShell is PowerShell when installed, cmd.exe otherwise.
func DefaultShell() []string {
	for _, sh := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(sh); err == nil {
			return []string{p, "-NoLogo"}
		}
	}
	return []string{"cmd.exe"}
}

// ShellCommand runs a command line through cmd.exe.
func ShellCommand(command string) []string {
	return []string{"cmd.exe", "/d", "/s", "/c", command}
}

// KillTree stops a process and its children; php artisan serve and npm run
// dev both leave grandchildren behind otherwise.
func KillTree(pid int) {
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	HideWindow(cmd)
	_ = cmd.Run()
}

// ForegroundGroup is a Unix notion; taskkill /T already reaches a console's
// processes.
func ForegroundGroup(fd uintptr) int { return 0 }

var shimLine = regexp.MustCompile(`(?i)^\s*@?\s*"?([a-z]:\\[^"]+\.exe)"?\s+%\*\s*$`)

// UnwrapShim returns the program a one-line .bat/.cmd wrapper runs, such as
// Herd's php.bat ("C:\...\php85\php.exe" %*). Running the .exe directly means
// Ctrl+C stops the program instead of cmd asking "Terminate batch job (Y/N)?".
func UnwrapShim(path string) string {
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
// PHP.exe run directly, enable it here so Ctrl+C in a terminal reaches them.
func init() {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler")
	_, _, _ = proc.Call(0, 0)
}

// Cwd is the current folder of a running process; Windows does not tell.
func Cwd(pid int) string { return "" }

// GroupOpenFiles lists a process group's open files; not on Windows.
func GroupOpenFiles(pgid int, keep func(path string) bool) ([]string, bool) { return nil, false }
