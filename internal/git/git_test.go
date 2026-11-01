package git

import (
	"os"
	"strings"
	"testing"

	"larvyn/internal/proc"
	"larvyn/internal/store"
)

func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "larvyn-git-test")
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func TestWorktreeLifecycle(t *testing.T) {
	if proc.Which("git") == "" {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		if _, err := proc.Git(repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(repo+"/README.md", []byte("hi\n"), 0o644)
	os.WriteFile(repo+"/.env", []byte("APP_NAME=Demo\n"), 0o644)
	proc.Git(repo, "add", "README.md")
	if _, err := proc.Git(repo, "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	p := store.Project{ID: "wt", Name: "Demo", Path: repo}
	if err := store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) }); err != nil {
		t.Fatal(err)
	}
	g := &GitService{}
	wt, err := g.CreateWorktree(WorktreeRequest{ProjectID: p.ID, Name: "Add Feature!", CopyEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	if wt.Branch != "larvyn/add-feature" {
		t.Errorf("branch = %s", wt.Branch)
	}
	if b, _ := os.ReadFile(wt.Path + "/.env"); string(b) != "APP_NAME=Demo\n" {
		t.Error(".env not copied")
	}
	os.WriteFile(wt.Path+"/feature.php", []byte("<?php // agent work\n"), 0o644)
	diff, err := g.BranchDiff(wt.Path, "main")
	if err != nil || !strings.Contains(diff, "agent work") {
		t.Fatalf("diff missing new file: %v %q", err, diff)
	}
	if _, err := g.MergeWorktree(p.ID, wt.Path, "feat: add feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(repo + "/feature.php"); err != nil {
		t.Error("merge did not bring feature.php into main")
	}
	if err := g.RemoveWorktree(p.ID, wt.Path, true); err != nil {
		t.Fatal(err)
	}
	if out, _ := proc.Git(repo, "branch"); strings.Contains(out, "larvyn/") {
		t.Errorf("branch not deleted: %s", out)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{"Add Feature!": "add-feature", "  Fix N+1 in Orders ": "fix-n-1-in-orders"} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
