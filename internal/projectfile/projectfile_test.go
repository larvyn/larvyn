package projectfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeFile(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, Name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Each write must look changed, even within the file system's mtime granularity.
	later := time.Now().Add(time.Duration(len(body)) * time.Second)
	_ = os.Chtimes(path, later, later)
}

func TestParseSkipsWrongEntries(t *testing.T) {
	f, warnings, err := Parse([]byte(`{
		"apiVersion": 1,
		"commands": [
			{"id": "fresh", "title": "Fresh", "run": "php artisan migrate:fresh --seed"},
			{"id": "Bad Id", "title": "x", "run": "x"},
			{"id": "fresh", "title": "Again", "run": "x"},
			{"id": "notitle", "run": "x"},
			{"id": "extra", "title": "x", "run": "x", "shell": "zsh"}
		],
		"stack": [{"key": "mail", "title": "Mailpit", "argv": ["mailpit"]}, {"key": "empty", "title": "x", "argv": []}],
		"plugins": ["larvyn.mailpit", {"id": "larvyn.horizon", "version": ">=1.0.0"}, {"id": "nodot"}, {"id": "a.b", "version": "latest"}],
		"worktree": {"setup": ["composer install", "", 3]},
		"future": true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Commands) != 1 || f.Commands[0].ID != "fresh" {
		t.Fatalf("commands: %+v", f.Commands)
	}
	if len(f.Stack) != 1 || f.Stack[0].Key != "mail" {
		t.Fatalf("stack: %+v", f.Stack)
	}
	if len(f.Plugins) != 2 || f.Plugins[0] != (Plugin{ID: "larvyn.mailpit"}) || f.Plugins[1].Version != ">=1.0.0" {
		t.Fatalf("plugins: %+v", f.Plugins)
	}
	if len(f.Worktree.Setup) != 1 || f.Worktree.Setup[0] != "composer install" {
		t.Fatalf("setup: %+v", f.Worktree.Setup)
	}
	// 4 commands, 1 stack item, 2 plugins, 2 setup lines and the unknown field.
	if len(warnings) != 10 {
		t.Fatalf("want 10 warnings, got %d: %q", len(warnings), warnings)
	}
}

func TestParseRefusesTheFile(t *testing.T) {
	for _, body := range []string{`{`, `[]`, `{"commands": []}`, `{"apiVersion": 2}`, `null`} {
		if _, _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: want an error", body)
		}
	}
	if _, _, err := Parse([]byte(`{"apiVersion": 2}`)); err == nil || !strings.Contains(err.Error(), "update Larvyn") {
		t.Errorf("a newer file should ask to update Larvyn: %v", err)
	}
}

func TestLoadKeepsTheLastGoodVersion(t *testing.T) {
	root := t.TempDir()
	if l := Load(root); l.Exists || l.Error != "" {
		t.Fatalf("no file: %+v", l)
	}
	Changed(root) // the watcher's first look
	writeFile(t, root, `{"apiVersion": 1, "commands": [{"id": "a", "title": "A", "run": "echo a"}]}`)
	if !Changed(root) {
		t.Fatal("a new file should count as a change")
	}
	if l := Load(root); !l.Exists || len(l.File.Commands) != 1 {
		t.Fatalf("got %+v", l)
	}
	writeFile(t, root, `{"apiVersion": 1, "commands": [`)
	l := Load(root)
	if l.Error == "" || len(l.File.Commands) != 1 || l.File.Commands[0].ID != "a" {
		t.Fatalf("a broken file should keep the last version: %+v", l)
	}
	if !Changed(root) || Changed(root) {
		t.Fatal("the broken write is one change, reported once")
	}
	os.Remove(Path(root))
	if !Changed(root) || Load(root).Exists || len(Load(root).File.Commands) != 0 {
		t.Fatalf("a removed file should load as none: %+v", Load(root))
	}
}

func TestLoadRefusesASymlink(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, Dir), 0o755)
	secret := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(secret, []byte(`{"apiVersion": 1}`), 0o644)
	if err := os.Symlink(secret, Path(root)); err != nil {
		t.Skip("no symlinks here")
	}
	if l := Load(root); l.Error == "" {
		t.Fatalf("a symlinked file should not load: %+v", l)
	}
}

func TestEditCommandsKeepsTheRest(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, `{
  "apiVersion": 1,
  "stack": [{"key": "mail", "title": "Mailpit", "argv": ["mailpit"]}],
  "commands": [
    {"id": "a", "title": "A", "run": "echo a"},
    {"id": "Broken!", "title": "keep me"},
    {"id": "b", "title": "B", "run": "echo b"}
  ],
  "future": {"x": 1}
}`)
	err := EditCommands(root, func(list []Command) ([]Command, error) {
		if len(list) != 2 {
			t.Fatalf("want the 2 valid commands, got %+v", list)
		}
		list[0].Run = "echo A"
		return append(list[1:2:2], list[0], Command{ID: "c", Title: "C", Run: "echo c"}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path(root))
	got := string(b)
	for _, want := range []string{`"future": {`, `"Broken!"`, `"echo A"`, `"id": "c"`, `"mailpit"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	// Fields and entries stay in their order; a changed command stays in place.
	if !(strings.Index(got, `"stack"`) < strings.Index(got, `"commands"`) &&
		strings.Index(got, `"echo A"`) < strings.Index(got, `"Broken!"`) &&
		strings.Index(got, `"Broken!"`) < strings.Index(got, `"echo b"`) &&
		strings.Index(got, `"echo b"`) < strings.Index(got, `"echo c"`)) {
		t.Errorf("order changed:\n%s", got)
	}
	if l := Load(root); len(l.File.Commands) != 3 {
		t.Fatalf("reloaded: %+v", l)
	}
}

func TestEditCommandsCreatesAndRefuses(t *testing.T) {
	root := t.TempDir()
	if err := EditCommands(root, func(list []Command) ([]Command, error) {
		return append(list, Command{ID: "a", Title: "A", Run: "echo a"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	if l := Load(root); l.Error != "" || len(l.File.Commands) != 1 {
		t.Fatalf("new file: %+v", l)
	}
	if err := EditCommands(root, func(list []Command) ([]Command, error) {
		return append(list, Command{ID: "a", Title: "dup", Run: "x"}), nil
	}); err == nil {
		t.Fatal("a duplicate id should be refused")
	}
	if err := EditCommands(root, func([]Command) ([]Command, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(Path(root)); strings.Contains(string(b), "commands") {
		t.Fatalf("an empty list should drop the field: %s", b)
	}
	writeFile(t, root, `{"apiVersion": 1,`)
	if err := EditCommands(root, func(l []Command) ([]Command, error) { return l, nil }); err == nil || !strings.Contains(err.Error(), "fix") {
		t.Fatalf("a broken file must not be overwritten: %v", err)
	}
}

// The example in the docs must load cleanly.
func TestExampleLoads(t *testing.T) {
	l := Load(filepath.Join("..", "..", "examples", "project"))
	if !l.Exists || l.Error != "" || len(l.Warnings) != 0 {
		t.Fatalf("examples/project: %+v", l)
	}
	if len(l.File.Commands) == 0 || len(l.File.Stack) == 0 || len(l.File.Plugins) == 0 || len(l.File.Worktree.Setup) == 0 {
		t.Fatalf("the example should show every section: %+v", l.File)
	}
}

// A Load between two changes must not hide the change from the watcher.
func TestLoadDoesNotTakeTheWatchersChange(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, `{"apiVersion": 1}`)
	if Changed(root) {
		t.Fatal("the first look is no change")
	}
	writeFile(t, root, `{"apiVersion": 1, "commands": [{"id": "a", "title": "A", "run": "x"}]}`)
	if len(Load(root).File.Commands) != 1 {
		t.Fatal("Load should read the new file")
	}
	if !Changed(root) {
		t.Fatal("the watcher should still see the change")
	}
	if Changed(root) {
		t.Fatal("reported twice")
	}
	if err := EditCommands(root, func([]Command) ([]Command, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if !Changed(root) {
		t.Fatal("an edit through Larvyn should count as a change too")
	}
}

// An edit between the window's first read and the watcher's first look is
// still a change.
func TestChangeBeforeTheWatchersFirstLook(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, `{"apiVersion": 1}`)
	Load(root)
	writeFile(t, root, `{"apiVersion": 1, "commands": [{"id": "a", "title": "A", "run": "x"}]}`)
	if !Changed(root) {
		t.Fatal("the edit after the first read should be reported")
	}
}

func TestConcurrentEditsKeepEveryCommand(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = EditCommands(root, func(list []Command) ([]Command, error) {
				return append(list, Command{ID: fmt.Sprintf("c%d", i), Title: "C", Run: "x"}), nil
			})
		}(i)
	}
	wg.Wait()
	if l := Load(root); len(l.File.Commands) != 20 {
		t.Fatalf("lost edits: %d commands", len(l.File.Commands))
	}
}

// An edit through Larvyn, then the file breaks before the next read: the
// last version that loaded is kept, not nothing.
func TestBrokenFileAfterAnEditKeepsTheLastLoad(t *testing.T) {
	root := t.TempDir()
	if err := EditCommands(root, func(list []Command) ([]Command, error) {
		return append(list, Command{ID: "a", Title: "A", Run: "x"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	Load(root)
	if err := EditCommands(root, func(list []Command) ([]Command, error) {
		return append(list, Command{ID: "b", Title: "B", Run: "x"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, `{"apiVersion": 1,`)
	if l := Load(root); l.Error == "" || len(l.File.Commands) == 0 {
		t.Fatalf("the last good version should stay: %+v", l)
	}
}
