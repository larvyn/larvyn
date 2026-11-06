package git

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"larvyn/internal/proc"
	"larvyn/internal/store"
)

func newRepo(t *testing.T) string {
	t.Helper()
	if proc.Which("git") == "" {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	// core.autocrlf off: Windows runners turn it on globally, and a restored
	// file would come back with CRLF line endings.
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}, {"config", "core.autocrlf", "false"}} {
		if _, err := proc.Git(repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644)
	proc.Git(repo, "add", "README.md")
	if _, err := proc.Git(repo, "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	return repo
}

// A file whose name contains " -> " used to be read as a rename to "..", and
// Discard then deleted the project's parent folder (#51).
func TestDiscardRenameLookalikeStaysInside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows drops trailing dots from file names: x -> .. can't exist there")
	}
	parent := t.TempDir()
	repo := filepath.Join(parent, "app")
	os.Rename(newRepo(t), repo)
	sibling := filepath.Join(parent, "other-project")
	os.MkdirAll(sibling, 0o755)
	odd := "x -> .."
	os.WriteFile(filepath.Join(repo, odd), []byte("x"), 0o644)

	g := &GitService{}
	st, err := g.Status(repo)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range st.Files {
		if f.Path == ".." {
			t.Fatalf("status reported %q", f.Path)
		}
		found = found || f.Path == odd
	}
	if !found {
		t.Fatalf("status files = %+v, want %q", st.Files, odd)
	}
	if err := g.Discard(repo, odd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, odd)); !os.IsNotExist(err) {
		t.Error("untracked file not removed")
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatal("Discard deleted outside the repository")
	}
}

func TestDiscardRefusesPathsOutside(t *testing.T) {
	repo := newRepo(t)
	g := &GitService{}
	for _, p := range []string{"..", "../x", "/etc", "", ".", "a/../../x"} {
		if err := g.Discard(repo, p); err == nil {
			t.Errorf("Discard(%q) succeeded", p)
		}
	}
	if _, err := os.Stat(repo); err != nil {
		t.Fatal(err)
	}
}

// A path git doesn't list (unknown, or git failing) is never deleted.
func TestDiscardDoesNotDeleteUnlistedPaths(t *testing.T) {
	repo := newRepo(t)
	ignored := filepath.Join(repo, "keep.log")
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("*.log\n"), 0o644)
	os.WriteFile(ignored, []byte("x"), 0o644)
	_ = (&GitService{}).Discard(repo, "keep.log")
	if _, err := os.Stat(ignored); err != nil {
		t.Fatal("deleted a file git does not list as untracked")
	}
}

func TestDiscardRestoresTrackedAndHandlesSubfolderProject(t *testing.T) {
	repo := newRepo(t)
	os.MkdirAll(filepath.Join(repo, "web"), 0o755)
	os.WriteFile(filepath.Join(repo, "web", "a.txt"), []byte("one\n"), 0o644)
	proc.Git(repo, "add", ".")
	proc.Git(repo, "commit", "-m", "web")
	os.WriteFile(filepath.Join(repo, "web", "a.txt"), []byte("changed\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "web", "new.txt"), []byte("n"), 0o644)

	// The project is the web/ subfolder; status paths are repo-relative.
	p := store.Project{ID: "sub", Name: "Sub", Path: filepath.Join(repo, "web")}
	if err := store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) }); err != nil {
		t.Fatal(err)
	}
	g := &GitService{}
	if err := g.Discard(p.ID, "web/a.txt"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "web", "a.txt")); string(b) != "one\n" {
		t.Errorf("a.txt = %q", b)
	}
	if err := g.Discard(p.ID, "web/new.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "web", "new.txt")); !os.IsNotExist(err) {
		t.Error("untracked file not removed")
	}
}

func TestParseStatusZRename(t *testing.T) {
	files := parseStatusZ("## main\x00R  new name\x00old -> name\x00?? a\"b\x00", nil)
	if len(files) != 2 || files[0].Path != "new name" || files[1].Path != `a"b` {
		t.Fatalf("files = %+v", files)
	}
	// A worktree-column rename (intent-to-add file) has its source too.
	files = parseStatusZ(" R renamed.txt\x00orig.txt\x00?? new.txt\x00", nil)
	if len(files) != 2 || files[0].Path != "renamed.txt" || files[1].Path != "new.txt" {
		t.Fatalf("worktree rename: files = %+v", files)
	}
	stats := parseNumstatZ("1\t2\t\x00old\x00new\x003\t0\tplain\x00")
	if stats["new"] != [2]int{1, 2} || stats["plain"] != [2]int{3, 0} {
		t.Fatalf("stats = %v", stats)
	}
}
