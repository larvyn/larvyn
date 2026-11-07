package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"larvyn/internal/proc"
	"larvyn/internal/store"
)

// An untracked file shows as all-new; an untracked symlink is not followed
// (it could lead to /dev/zero or out of the repository) (#54).
func TestDiffUntracked(t *testing.T) {
	if proc.Which("git") == "" {
		t.Skip("git not installed")
	}
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("TOKEN=1\n"), 0o644)
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		proc.Git(repo, args...)
	}
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644)
	proc.Git(repo, "add", "README.md")
	proc.Git(repo, "commit", "-m", "init")
	p := store.Project{ID: store.NewID(), Name: "d", Path: repo}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) })

	os.WriteFile(filepath.Join(repo, "new.txt"), []byte("a\nb\n"), 0o644)
	out, err := (&GitService{}).Diff(p.ID, "new.txt", false)
	if err != nil || !strings.Contains(out, "+a\n+b\n") {
		t.Fatalf("untracked file: %q %v", out, err)
	}

	if err := os.Symlink(outside, filepath.Join(repo, "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	out, _ = (&GitService{}).Diff(p.ID, "link", false)
	if strings.Contains(out, "TOKEN") {
		t.Fatalf("followed the symlink: %q", out)
	}
}
