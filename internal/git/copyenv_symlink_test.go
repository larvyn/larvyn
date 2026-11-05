package git

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"larvyn/internal/proc"
	"larvyn/internal/store"
)

// A branch that commits .env (or the database folder) as a symlink out of the
// checkout must not get the target overwritten when a worktree copies the
// main checkout's files into it (#51).
func TestCreateWorktreeDoesNotWriteThroughSymlinks(t *testing.T) {
	if proc.Which("git") == "" {
		t.Skip("git not installed")
	}
	outside := t.TempDir()
	envTarget := filepath.Join(outside, "bashrc")
	os.WriteFile(envTarget, []byte("keep me\n"), 0o644)

	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		proc.Git(repo, args...)
	}
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644)
	proc.Git(repo, "add", "README.md")
	proc.Git(repo, "commit", "-m", "init")
	// The evil branch commits .env and database/ as symlinks out.
	proc.Git(repo, "checkout", "-q", "-b", "evil")
	if err := os.Symlink(envTarget, filepath.Join(repo, ".env")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	os.Symlink(outside, filepath.Join(repo, "database"))
	proc.Git(repo, "add", "-f", ".env", "database")
	if _, err := proc.Git(repo, "commit", "-m", "evil"); err != nil {
		t.Fatal(err)
	}
	proc.Git(repo, "checkout", "-q", "main")
	// The main checkout has a real .env and sqlite database to copy.
	os.WriteFile(filepath.Join(repo, ".env"), []byte("DB_CONNECTION=sqlite\nSECRET=1\n"), 0o600)
	os.MkdirAll(filepath.Join(repo, "database"), 0o755)
	os.WriteFile(filepath.Join(repo, "database", "database.sqlite"), []byte("db"), 0o644)

	p := store.Project{ID: "evil", Name: "Evil", Path: repo}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) })
	if _, err := (&GitService{}).CreateWorktree(WorktreeRequest{ProjectID: p.ID, Name: "x", Base: "evil", CopyEnv: true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(envTarget); string(b) != "keep me\n" {
		t.Errorf(".env copy went through the symlink: %q", b)
	}
	if _, err := os.Stat(filepath.Join(outside, "database.sqlite")); err == nil {
		t.Error("database copy went through the symlinked folder")
	}
}

func TestCreateWorktreeKeepsEnvPrivate(t *testing.T) {
	if proc.Which("git") == "" {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		proc.Git(repo, args...)
	}
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644)
	proc.Git(repo, "add", "README.md")
	proc.Git(repo, "commit", "-m", "init")
	os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1\n"), 0o600)
	p := store.Project{ID: "private", Name: "Private", Path: repo}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) })
	wt, err := (&GitService{}).CreateWorktree(WorktreeRequest{ProjectID: p.ID, Name: "y", CopyEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(wt.Path, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf(".env mode = %v, want 0600", fi.Mode().Perm())
	}
}

// A branch that tracks a world-readable .env gets it replaced by the main
// checkout's private one, and the mode follows.
func TestCreateWorktreeTightensTrackedEnv(t *testing.T) {
	if proc.Which("git") == "" {
		t.Skip("git not installed")
	}
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		proc.Git(repo, args...)
	}
	os.WriteFile(filepath.Join(repo, ".env"), []byte("TRACKED=1\n"), 0o644)
	proc.Git(repo, "add", "-f", ".env")
	proc.Git(repo, "commit", "-m", "tracked env")
	os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1\n"), 0o644)
	os.Chmod(filepath.Join(repo, ".env"), 0o600)
	p := store.Project{ID: "tracked", Name: "Tracked", Path: repo}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) })
	wt, err := (&GitService{}).CreateWorktree(WorktreeRequest{ProjectID: p.ID, Name: "z", CopyEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(wt.Path, ".env")
	if b, _ := os.ReadFile(dst); string(b) != "SECRET=1\n" {
		t.Fatalf(".env = %q", b)
	}
	if fi, _ := os.Stat(dst); fi.Mode().Perm() != 0o600 {
		t.Errorf(".env mode = %v, want 0600", fi.Mode().Perm())
	}
}
