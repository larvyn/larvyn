//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func hideWindow(cmd *exec.Cmd) {}

func defaultShell() []string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return []string{sh, "-l"}
	}
	return []string{"/bin/bash", "-l"}
}

func shellCommand(command string) []string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return []string{sh, "-lc", command}
}

func killTree(pid int) {
	_ = exec.Command("pkill", "-TERM", "-P", itoa(pid)).Run()
}

func unwrapShim(path string) string { return path }
