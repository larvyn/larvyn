package laravel

import (
	"os"
	"path/filepath"
	"testing"
)

// outsideTarget makes a file outside the project for a symlink to point at.
func outsideTarget(t *testing.T) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "bashrc")
	if err := os.WriteFile(target, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return target
}

func assertUntouched(t *testing.T, path string) {
	t.Helper()
	if b, _ := os.ReadFile(path); string(b) != "keep me\n" {
		t.Fatalf("file outside the project changed: %q", b)
	}
}

func TestSaveEnvRefusesSymlinkOutside(t *testing.T) {
	dir := t.TempDir()
	target := outsideTarget(t)
	if err := os.Symlink(target, filepath.Join(dir, ".env")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	p, err := addProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	l := &LaravelService{}
	if err := l.SaveEnv(p.ID, ".env", "APP_NAME=pwned\n"); err == nil {
		t.Error("SaveEnv wrote through a symlink out of the project")
	}
	if err := l.SetEnv(p.ID, "APP_DEBUG", "false"); err == nil {
		t.Error("SetEnv wrote through a symlink out of the project")
	}
	assertUntouched(t, target)
}

func TestSaveEnvFollowsSymlinkInside(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.local"), []byte("A=1\n"), 0o644)
	if err := os.Symlink(".env.local", filepath.Join(dir, ".env")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	p, _ := addProject(dir)
	if err := (&LaravelService{}).SaveEnv(p.ID, ".env", "A=2\n"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".env.local")); string(b) != "A=2\n" {
		t.Errorf(".env.local = %q", b)
	}
}

func TestClearLogRefusesSymlinkOutside(t *testing.T) {
	dir := t.TempDir()
	target := outsideTarget(t)
	logs := filepath.Join(dir, "storage", "logs")
	os.MkdirAll(logs, 0o755)
	if err := os.Symlink(target, filepath.Join(logs, "laravel.log")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	// A whole logs folder linked out of the project is refused too.
	other := filepath.Join(dir, "storage", "other")
	os.Symlink(filepath.Dir(target), other)
	p, _ := addProject(dir)
	l := &LaravelService{}
	if err := l.ClearLog(p.ID, "laravel.log"); err == nil {
		t.Error("ClearLog truncated through a symlink out of the project")
	}
	assertUntouched(t, target)

	os.WriteFile(filepath.Join(logs, "real.log"), []byte("x\n"), 0o644)
	if err := l.ClearLog(p.ID, "real.log"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(logs, "real.log")); fi.Size() != 0 {
		t.Error("real.log not cleared")
	}
}
