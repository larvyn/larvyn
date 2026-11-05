package store

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// state.json and larvyn.log hold launch commands, paths and errors: only the
// user may read them (#53).
func TestStateAndLogArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	dir := t.TempDir()
	t.Setenv("LARVYN_HOME", dir)
	// A log left world-readable by an older version is tightened.
	os.WriteFile(filepath.Join(dir, "larvyn.log"), []byte("old\n"), 0o644)
	AppendLog("info", "hello")
	s := New()
	if err := s.Update(func(d *State) { d.Projects = append(d.Projects, Project{ID: "a"}) }); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"larvyn.log", "state.json"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", name, fi.Mode().Perm())
		}
	}
}

func TestFallbackHomeIsPrivateAndStable(t *testing.T) {
	a, b := fallbackHome(), fallbackHome()
	if a != b {
		t.Fatalf("fallback changed within a run: %q then %q", a, b)
	}
	if a == filepath.Join(os.TempDir(), "larvyn") {
		t.Fatal("fallback is the shared, predictable temp path")
	}
	if fi, err := os.Stat(a); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700) {
		t.Fatalf("fallback %q: %v %v", a, fi, err)
	}
	os.RemoveAll(a)
}

// A state.json.tmp an older version left world-readable doesn't make the
// next state.json world-readable.
func TestSaveIgnoresStaleTempFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	dir := t.TempDir()
	t.Setenv("LARVYN_HOME", dir)
	os.WriteFile(filepath.Join(dir, "state.json.tmp"), []byte("{}"), 0o644)
	if err := New().Update(func(d *State) { d.Projects = append(d.Projects, Project{ID: "b"}) }); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "state.json")); fi.Mode().Perm() != 0o600 {
		t.Errorf("state.json mode = %v, want 0600", fi.Mode().Perm())
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "state.json.*.tmp")); len(m) > 0 {
		t.Errorf("temp files left behind: %v", m)
	}
}

// Without $HOME, the folder still comes from the account's home, so it is
// the same on every launch and state persists.
func TestHomeDirWithoutHomeEnvIsStable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("UserConfigDir reads $HOME on linux")
	}
	t.Setenv("LARVYN_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if u, err := user.Current(); err != nil || u.HomeDir == "" {
		t.Skip("the account has no home in the user database; fallbackHome covers this")
	}
	t.Setenv("HOME", "")
	a, b := HomeDir(), HomeDir()
	if a != b || strings.HasPrefix(a, os.TempDir()) {
		t.Fatalf("HomeDir without $HOME = %q then %q, want a stable folder outside the temp dir", a, b)
	}
}
