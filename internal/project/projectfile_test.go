package project

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"larvyn/internal/projectfile"
	"larvyn/internal/store"
)

func addTestProject(t *testing.T, file string) store.Project {
	t.Helper()
	dir := t.TempDir()
	if file != "" {
		os.MkdirAll(filepath.Join(dir, projectfile.Dir), 0o755)
		os.WriteFile(projectfile.Path(dir), []byte(file), 0o644)
	}
	p, err := (&ProjectService{}).Add(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { (&ProjectService{}).Remove(p.ID) })
	return p
}

func ids(list []ProjectCommand) (out []string) {
	for _, c := range list {
		s := c.ID
		if c.Shared {
			s += "*"
		}
		out = append(out, s)
	}
	return out
}

func TestCommandsMergeLocalAndShared(t *testing.T) {
	s := &ProjectService{}
	p := addTestProject(t, `{"apiVersion": 1, "commands": [
		{"id": "fresh", "title": "Fresh", "run": "php artisan migrate:fresh"},
		{"id": "lint", "title": "Lint", "run": "vendor/bin/pint"}]}`)
	if _, err := s.SaveCommand(p.ID, ProjectCommand{Title: "My Seed!", Run: "php artisan db:seed"}); err != nil {
		t.Fatal(err)
	}
	// A local command with a shared one's id replaces it for this user.
	if _, err := s.SaveCommand(p.ID, ProjectCommand{ID: "lint", Title: "Lint (mine)", Run: "vendor/bin/pint --test"}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Commands(p.ID)
	if got := ids(list); len(got) != 3 || got[0] != "fresh*" || got[1] != "my-seed" || got[2] != "lint" {
		t.Fatalf("got %v", got)
	}
	// The same title again gets a free id.
	c, _ := s.SaveCommand(p.ID, ProjectCommand{Title: "My seed", Run: "x"})
	if c.ID != "my-seed-2" {
		t.Fatalf("got id %q", c.ID)
	}
	if _, err := s.SaveCommand(p.ID, ProjectCommand{Title: "Nothing"}); err == nil {
		t.Fatal("a command with nothing to run should be refused")
	}
}

func TestMoveCommandBetweenLocalAndShared(t *testing.T) {
	s := &ProjectService{}
	p := addTestProject(t, "")
	if _, err := s.SaveCommand(p.ID, ProjectCommand{ID: "seed", Title: "Seed", Run: "php artisan db:seed"}); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveCommand(p.ID, "seed", true); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Commands(p.ID)
	if got := ids(list); len(got) != 1 || got[0] != "seed*" {
		t.Fatalf("after sharing: %v", got)
	}
	if l := projectfile.Load(p.Path); len(l.File.Commands) != 1 || l.File.Commands[0].Run != "php artisan db:seed" {
		t.Fatalf("file: %+v", l)
	}
	if err := s.MoveCommand(p.ID, "seed", false); err != nil {
		t.Fatal(err)
	}
	list, _ = s.Commands(p.ID)
	if got := ids(list); len(got) != 1 || got[0] != "seed" {
		t.Fatalf("after moving back: %v", got)
	}
	if l := projectfile.Load(p.Path); len(l.File.Commands) != 0 {
		t.Fatalf("still shared: %+v", l.File.Commands)
	}
	if err := s.DeleteCommand(p.ID, "seed", false); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Commands(p.ID); len(list) != 0 {
		t.Fatalf("not deleted: %v", ids(list))
	}
}

func TestFileReportsPlugins(t *testing.T) {
	old := PluginCheck
	PluginCheck = func(id, _, _ string) string {
		if id == "larvyn.mailpit" {
			return ""
		}
		return "missing"
	}
	defer func() { PluginCheck = old }()
	p := addTestProject(t, `{"apiVersion": 1, "plugins": ["larvyn.mailpit", "larvyn.horizon"], "commands": [{"id": "x"}]}`)
	info, err := (&ProjectService{}).File(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Exists || len(info.Plugins) != 2 || info.Plugins[0].Problem != "" || info.Plugins[1].Problem != "missing" || len(info.Warnings) != 1 {
		t.Fatalf("got %+v", info)
	}
}

// A move whose second half fails leaves both sides as they were.
func TestFailedMoveIsUndone(t *testing.T) {
	// Windows ignores a folder's read-only bit, and root ignores it anywhere.
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("read-only folders don't stop writes here")
	}
	s := &ProjectService{}
	p := addTestProject(t, `{"apiVersion": 1, "commands": [{"id": "seed", "title": "Team seed", "run": "php artisan db:seed"}]}`)
	if _, err := s.SaveCommand(p.ID, ProjectCommand{ID: "mine", Title: "Mine", Run: "echo mine"}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() []string {
		list, _ := s.Commands(p.ID)
		var out []string
		for _, c := range list {
			out = append(out, fmt.Sprintf("%s:%v:%s", c.ID, c.Shared, c.Run))
		}
		return out
	}
	want := fmt.Sprint(snapshot())

	// Shared → mine, but the project file can't be written: the local copy goes.
	dir := filepath.Join(p.Path, projectfile.Dir)
	os.Chmod(dir, 0o500)
	err := s.MoveCommand(p.ID, "seed", false)
	os.Chmod(dir, 0o755)
	if err == nil {
		t.Fatal("the move should fail")
	}
	if got := fmt.Sprint(snapshot()); got != want {
		t.Fatalf("not undone:\n got %s\nwant %s", got, want)
	}

	// Mine → shared, but the state can't be saved: the file goes back.
	home := store.HomeDir()
	os.Chmod(home, 0o500)
	err = s.MoveCommand(p.ID, "mine", true)
	os.Chmod(home, 0o755)
	if err == nil {
		t.Fatal("the move should fail")
	}
	if got := fmt.Sprint(snapshot()); got != want {
		t.Fatalf("not undone:\n got %s\nwant %s", got, want)
	}
	if l := projectfile.Load(p.Path); len(l.File.Commands) != 1 || l.File.Commands[0].ID != "seed" {
		t.Fatalf("file: %+v", l.File.Commands)
	}
}
