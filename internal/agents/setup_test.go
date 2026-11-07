package agents

import (
	"os"
	"path/filepath"
	"testing"

	"larvyn/internal/projectfile"
)

func TestWorktreeSetupPrefersTheBranchFile(t *testing.T) {
	write := func(dir, body string) {
		os.MkdirAll(filepath.Join(dir, projectfile.Dir), 0o755)
		os.WriteFile(projectfile.Path(dir), []byte(body), 0o644)
	}
	root, wt := t.TempDir(), t.TempDir()
	write(root, `{"apiVersion": 1, "worktree": {"setup": ["composer install"]}}`)
	// No file in the worktree yet (not committed): the main folder's.
	if got := worktreeSetup(wt, root); len(got) != 1 || got[0] != "composer install" {
		t.Fatalf("got %q", got)
	}
	// The branch's own file wins, even when it has no setup.
	other := t.TempDir()
	write(other, `{"apiVersion": 1}`)
	if got := worktreeSetup(other, root); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestWorktreeSetupSkipsABrokenBranchFile(t *testing.T) {
	root, wt := t.TempDir(), t.TempDir()
	for dir, body := range map[string]string{root: `{"apiVersion": 1, "worktree": {"setup": ["composer install"]}}`, wt: `{"apiVersion": 1,`} {
		os.MkdirAll(filepath.Join(dir, projectfile.Dir), 0o755)
		os.WriteFile(projectfile.Path(dir), []byte(body), 0o644)
	}
	if got := worktreeSetup(wt, root); len(got) != 1 || got[0] != "composer install" {
		t.Fatalf("a branch file that doesn't parse should fall back to the main folder's: %q", got)
	}
}
