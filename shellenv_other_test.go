//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolDirs(t *testing.T) {
	got := strings.Join(toolDirs("/Users/me", "", ""), "\n")
	for _, want := range []string{
		"/Users/me/.composer/vendor/bin",
		"/Users/me/.config/composer/vendor/bin",
		"/Users/me/Library/Application Support/Herd/bin",
		"/opt/homebrew/bin",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("toolDirs misses %s:\n%s", want, got)
		}
	}
	if d := toolDirs("/Users/me", "/custom/composer", "")[0]; d != "/custom/composer/vendor/bin" {
		t.Errorf("COMPOSER_HOME should come first, got %s", d)
	}
}

func TestAddToolDirsKeepsShellOrder(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".composer", "vendor", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("COMPOSER_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("PATH", "/usr/bin:/bin")
	addToolDirs()
	parts := filepath.SplitList(os.Getenv("PATH"))
	if parts[0] != "/usr/bin" || parts[1] != "/bin" {
		t.Fatalf("shell entries should stay first: %v", parts)
	}
	found := false
	for _, p := range parts {
		if p == bin {
			found = true
		}
		if strings.HasPrefix(p, home) && p != bin {
			t.Errorf("added a folder that does not exist: %s", p)
		}
	}
	if !found {
		t.Fatalf("%s not added: %v", bin, parts)
	}
}
