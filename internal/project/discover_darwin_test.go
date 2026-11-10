//go:build darwin

package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGuardedDirsOnMac(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Pictures", "code"} {
		os.MkdirAll(filepath.Join(home, name), 0o755)
	}

	guarded, narrowed := guardedDirs(home, nil, false)
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Library", "Music", "Movies", "Pictures", "Public"} {
		if !guarded[filepath.Join(home, name)] {
			t.Errorf("%s not guarded", name)
		}
	}
	if guarded[filepath.Join(home, "code")] {
		t.Error("code guarded")
	}
	if !narrowed {
		t.Error("want narrowed: Desktop, Documents and Downloads exist")
	}
	if guarded["/"] || guarded["/System/Volumes/Data"] {
		t.Error("the system volumes must not be guarded")
	}

	// Asked for: Desktop, Documents and Downloads are walked, the rest not.
	guarded, narrowed = guardedDirs(home, nil, true)
	if narrowed || guarded[filepath.Join(home, "Documents")] || !guarded[filepath.Join(home, "Pictures")] {
		t.Errorf("widened: narrowed=%v guarded=%v", narrowed, guarded)
	}

	// A folder holding an added project was granted when it was added.
	// The check ignores case, as APFS does by default.
	guarded, _ = guardedDirs(home, []string{filepath.Join(home, "documents", "shop")}, false)
	if guarded[filepath.Join(home, "Documents")] || !guarded[filepath.Join(home, "Desktop")] {
		t.Errorf("added project: guarded=%v", guarded)
	}
}

func TestDiscoverSkipsGuardedInAnyCase(t *testing.T) {
	root := realTempDir(t)
	os.MkdirAll(filepath.Join(root, "Documents", "shop"), 0o755)
	os.WriteFile(filepath.Join(root, "Documents", "shop", "artisan"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "Documents", "shop", "composer.json"), []byte(`{"require":{"laravel/framework":"^11.0"}}`), 0o644)
	guarded := map[string]bool{filepath.Join(root, "DOCUMENTS"): true}
	if found := discover([]string{root}, guarded, 4, time.Now().Add(time.Minute)); len(found) != 0 {
		t.Errorf("found %v inside a guarded folder spelled another way", found)
	}
}
