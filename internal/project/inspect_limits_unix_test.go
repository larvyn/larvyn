//go:build !windows

package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"larvyn/internal/store"
)

// A project whose manifests are symlinks to /dev/zero is inspected without
// reading them forever (#54).
func TestInspectSkipsDeviceManifests(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "artisan"), []byte("<?php\n"), 0o644)
	for _, f := range []string{"composer.json", "composer.lock", "package.json"} {
		if err := os.Symlink("/dev/zero", filepath.Join(dir, f)); err != nil {
			t.Skip(err)
		}
	}
	done := make(chan ProjectInfo, 1)
	go func() { done <- Inspect(store.Project{ID: "z", Name: "z", Path: dir}) }()
	select {
	case info := <-done:
		if !info.Exists || info.IsLaravel {
			t.Fatalf("exists=%v laravel=%v", info.Exists, info.IsLaravel)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Inspect hung on /dev/zero")
	}
}
