//go:build windows

package main

import (
	"os/exec"
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
