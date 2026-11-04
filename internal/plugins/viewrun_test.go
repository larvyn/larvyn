package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// installViewRun installs runManifest with the permissions views need to
// run things, plus a fake php that prints its arguments.
func installViewRun(t *testing.T, perms ...string) (*PluginService, store.Project) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake php is a shell script")
	}
	reset(t)
	withPrograms(t)
	granted := append([]string{"project.read", "terminal.run"}, perms...)
	src := t.TempDir()
	writePlugin(t, src, edit(t, func(m map[string]any) {
		m["permissions"] = granted
		c := contributes(m)
		c["commands"] = []any{map[string]any{"id": "hello", "title": "Say hello", "run": "echo view-ran"}}
		c["stack"] = []any{map[string]any{"key": "watch", "title": "Watcher", "argv": []string{"mailpit"}}}
	}))
	s := &PluginService{}
	if _, err := s.Install(src, granted); err != nil {
		t.Fatal(err)
	}
	proj := store.Project{ID: store.NewID(), Name: "app", Path: t.TempDir()}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, proj) })
	t.Cleanup(func() {
		store.Update(func(d *store.State) { d.Projects = nil })
		stopSessions("acme.horizon")
	})
	php := filepath.Join(t.TempDir(), "php")
	if err := os.WriteFile(php, []byte("#!/bin/sh\necho fake-php \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	store.Update(func(d *store.State) { d.Settings.PHPBinary = php })
	t.Cleanup(func() { store.Update(func(d *store.State) { d.Settings.PHPBinary = "" }) })
	return s, proj
}

// confirmOf returns the ConfirmError err holds, failing if it is another error.
func confirmOf(t *testing.T, err error) ConfirmError {
	t.Helper()
	var ce ConfirmError
	if !errors.As(err, &ce) {
		t.Fatalf("want a confirmation request, got %v", err)
	}
	return ce
}

func TestViewTerminalRun(t *testing.T) {
	s, proj := installViewRun(t)
	call := func() (ViewResult, error) {
		return s.Call("acme.horizon", proj.ID, "terminal.run", map[string]any{"id": "hello"})
	}
	ce := confirmOf(t, func() error { _, err := call(); return err }())
	if ce.Kind != "command" || ce.Key != "hello" || ce.Line != "echo view-ran" {
		t.Fatalf("confirmation: %+v", ce)
	}
	if err := s.ConfirmRun("acme.horizon", "command", "hello", "echo other"); err == nil {
		t.Fatal("confirming a line other than the plugin's must fail")
	}
	if err := s.ConfirmRun(ce.PluginID, ce.Kind, ce.Key, ce.Line); err != nil {
		t.Fatal(err)
	}
	r, err := call()
	if err != nil {
		t.Fatal(err)
	}
	if r.Session == nil || r.Session.Cwd != proj.Path {
		t.Fatalf("result: %+v", r)
	}
	waitOutput(t, r.Session.ID, "view-ran")

	// Only commands from larvyn.json: no free-form lines.
	if _, err := s.Call("acme.horizon", proj.ID, "terminal.run", map[string]any{"id": "rm -rf /"}); err == nil || !strings.Contains(err.Error(), "no command") {
		t.Errorf("an undeclared command must be refused: %v", err)
	}
}

func TestViewArtisanRun(t *testing.T) {
	s, proj := installViewRun(t)
	args := map[string]any{"args": []any{"horizon:pause"}}
	if _, err := s.Call("acme.horizon", proj.ID, "artisan.run", args); err == nil || !strings.Contains(err.Error(), "artisan.run") {
		t.Fatalf("artisan.run needs its permission: %v", err)
	}

	s, proj = installViewRun(t, "artisan.run")
	_, err := s.Call("acme.horizon", proj.ID, "artisan.run", args)
	ce := confirmOf(t, err)
	if ce.Kind != "artisan" || ce.Line != "php artisan horizon:pause" {
		t.Fatalf("confirmation: %+v", ce)
	}
	if err := s.ConfirmRun(ce.PluginID, ce.Kind, ce.Key, ce.Line); err != nil {
		t.Fatal(err)
	}
	r, err := s.Call("acme.horizon", proj.ID, "artisan.run", args)
	if err != nil {
		t.Fatal(err)
	}
	waitOutput(t, r.Session.ID, "fake-php artisan horizon:pause")

	// Another line asks again.
	_, err = s.Call("acme.horizon", proj.ID, "artisan.run", map[string]any{"args": []any{"migrate:fresh", "--force"}})
	confirmOf(t, err)

	for _, bad := range []any{nil, []any{}, []any{"--env=x"}, []any{"migrate; rm -rf /"}, []any{1}, []any{"migrate", "a\nb"}} {
		if _, err := s.Call("acme.horizon", proj.ID, "artisan.run", map[string]any{"args": bad}); err == nil || errors.As(err, new(ConfirmError)) {
			t.Errorf("args %#v must be refused: %v", bad, err)
		}
	}
}

func TestViewStackControl(t *testing.T) {
	var got []string
	old := StackControl
	StackControl = func(projectID, key, action string) (terminal.SessionInfo, error) {
		got = append(got, action+" "+key)
		return terminal.SessionInfo{ID: "s1"}, nil
	}
	t.Cleanup(func() { StackControl = old })

	s, proj := installViewRun(t)
	if _, err := s.Call("acme.horizon", proj.ID, "stack.start", map[string]any{"key": "queue"}); err == nil || !strings.Contains(err.Error(), "stack.control") {
		t.Fatalf("stack.start needs stack.control: %v", err)
	}
	s, proj = installViewRun(t, "stack.control")
	for _, m := range []string{"stack.start", "stack.restart", "stack.stop"} {
		if _, err := s.Call("acme.horizon", proj.ID, m, map[string]any{"key": "queue"}); err != nil {
			t.Fatal(m, err)
		}
	}
	if _, err := s.Call("acme.horizon", proj.ID, "stack.start", map[string]any{"key": SessionKeyPrefix("acme.horizon") + "watch"}); err != nil {
		t.Errorf("a plugin controls its own items: %v", err)
	}
	if _, err := s.Call("acme.horizon", proj.ID, "stack.stop", map[string]any{"key": "plugin:other.plugin:x"}); err == nil {
		t.Error("another plugin's items must be refused")
	}
	if _, err := s.Call("acme.horizon", proj.ID, "stack.start", map[string]any{}); err == nil {
		t.Error("a missing key must be refused")
	}
	want := "start queue,restart queue,stop queue,start plugin:acme.horizon:watch"
	if strings.Join(got, ",") != want {
		t.Errorf("stack calls: %v", got)
	}
}
