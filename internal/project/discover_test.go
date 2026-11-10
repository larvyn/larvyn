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
	// A site named like a bundle is still a project; an app bundle is not
	// walked into.
	writeLaravel(t, filepath.Join(root, "code", "acme.app"))
	writeLaravel(t, filepath.Join(root, "Tool.app", "Contents"))

	got := discover([]string{root}, map[string]bool{filepath.Join(root, "Documents"): true}, 3, time.Now().Add(time.Minute))
	paths := map[string]bool{}
	for _, f := range got {
		paths[f.Path] = true
	}
	if len(got) != 2 || !paths[filepath.Join(root, "code", "shop")] || !paths[filepath.Join(root, "code", "acme.app")] {
		t.Fatalf("want code/shop and code/acme.app: %+v", got)
	}
}

func TestStackSitePathsReadsValetConfig(t *testing.T) {
	home := t.TempDir()
	sites := filepath.Join(t.TempDir(), "sites")
	writeLaravel(t, filepath.Join(sites, "shop"))
	os.MkdirAll(filepath.Join(home, ".config", "valet"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "valet", "config.json"), []byte(`{"tld":"test","paths":["`+filepath.ToSlash(sites)+`","relative"]}`), 0o644)

	paths := stackSitePaths(home)
	if len(paths) != 1 || !sameFolder(paths[0], sites) {
		t.Fatalf("want the parked folder only: %v", paths)
	}
	// A guarded parked folder is not even looked at.
	roots := discoverRoots(home, map[string]bool{sites: true})
	for _, r := range roots {
		if within(r, sites) {
			t.Fatalf("guarded root kept: %v", roots)
		}
	}
	got := discover(paths, nil, 3, time.Now().Add(time.Minute))
	if len(got) != 1 || !sameFolder(got[0].Path, filepath.Join(sites, "shop")) {
		t.Fatalf("want the app in the parked folder: %+v", got)
	}
}

func TestWithin(t *testing.T) {
	d := filepath.Join(string(filepath.Separator)+"home", "me", "Documents")
	for path, want := range map[string]bool{
		d:                              true,
		filepath.Join(d, "app"):        true,
		d + "-old":                     false,
		filepath.Join(filepath.Dir(d)): false,
	} {
		if got := within(path, d); got != want {
			t.Errorf("within(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestResolveUnguardedSkipsSymlinksIntoGuardedFolders(t *testing.T) {
	home := t.TempDir()
	docs := filepath.Join(home, "Documents")
	os.MkdirAll(filepath.Join(docs, "sites"), 0o755)
	os.MkdirAll(filepath.Join(home, "code"), 0o755)
	guarded := map[string]bool{docs: true}

	// A link straight into Documents, a relative one, and a link to a
	// folder that itself links there.
	os.Symlink(filepath.Join(docs, "sites"), filepath.Join(home, "Herd"))
	os.Symlink(filepath.Join("Documents", "sites"), filepath.Join(home, "Sites"))
	os.Symlink(filepath.Join(home, "Sites"), filepath.Join(home, "www"))
	for _, name := range []string{"Herd", "Sites", "www"} {
		if p, ok := resolveUnguarded(filepath.Join(home, name), guarded); ok {
			t.Errorf("%s resolved to %s, want it left out", name, p)
		}
	}

	os.Symlink("code", filepath.Join(home, "dev"))
	want, _ := filepath.EvalSymlinks(filepath.Join(home, "code"))
	if p, ok := resolveUnguarded(filepath.Join(home, "dev"), guarded); !ok || p != want {
		t.Errorf("dev = %q, %v; want %q", p, ok, want)
	}
	if _, ok := resolveUnguarded(filepath.Join(home, "missing"), guarded); ok {
		t.Error("a missing folder resolved")
	}
	// A symlink loop ends.
	os.Symlink("loop-b", filepath.Join(home, "loop-a"))
	os.Symlink("loop-a", filepath.Join(home, "loop-b"))
	if _, ok := resolveUnguarded(filepath.Join(home, "loop-a"), guarded); ok {
		t.Error("a symlink loop resolved")
	}
}
