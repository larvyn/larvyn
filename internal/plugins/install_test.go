package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"larvyn/internal/store"
)

// reset empties the plugins folder and forgets every plugin decision.
func reset(t *testing.T) {
	t.Helper()
	os.RemoveAll(Dir())
	store.Update(func(d *store.State) { d.Plugins = nil })
	t.Cleanup(func() {
		os.RemoveAll(Dir())
		store.Update(func(d *store.State) { d.Plugins = nil })
	})
}

var horizonPerms = []string{"project.read", "terminal.run"}

func TestInstallCopiesAndEnables(t *testing.T) {
	reset(t)
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	os.MkdirAll(filepath.Join(src, ".git"), 0o755)
	os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(src, "node_modules", "x"), 0o755)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("secret"), 0o644)
	symlinks := os.Symlink(outside, filepath.Join(src, "ui", "secret.txt")) == nil

	s := &PluginService{}
	p, err := s.Install(src, horizonPerms)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Enabled || p.Dir != filepath.Join(Dir(), "acme.horizon") || p.Linked {
		t.Fatalf("installed plugin: %+v", p)
	}
	for _, f := range []string{"larvyn.json", "ui/index.html"} {
		if _, err := os.Stat(filepath.Join(p.Dir, f)); err != nil {
			t.Errorf("%s was not copied: %v", f, err)
		}
	}
	for _, f := range []string{".git", "node_modules"} {
		if _, err := os.Stat(filepath.Join(p.Dir, f)); err == nil {
			t.Errorf("%s should not be copied", f)
		}
	}
	if _, err := os.Lstat(filepath.Join(p.Dir, "ui", "secret.txt")); symlinks && err == nil {
		t.Error("a symlink was copied")
	}
	if _, err := os.Stat(filepath.Join(src, ManifestFile)); err != nil {
		t.Error("the source folder must be left alone")
	}
	if st := store.Plugins()["acme.horizon"]; !st.Enabled || len(st.Granted) != 2 || st.InstalledAt == 0 {
		t.Errorf("state: %+v", st)
	}
	left, _ := filepath.Glob(filepath.Join(Dir(), ".install-*"))
	if len(left) > 0 {
		t.Errorf("temporary folders left behind: %v", left)
	}
}

func TestInstallNeedsEveryPermission(t *testing.T) {
	reset(t)
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	_, err := (&PluginService{}).Install(src, []string{"project.read"})
	if err == nil || !strings.Contains(err.Error(), "terminal.run") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(Dir(), "acme.horizon")); err == nil {
		t.Error("a plugin was installed without its permissions")
	}
}

func TestInstallRefusesSymlinkedEntry(t *testing.T) {
	reset(t)
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	// The entry is a symlink to a file inside the folder: valid in place,
	// but symlinks aren't copied, so the installed copy would miss it.
	os.Rename(filepath.Join(src, "ui", "index.html"), filepath.Join(src, "real.html"))
	if err := os.Symlink(filepath.Join(src, "real.html"), filepath.Join(src, "ui", "index.html")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	_, err := (&PluginService{}).Install(src, horizonPerms)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not copied") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(Dir(), "acme.horizon")); err == nil {
		t.Error("an invalid copy was installed")
	}
}

func TestInstallTooLarge(t *testing.T) {
	reset(t)
	old := maxInstallFiles
	maxInstallFiles = 3
	defer func() { maxInstallFiles = old }()
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	for _, f := range []string{"a", "b", "c"} {
		os.WriteFile(filepath.Join(src, f), []byte("x"), 0o644)
	}
	if _, err := (&PluginService{}).Install(src, horizonPerms); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %v", err)
	}
}

func TestUpdateKeepsOldVersionOnFailure(t *testing.T) {
	reset(t)
	s := &PluginService{}
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	if _, err := s.Install(src, horizonPerms); err != nil {
		t.Fatal(err)
	}
	// A newer version with an invalid copy must not replace the working one.
	os.Rename(filepath.Join(src, "ui", "index.html"), filepath.Join(src, "real.html"))
	if os.Symlink(filepath.Join(src, "real.html"), filepath.Join(src, "ui", "index.html")) != nil {
		t.Skip("no symlinks here")
	}
	if _, err := s.Install(src, horizonPerms); err == nil {
		t.Fatal("the broken update was installed")
	}
	p, err := find("acme.horizon")
	if err != nil || p.Error != "" || !p.Enabled {
		t.Errorf("the installed version should still work: %+v %v", p, err)
	}
}

func TestDroppedInPluginStaysOff(t *testing.T) {
	reset(t)
	writePlugin(t, filepath.Join(Dir(), "acme.horizon"), validManifest)
	p, err := find("acme.horizon")
	if err != nil {
		t.Fatal(err)
	}
	if p.Enabled || p.NeedsApproval || len(Active()) != 0 {
		t.Errorf("a plugin copied in by hand must stay off until approved: %+v", p)
	}
	s := &PluginService{}
	if _, err := s.Enable("acme.horizon", []string{"project.read"}); err == nil {
		t.Error("enabled without every permission")
	}
	if p, err = s.Enable("acme.horizon", horizonPerms); err != nil || !p.Enabled {
		t.Fatalf("enable: %+v %v", p, err)
	}
	if len(Active()) != 1 {
		t.Error("the enabled plugin should be active")
	}
}

func TestNewPermissionNeedsApproval(t *testing.T) {
	reset(t)
	s := &PluginService{}
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	if _, err := s.Install(src, horizonPerms); err != nil {
		t.Fatal(err)
	}
	// An update on disk that asks for one more permission.
	writePlugin(t, filepath.Join(Dir(), "acme.horizon"), edit(t, func(m map[string]any) {
		m["permissions"] = []string{"project.read", "terminal.run", "project.env.read"}
	}))
	p, _ := find("acme.horizon")
	if p.Enabled || !p.NeedsApproval {
		t.Fatalf("a plugin asking for more must stop until approved: %+v", p)
	}
	if p, _ = s.Enable("acme.horizon", append(horizonPerms, "project.env.read")); !p.Enabled || p.NeedsApproval {
		t.Errorf("after approval: %+v", p)
	}
}

func TestDisable(t *testing.T) {
	reset(t)
	s := &PluginService{}
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	s.Install(src, horizonPerms)
	if err := s.Disable("acme.horizon"); err != nil {
		t.Fatal(err)
	}
	p, _ := find("acme.horizon")
	if p.Enabled || p.NeedsApproval || len(p.Granted) != 2 {
		t.Errorf("disabled: %+v", p)
	}
}

func TestUninstall(t *testing.T) {
	reset(t)
	s := &PluginService{}
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	s.Install(src, horizonPerms)
	if err := s.Uninstall("acme.horizon"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(Dir(), "acme.horizon")); err == nil {
		t.Error("folder still there")
	}
	if _, ok := store.Plugins()["acme.horizon"]; ok {
		t.Error("state still there")
	}
	for _, id := range []string{"../escape", "acme.horizon/../../x", ""} {
		if err := s.Uninstall(id); err == nil {
			t.Errorf("Uninstall(%q) should be refused", id)
		}
	}
}

func TestUninstallLinkedKeepsSource(t *testing.T) {
	reset(t)
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	os.MkdirAll(Dir(), 0o755)
	if err := os.Symlink(src, filepath.Join(Dir(), "acme.horizon")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	p, _ := find("acme.horizon")
	if !p.Linked {
		t.Errorf("should be marked linked: %+v", p)
	}
	if err := (&PluginService{}).Uninstall("acme.horizon"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(src, ManifestFile)); err != nil {
		t.Error("uninstalling a linked plugin deleted its source")
	}
}

func TestInspect(t *testing.T) {
	reset(t)
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	p, err := (&PluginService{}).Inspect(src)
	if err != nil || p.Manifest.ID != "acme.horizon" || p.Enabled {
		t.Fatalf("inspect: %+v %v", p, err)
	}
	if _, err := (&PluginService{}).Inspect(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not a Larvyn plugin") {
		t.Errorf("empty folder: %v", err)
	}
}
