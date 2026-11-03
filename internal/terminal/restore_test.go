package terminal

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"larvyn/internal/proc"
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
	if !strings.Contains(got, "\x1b[?1004l") {
		t.Fatalf("focus reporting should be turned off: %q", got)
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

func TestCleanCutSkipsPartialSequences(t *testing.T) {
	cases := map[string]string{
		"tail\nnext line":  "next line",
		"5;10H\x1b[2Jdraw": "\x1b[2Jdraw",
		"\xa9\xa9rest":     "rest", // continuation bytes of a cut character
	}
	for in, want := range cases {
		if got := string(cleanCut([]byte(in))); got != want {
			t.Errorf("cleanCut(%q) = %q, want %q", in, got, want)
		}
	}
}

// Two quick clicks on Relaunch must start one process, not two.
func TestRelaunchTwiceStartsOneProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	ts := &TerminalService{sessions: map[string]*session{}}
	defer ts.ServiceShutdown()
	info, err := ts.Start(SessionSpec{Kind: "run", Argv: []string{"/bin/sh", "-c", "sleep 30"}, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_ = ts.Kill(info.ID)
	waitFor(t, func() bool { s, _ := ts.get(info.ID); return !s.snapshotInfo().Running })

	errs := make(chan error, 2)
	for range 2 {
		go func() { _, err := ts.Relaunch(info.ID); errs <- err }()
	}
	ok := 0
	for range 2 {
		if <-errs == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("%d relaunches succeeded, want exactly 1", ok)
	}
}

func TestSharesFolderLeavesTheLatestAgent(t *testing.T) {
	dir := t.TempDir()
	mk := func(key, cwd string, last int64) *session {
		return &session{info: SessionInfo{Kind: "agent", Key: key, Cwd: cwd, LastActivity: last}}
	}
	old, latest, other, elsewhere := mk("opencode", dir, 1), mk("opencode", dir, 5), mk("pi", dir, 9), mk("opencode", t.TempDir(), 9)
	all := []*session{old, latest, other, elsewhere}
	if !sharesFolder(old, all) {
		t.Error("the older opencode shares its folder with a later one")
	}
	if sharesFolder(latest, all) || sharesFolder(other, all) || sharesFolder(elsewhere, all) {
		t.Error("only an older session of the same agent in the same folder shares it")
	}
}

// On restore, of two agents that resume by folder only the one that talked
// last resumes; the other waits for Relaunch.
func TestRestoreResumesOneAgentPerFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	t.Setenv("LARVYN_HOME", t.TempDir())
	store.Reload()
	t.Cleanup(store.Reload)
	dir := t.TempDir()
	_ = store.Update(func(d *store.State) { d.Projects = []store.Project{{ID: "p1", Path: dir}} })
	prev := AgentRelaunch
	t.Cleanup(func() { AgentRelaunch = prev })
	AgentRelaunch = func(spec SessionSpec, shared bool) ([]string, bool, error) {
		// A bare name: relaunch must resolve it like Start does.
		return []string{"sh", "-c", "sleep 30"}, !shared, nil
	}
	ts := &TerminalService{sessions: map[string]*session{}}
	defer ts.ServiceShutdown()
	ts.restoreWorkspace(savedWorkspace{Version: workspaceVersion, Sessions: []savedSession{
		{ID: "a", ProjectID: "p1", Kind: "agent", Key: "opencode", Title: "old", Cwd: dir, Running: true, LastActivity: 100},
		{ID: "b", ProjectID: "p1", Kind: "agent", Key: "opencode", Title: "latest", Cwd: dir, Running: true, LastActivity: 200},
	}})
	for _, s := range ts.List() {
		if want := s.Title == "latest"; s.Running != want {
			t.Errorf("%s running = %v, want %v", s.Title, s.Running, want)
		}
	}
}

// New output alone waits for scrollbackSaveEvery; a change to the sessions
// is saved on the next tick.
func TestAutosaveSpacesOutOutputOnlySaves(t *testing.T) {
	t.Setenv("LARVYN_HOME", t.TempDir())
	store.Reload()
	t.Cleanup(store.Reload)
	ts := &TerminalService{sessions: map[string]*session{}, persist: true}
	ts.Restore()
	ts.ws.lastSave = time.Now()
	ts.touchOutput()
	ts.autosave()
	if _, err := os.Stat(workspacePath()); err == nil {
		t.Fatal("output alone should not be saved right after a save")
	}
	ts.touch()
	ts.autosave()
	if _, err := os.Stat(workspacePath()); err != nil {
		t.Fatal("a change should be saved:", err)
	}
	ts.ws.lastSave = time.Now().Add(-scrollbackSaveEvery)
	ts.touchOutput()
	ts.autosave()
	if ts.ws.outDirty.Load() {
		t.Fatal("output should be saved once the last save is old enough")
	}
}

// A shell comes back in the folder it was in, not the one it started in.
func TestSavedShellKeepsItsCurrentFolder(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("the current folder of a process is read on Linux and macOS")
	}
	t.Setenv("LARVYN_HOME", t.TempDir())
	store.Reload()
	t.Cleanup(store.Reload)
	dir := t.TempDir()
	sub := filepath.Join(dir, "app")
	_ = os.Mkdir(sub, 0o755)
	_ = store.Update(func(d *store.State) {
		d.Projects = []store.Project{{ID: "p1", Path: dir}}
		d.Settings.Shell = "/bin/sh"
	})
	ts := &TerminalService{sessions: map[string]*session{}, persist: true}
	ts.Restore()
	info, err := ts.Start(SessionSpec{ProjectID: "p1", Kind: "shell", Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	_ = ts.Write(info.ID, "cd app && echo moved-$((1+1))\n")
	waitFor(t, func() bool { out, _ := ts.Output(info.ID); return strings.Contains(out, "moved-2") })
	_ = ts.ServiceShutdown()

	b, _ := os.ReadFile(workspacePath())
	var ws savedWorkspace
	_ = json.Unmarshal(b, &ws)
	if len(ws.Sessions) != 1 || !proc.SamePath(ws.Sessions[0].Cwd, sub) {
		t.Fatalf("saved cwd %+v, want %s", ws.Sessions, sub)
	}
}
