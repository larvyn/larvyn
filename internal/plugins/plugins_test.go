package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"larvyn/internal/updater"
)

func TestLoad(t *testing.T) {
	os.RemoveAll(Dir())
	if got := Load(); len(got) != 0 {
		t.Fatalf("no plugins folder should mean no plugins, got %+v", got)
	}
	defer os.RemoveAll(Dir())

	writePlugin(t, filepath.Join(Dir(), "acme.horizon"), validManifest)
	writePlugin(t, filepath.Join(Dir(), "wrong-name"), validManifest)
	writePlugin(t, filepath.Join(Dir(), "acme.newer"), edit(t, func(m map[string]any) {
		m["id"] = "acme.newer"
		m["larvyn"] = ">=99.0.0"
	}))
	os.MkdirAll(filepath.Join(Dir(), "empty"), 0o755)
	os.MkdirAll(filepath.Join(Dir(), ".cache"), 0o755)
	os.WriteFile(filepath.Join(Dir(), "notes.txt"), []byte("x"), 0o644)

	old := updater.Version
	updater.Version = "v0.1.0-beta.8"
	defer func() { updater.Version = old }()

	got := map[string]Plugin{}
	var order []string
	for _, p := range Load() {
		got[filepath.Base(p.Dir)] = p
		order = append(order, p.Manifest.ID)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 plugin folders (not .cache or notes.txt), got %v", order)
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] > order[i] {
			t.Errorf("not sorted by id: %v", order)
		}
	}
	want := map[string]string{
		"acme.horizon": "",
		"wrong-name":   "named after the plugin id",
		"acme.newer":   "needs Larvyn >=99.0.0",
		"empty":        "no larvyn.json",
	}
	for dir, msg := range want {
		p := got[dir]
		if msg == "" && p.Error != "" || msg != "" && !strings.Contains(p.Error, msg) {
			t.Errorf("%s: error %q, want %q", dir, p.Error, msg)
		}
	}
	if got["empty"].Manifest.ID != "empty" {
		t.Errorf("a broken plugin should still have an id to show: %+v", got["empty"])
	}
}

func TestLoadFollowsSymlinkedFolders(t *testing.T) {
	defer os.RemoveAll(Dir())
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	os.MkdirAll(Dir(), 0o755)
	if err := os.Symlink(src, filepath.Join(Dir(), "acme.horizon")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	ps := Load()
	if len(ps) != 1 || ps[0].Error != "" {
		t.Errorf("a symlinked plugin folder should load: %+v", ps)
	}
}
