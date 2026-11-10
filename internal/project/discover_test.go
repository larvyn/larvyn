package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"larvyn/internal/store"
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

	got := discover([]string{root}, nil, 3, time.Now().Add(time.Minute))
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
	// Saved straight to the store: Add would also ask a running Flock to
	// serve the folder.
	p := store.Project{ID: store.NewID(), Name: "App", Path: app}
	if err := store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) }); err != nil {
		t.Fatal(err)
	}
	defer (&ProjectService{}).Remove(p.ID)
	got := discover([]string{root}, nil, 3, time.Now().Add(time.Minute))
	if len(got) != 1 || !got[0].Added {
		t.Fatalf("want the app marked as added: %+v", got)
	}
}

func TestDiscoverKeepsFoldersThatDifferInCase(t *testing.T) {
	root := t.TempDir()
	writeLaravel(t, filepath.Join(root, "Shop"))
	if _, err := os.Stat(filepath.Join(root, "shop")); err == nil {
		t.Skip("this file system ignores case")
	}
	writeLaravel(t, filepath.Join(root, "shop"))
	p := store.Project{ID: store.NewID(), Name: "Shop", Path: filepath.Join(root, "Shop")}
	if err := store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) }); err != nil {
		t.Fatal(err)
	}
	defer (&ProjectService{}).Remove(p.ID)

	got := discover([]string{root}, nil, 3, time.Now().Add(time.Minute))
	if len(got) != 2 {
		t.Fatalf("found %d projects, want 2: %+v", len(got), got)
	}
	for _, f := range got {
		if want := filepath.Base(f.Path) == "Shop"; f.Added != want {
			t.Errorf("%s: added = %v, want %v", f.Path, f.Added, want)
		}
	}
}

func TestSameFolder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "App")
	os.MkdirAll(dir, 0o755)
	if !sameFolder(dir, dir+string(filepath.Separator)) {
		t.Error("a trailing separator made it another folder")
	}
	link := filepath.Join(root, "link")
	if os.Symlink(dir, link) == nil && !sameFolder(dir, link) {
		t.Error("a symlink to the folder is not the same folder")
	}
	other := filepath.Join(root, "Other")
	os.MkdirAll(other, 0o755)
	if sameFolder(dir, other) {
		t.Error("two folders are the same")
	}
	// Case: the same folder where the file system ignores it, two otherwise.
	lower := filepath.Join(root, "app")
	_, err := os.Stat(lower)
	if insensitive := err == nil; sameFolder(dir, lower) != insensitive {
		t.Errorf("App and app: same = %v, want %v", !insensitive, insensitive)
	}
}

func TestAddReturnsTheProjectForTheSameFolder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "App")
	os.MkdirAll(dir, 0o755)
	p := store.Project{ID: store.NewID(), Name: "App", Path: dir}
	if err := store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) }); err != nil {
		t.Fatal(err)
	}
	defer (&ProjectService{}).Remove(p.ID)
	link := filepath.Join(root, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	// No artisan file in the folder, so Add doesn't ask Flock to serve it.
	got, err := (&ProjectService{}).Add(link)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != p.ID {
		t.Errorf("Add of a symlink to a project's folder made project %s, want %s", got.ID, p.ID)
	}
}

func TestDiscoverSkipsGuardedFoldersAndBundles(t *testing.T) {
	root := t.TempDir()
	writeLaravel(t, filepath.Join(root, "code", "shop"))
	writeLaravel(t, filepath.Join(root, "Documents", "crm"))
	writeLaravel(t, filepath.Join(root, "Photos Library.photoslibrary", "x"))

	got := discover([]string{root}, map[string]bool{filepath.Join(root, "Documents"): true}, 3, time.Now().Add(time.Minute))
	if len(got) != 1 || got[0].Path != filepath.Join(root, "code", "shop") {
		t.Fatalf("want only code/shop: %+v", got)
	}
}
