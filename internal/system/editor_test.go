package system

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeBundle(t *testing.T, dir, rel string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBundledEditor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exec bits are not meaningful on windows")
	}
	sys, user := t.TempDir(), t.TempDir()
	dirs := []string{sys, user}

	if got := bundledEditor("code", dirs); got != "" {
		t.Fatalf("empty dirs: got %q", got)
	}

	// Found in ~/Applications when /Applications doesn't have it.
	userCode := fakeBundle(t, user, macEditorBundles["code"], 0o755)
	if got := bundledEditor("code", dirs); got != userCode {
		t.Fatalf("code: got %q, want %q", got, userCode)
	}
	// /Applications wins when both have it.
	sysCode := fakeBundle(t, sys, macEditorBundles["code"], 0o755)
	if got := bundledEditor("code", dirs); got != sysCode {
		t.Fatalf("code: got %q, want %q", got, sysCode)
	}

	// A file without the exec bit is skipped.
	fakeBundle(t, sys, macEditorBundles["phpstorm"], 0o644)
	if got := bundledEditor("phpstorm", dirs); got != "" {
		t.Fatalf("non-executable phpstorm: got %q", got)
	}

	// Each known editor resolves; unknown names never do.
	for name, rel := range macEditorBundles {
		if name == "code" || name == "phpstorm" {
			continue
		}
		want := fakeBundle(t, user, rel, 0o755)
		if got := bundledEditor(name, dirs); got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	if got := bundledEditor("nvim", dirs); got != "" {
		t.Fatalf("nvim has no bundle: got %q", got)
	}
}

func TestEditorNotFoundHint(t *testing.T) {
	msg := editorNotFound("code")
	if !strings.Contains(msg, "Install 'code' command in PATH") || !strings.Contains(msg, "Settings") {
		t.Fatalf("code hint missing: %q", msg)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(msg, "/Applications") {
		t.Fatalf("darwin message should mention /Applications: %q", msg)
	}
	if msg := editorNotFound("myeditor"); msg != "myeditor was not found on PATH; pick another editor in Settings" {
		t.Fatalf("custom editor: %q", msg)
	}
}

// Every built-in editor with a macOS bundle has an install hint.
func TestEditorTablesAgree(t *testing.T) {
	for name := range macEditorBundles {
		if _, ok := editorInstallHints[name]; !ok {
			t.Errorf("%s has a bundle path but no install hint", name)
		}
	}
	for id, tmpl := range editors {
		if id == "nvim" {
			continue
		}
		if _, ok := macEditorBundles[tmpl[0]]; !ok {
			t.Errorf("editor %s (%s) has no macOS bundle path", id, tmpl[0])
		}
	}
}
