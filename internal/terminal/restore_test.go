package terminal

import (
	"bytes"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"larvyn/internal/store"
)

func TestRemapLayoutDropsMissingPanes(t *testing.T) {
	in := map[string]ProjectLayout{
		"p1": {Active: "b", Tabs: []TabLayout{
			{ID: "a", Panes: []string{"s1", "gone"}, Dir: "col"},
			{ID: "b", Panes: []string{"gone"}, Dir: "row"},
		}},
		"p2": {Active: "c", Tabs: []TabLayout{{ID: "c", Panes: []string{"gone"}}}},
	}
	got := remapLayout(in, func(id string) (string, bool) {
		if id == "s1" {
			return "n1", true
		}
		return "", false
	})
	want := map[string]ProjectLayout{
		"p1": {Active: "a", Tabs: []TabLayout{{ID: "a", Panes: []string{"n1"}, Dir: "col"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRestoredScrollbackStartsAtALine(t *testing.T) {
	if restoredScrollback(nil, 0) != nil {
		t.Fatal("no output should restore nothing")
	}
	big := append(bytes.Repeat([]byte("x"), restoreTailBytes), []byte("\x1b[31mcut\nkept line\n")...)
	got := string(restoredScrollback(big, time.Now().UnixMilli()))
	if !strings.HasPrefix(got, "kept line\n") {
		t.Fatalf("tail should start after the first newline, got %q", got[:min(40, len(got))])
	}
	if !strings.Contains(got, "\x1b[?1049l") || !strings.Contains(got, "restored from the previous session") {
		t.Fatalf("missing reset or marker: %q", got)
	}
}

// TestWorkspaceRoundTrip saves a shell, a one-off run and its layout, then
// restores them in a fresh service: the shell runs again below its old
// output, the run only shows its output, and the layout follows the new ids.
func TestWorkspaceRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	t.Setenv("LARVYN_HOME", t.TempDir())
	store.Reload()
	t.Cleanup(store.Reload)
	dir := t.TempDir()
	_ = store.Update(func(d *store.State) {
		d.Projects = []store.Project{{ID: "p1", Name: "app", Path: dir}, {ID: "p2", Name: "gone", Path: dir}}
		d.Settings.Shell = "/bin/sh"
	})

	a := &TerminalService{sessions: map[string]*session{}, persist: true}
	a.Restore() // nothing saved yet; allows saving
	shell, err := a.Start(SessionSpec{ProjectID: "p1", Kind: "shell", Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	run, err := a.Start(SessionSpec{ProjectID: "p1", Kind: "run", Argv: []string{"/bin/sh", "-c", "echo hello-from-run"}, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := a.Start(SessionSpec{ProjectID: "p2", Kind: "shell", Cwd: dir})
	waitFor(t, func() bool {
		out, _ := a.Output(run.ID)
		return strings.Contains(out, "hello-from-run") && !a.find("p1", "run", "").Running
	})
	_ = a.Write(shell.ID, "echo marker-$((40+2))\n")
	waitFor(t, func() bool { out, _ := a.Output(shell.ID); return strings.Contains(out, "marker-42") })
	a.SetLayout(map[string]ProjectLayout{"p1": {Active: "t2", Tabs: []TabLayout{
		{ID: "t1", Panes: []string{shell.ID, run.ID}, Dir: "col"},
		{ID: "t2", Panes: []string{other.ID}},
	}}})
	if err := a.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(workspacePath()); err != nil || fi.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows" {
		t.Fatalf("workspace.json missing or readable by others: %v %v", fi, err)
	}

	// p2 disappears between runs: its terminal is skipped.
	_ = store.Update(func(d *store.State) { d.Projects = d.Projects[:1] })
	b := &TerminalService{sessions: map[string]*session{}, persist: true}
	defer b.ServiceShutdown()
	layout := b.Restore()
	list := b.List()
	if len(list) != 2 {
		t.Fatalf("want 2 restored sessions, got %+v", list)
	}
	var newShell, newRun SessionInfo
	for _, s := range list {
		if s.Kind == "shell" {
			newShell = s
		} else {
			newRun = s
		}
	}
	if !newShell.Running {
		t.Error("the shell should run again")
	}
	if newRun.Running || !newRun.Restored {
		t.Errorf("the run must not run again: %+v", newRun)
	}
	out, _ := b.Output(newRun.ID)
	if !strings.Contains(out, "hello-from-run") || !strings.Contains(out, "restored from the previous session") {
		t.Errorf("run output not restored: %q", out)
	}
	out, _ = b.Output(newShell.ID)
	if !strings.Contains(out, "marker-42") {
		t.Errorf("shell output not restored: %q", out)
	}
	want := map[string]ProjectLayout{"p1": {Active: "t1", Tabs: []TabLayout{{ID: "t1", Panes: []string{newShell.ID, newRun.ID}, Dir: "col"}}}}
	if !reflect.DeepEqual(layout, want) {
		t.Errorf("layout %+v, want %+v", layout, want)
	}

	// Relaunch runs the command again in the same session.
	if _, err := b.Relaunch(newRun.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		out, _ := b.Output(newRun.ID)
		return strings.Count(out, "hello-from-run") == 2
	})
}

func TestNoRestoreSettingSkipsAndDeletes(t *testing.T) {
	t.Setenv("LARVYN_HOME", t.TempDir())
	store.Reload()
	t.Cleanup(store.Reload)
	_ = os.WriteFile(workspacePath(), []byte(`{"version":1,"sessions":[]}`), 0o600)
	_ = store.Update(func(d *store.State) { d.Settings.NoRestore = true })
	ts := &TerminalService{sessions: map[string]*session{}, persist: true}
	ts.Restore()
	if _, err := os.Stat(workspacePath()); !os.IsNotExist(err) {
		t.Fatal("workspace.json should be removed when restore is off")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
