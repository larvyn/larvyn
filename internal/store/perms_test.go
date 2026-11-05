package store

import (
	"os"
	"path/filepath"
	"runtime"
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
