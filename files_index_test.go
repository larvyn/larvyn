package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"

	"larvyn/internal/proc"
)

func writeFiles(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func indexFor(t *testing.T, root string) FileIndex {
	t.Helper()
	p, err := (&ProjectService{}).Add(root)
	if err != nil {
		t.Fatal(err)
	}
	defer (&ProjectService{}).Remove(p.ID)
	idx, err := (&FileService{}).AllFiles(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func gitInit(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		if _, err := proc.Git(root, args...); err != nil {
			t.Fatal(err)
		}
	}
}

// In a git repository git decides: .gitignore is honoured, untracked files
// that are not ignored are listed, deleted tracked files and .git are not.
func TestAllFilesGitRepo(t *testing.T) {
	if proc.Which("git") == "" {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	gitInit(t, root)
	writeFiles(t, root,
		"app/Models/User.php",
		"gone.txt",
		"secret.log",
		"node_modules/pkg/index.js",
		"untracked/new.php",
		"vendor/laravel/framework/src/Foundation.php", // tracked below: git wins over the walk's rules
	)
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n/node_modules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := proc.Git(root, "add", ".gitignore", "app", "gone.txt", "vendor"); err != nil {
		t.Fatal(err)
	}
	if _, err := proc.Git(root, "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	idx := indexFor(t, root)
	want := []string{".gitignore", "app/Models/User.php", "untracked/new.php", "vendor/laravel/framework/src/Foundation.php"}
	if !slices.Equal(idx.Files, want) || idx.Truncated {
		t.Fatalf("index = %v (truncated %v), want %v", idx.Files, idx.Truncated, want)
	}
}

// Without git the tree is walked and dependencies, build output and editor
// folders are skipped.
func TestAllFilesWalk(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root,
		"artisan",
		"app/Http/Kernel.php",
		"resources/js/app.ts",
		"node_modules/pkg/index.js",
		"vendor/autoload.php",
		"storage/framework/views/abc.php",
		"storage/logs/laravel.log",
		"bootstrap/cache/config.php",
		"bootstrap/app.php",
		"public/build/manifest.json",
		"public/index.php",
		"dist/out.js",
		".idea/workspace.xml",
		".vscode/settings.json",
		".git/HEAD",
		"app/.DS_Store",
	)
	idx := indexFor(t, root)
	want := []string{"app/Http/Kernel.php", "artisan", "bootstrap/app.php", "public/index.php", "resources/js/app.ts", "storage/logs/laravel.log"}
	if !slices.Equal(idx.Files, want) || idx.Truncated {
		t.Fatalf("index = %v (truncated %v), want %v", idx.Files, idx.Truncated, want)
	}
}

func TestAllFilesCap(t *testing.T) {
	old := maxIndexedFiles
	defer func() { maxIndexedFiles = old }()
	root := t.TempDir()
	for i := range 8 {
		writeFiles(t, root, "f"+strconv.Itoa(i)+".txt")
	}
	maxIndexedFiles = 5
	idx := indexFor(t, root)
	if len(idx.Files) != 5 || !idx.Truncated || !slices.IsSorted(idx.Files) {
		t.Fatalf("index = %v (truncated %v), want 5 sorted files and truncated", idx.Files, idx.Truncated)
	}
	maxIndexedFiles = 8 // exactly at the cap is not truncated
	if idx := indexFor(t, root); len(idx.Files) != 8 || idx.Truncated {
		t.Fatalf("index = %v (truncated %v), want 8 files, not truncated", idx.Files, idx.Truncated)
	}
}

// Symlinks out of the project (or to nothing) are dropped in both modes; one
// pointing inside stays.
func TestAllFilesStaysInside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	outside := t.TempDir()
	writeFiles(t, outside, "secret.txt", "dir/inner.txt")

	check := func(t *testing.T, root string) {
		writeFiles(t, root, "real.txt")
		for name, target := range map[string]string{
			"out-file.txt": filepath.Join(outside, "secret.txt"),
			"out-dir":      filepath.Join(outside, "dir"),
			"dangling.txt": filepath.Join(root, "nope"),
			"in-link.txt":  filepath.Join(root, "real.txt"),
		} {
			if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
		}
		idx := indexFor(t, root)
		want := []string{"in-link.txt", "real.txt"}
		if !slices.Equal(idx.Files, want) {
			t.Fatalf("index = %v, want %v", idx.Files, want)
		}
	}
	t.Run("walk", func(t *testing.T) { check(t, t.TempDir()) })
	t.Run("git", func(t *testing.T) {
		if proc.Which("git") == "" {
			t.Skip("git not installed")
		}
		root := t.TempDir()
		gitInit(t, root)
		check(t, root)
	})
}
