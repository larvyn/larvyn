package project

import (
	"os"
	"path/filepath"
	"testing"

	"larvyn/internal/store"
)

func TestRepairEnvRefusesSymlinkOutside(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside")
	broken := "APP_URL=http://localhost:8000:8000\n"
	os.WriteFile(target, []byte(broken), 0o644)
	if err := os.Symlink(target, filepath.Join(dir, ".env")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	p := store.Project{ID: store.NewID(), Name: "x", Path: dir}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) })
	if changed, err := (&ProjectService{}).RepairEnv(p.ID); err == nil || changed {
		t.Errorf("RepairEnv followed the symlink: changed=%v err=%v", changed, err)
	}
	if b, _ := os.ReadFile(target); string(b) != broken {
		t.Fatalf("file outside the project changed: %q", b)
	}
}
