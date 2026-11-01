//go:build !windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const pathMarker = "__LARVYN_PATH__"

// shellEnvTimeout bounds the login shell started to read PATH. Heavy rc files
// (oh-my-zsh, nvm, conda) can take a few seconds on a cold start.
const shellEnvTimeout = 10 * time.Second

// importShellPath adds the PATH of the user's login shell to ours. Opened
// from a desktop menu, Larvyn inherits the session's PATH, which misses what
// ~/.zshrc or ~/.bashrc add (nvm, fnm, bun, npm globals…), so agents like
// codex and opencode, or node itself, would look missing.
func importShellPath() {
	defer addToolDirs()
	sh := os.Getenv("SHELL")
	if sh == "" || os.Getenv("LARVYN_NO_SHELL_ENV") != "" {
		return
	}
	script := `printf '\n` + pathMarker + `%s` + pathMarker + `\n' "$PATH"`
	if filepath.Base(sh) == "fish" {
		script = `printf '\n` + pathMarker + `%s` + pathMarker + `\n' (string join : $PATH)`
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), shellEnvTimeout)
	defer cancel()
	// -i loads the interactive rc files, where most version managers live.
	cmd := exec.CommandContext(ctx, sh, "-ilc", script)
	cmd.Env = append(os.Environ(), "LARVYN_NO_SHELL_ENV=1")
	out, _ := cmd.Output() // rc files may print or exit non-zero; the marker decides
	shellPath := between(string(out), pathMarker)
	if shellPath == "" {
		// Without this trace a missing tool (laravel, node…) is a mystery.
		(&SystemService{}).Log("warn", fmt.Sprintf("shell PATH import from %s failed after %s (timeout %s, ctx %v)",
			sh, time.Since(start).Round(time.Millisecond), shellEnvTimeout, ctx.Err()))
		return
	}
	os.Setenv("PATH", mergePath(shellPath, os.Getenv("PATH")))
}

// addToolDirs appends the folders Laravel tooling installs into (Composer
// global bin with the laravel installer, Herd, Homebrew) when they exist, so
// those tools are found even when the shell PATH import fails or times out.
// They go last, so the shell's own order still wins.
func addToolDirs() {
	home, _ := os.UserHomeDir()
	var dirs []string
	for _, d := range toolDirs(home, os.Getenv("COMPOSER_HOME"), os.Getenv("XDG_CONFIG_HOME")) {
		if isDir(d) {
			dirs = append(dirs, d)
		}
	}
	os.Setenv("PATH", mergePath(os.Getenv("PATH"), strings.Join(dirs, string(os.PathListSeparator))))
}

func toolDirs(home, composerHome, xdgConfig string) []string {
	var dirs []string
	if composerHome != "" {
		dirs = append(dirs, filepath.Join(composerHome, "vendor", "bin"))
	}
	if xdgConfig == "" && home != "" {
		xdgConfig = filepath.Join(home, ".config")
	}
	if xdgConfig != "" {
		dirs = append(dirs, filepath.Join(xdgConfig, "composer", "vendor", "bin"), filepath.Join(xdgConfig, "herd-lite", "bin"))
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".composer", "vendor", "bin"),
			filepath.Join(home, "Library", "Application Support", "Herd", "bin"),
		)
	}
	return append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
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
