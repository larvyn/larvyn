package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLaravel(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "artisan"), []byte("#!/usr/bin/env php"), 0o644)
	os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"laravel/framework":"^12.0"}}`), 0o644)
}

func TestDiscoverFindsLaravelApps(t *testing.T) {
	root := t.TempDir()
	writeLaravel(t, filepath.Join(root, "code", "shop"))
	writeLaravel(t, filepath.Join(root, "code", "client", "crm"))
	// Not projects: a nested app inside a project, one under node_modules,
	// a hidden folder, a plain PHP package and a git worktree.
	writeLaravel(t, filepath.Join(root, "code", "shop", "packages", "inner"))
	writeLaravel(t, filepath.Join(root, "code", "node_modules", "x"))
	writeLaravel(t, filepath.Join(root, ".cache", "y"))
	os.MkdirAll(filepath.Join(root, "lib"), 0o755)
	os.WriteFile(filepath.Join(root, "lib", "composer.json"), []byte(`{"require":{"php":"^8.2"}}`), 0o644)
	wt := filepath.Join(root, "worktrees", "shop-agent")
	writeLaravel(t, wt)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ../../code/shop/.git/worktrees/a"), 0o644)
	// A custom APP_NAME names the project.
	os.WriteFile(filepath.Join(root, "code", "client", "crm", ".env"), []byte("APP_NAME=\"Acme CRM\"\n"), 0o644)
	// Too deep for the depth limit.
	writeLaravel(t, filepath.Join(root, "a", "b", "c", "deep"))

	got := discover([]string{root}, 3, time.Now().Add(time.Minute))
	names := map[string]string{}
	for _, f := range got {
		names[f.Path] = f.Name
	}
	if len(got) != 2 {
		t.Fatalf("found %d projects, want 2: %+v", len(got), got)
	}
	if names[filepath.Join(root, "code", "shop")] != "Shop" {
		t.Errorf("shop not found: %+v", got)
	}
	if names[filepath.Join(root, "code", "client", "crm")] != "Acme CRM" {
		t.Errorf("crm not found or misnamed: %+v", got)
	}
}

func TestDiscoverMarksAddedProjects(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "app")
	writeLaravel(t, app)
	s := &ProjectService{}
	p, err := s.Add(app)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Remove(p.ID)
	got := discover([]string{root}, 3, time.Now().Add(time.Minute))
	if len(got) != 1 || !got[0].Added {
		t.Fatalf("want the app marked as added: %+v", got)
	}
}
