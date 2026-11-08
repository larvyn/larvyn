package clibin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteReplacesOnlyWhatChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin", "larvyn.exe")
	if err := write(path, []byte("v1")); err != nil {
		t.Fatal(err)
	}
	st1, _ := os.Stat(path)
	if runtime.GOOS != "windows" && st1.Mode().Perm() != 0o700 {
		t.Errorf("mode %v", st1.Mode())
	}
	// Same bytes: the file is left alone.
	if err := write(path, []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if st2, _ := os.Stat(path); !st2.ModTime().Equal(st1.ModTime()) {
		t.Error("rewritten with the same bytes")
	}
	if err := write(path, []byte("v2")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "v2" {
		t.Errorf("got %q", b)
	}
	for _, leftover := range []string{path + ".new", path + ".old"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("%s left behind", filepath.Base(leftover))
		}
	}
}

func TestInstallLinksTheApp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows writes the embedded CLI")
	}
	t.Setenv("LARVYN_HOME", t.TempDir())
	dir, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	if got, _ := os.Readlink(filepath.Join(dir, "larvyn")); got != exe {
		t.Errorf("links to %q, want %q", got, exe)
	}
	// Again: nothing to do.
	if _, err := Install(); err != nil {
		t.Fatal(err)
	}
}
