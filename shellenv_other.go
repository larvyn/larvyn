//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const pathMarker = "__LARVYN_PATH__"

// importShellPath adds the PATH of the user's login shell to ours. Opened
// from a desktop menu, Larvyn inherits the session's PATH, which misses what
// ~/.zshrc or ~/.bashrc add (nvm, fnm, bun, npm globals…), so agents like
// codex and opencode, or node itself, would look missing.
func importShellPath() {
	sh := os.Getenv("SHELL")
	if sh == "" || os.Getenv("LARVYN_NO_SHELL_ENV") != "" {
		return
	}
	script := `printf '\n` + pathMarker + `%s` + pathMarker + `\n' "$PATH"`
	if filepath.Base(sh) == "fish" {
		script = `printf '\n` + pathMarker + `%s` + pathMarker + `\n' (string join : $PATH)`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	// -i loads the interactive rc files, where most version managers live.
	cmd := exec.CommandContext(ctx, sh, "-ilc", script)
	cmd.Env = append(os.Environ(), "LARVYN_NO_SHELL_ENV=1")
	out, _ := cmd.Output() // rc files may print or exit non-zero; the marker decides
	if shellPath := between(string(out), pathMarker); shellPath != "" {
		os.Setenv("PATH", mergePath(shellPath, os.Getenv("PATH")))
	}
}

func between(s, marker string) string {
	_, rest, ok := strings.Cut(s, marker)
	if !ok {
		return ""
	}
	v, _, ok := strings.Cut(rest, marker)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

// mergePath keeps the shell's order first and appends entries only the
// current PATH had, without duplicates.
func mergePath(first, second string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range append(filepath.SplitList(first), filepath.SplitList(second)...) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}
