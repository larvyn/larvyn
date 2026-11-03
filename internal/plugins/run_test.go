package plugins

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// runManifest is a plugin with one command and one stack item that print
// markers, so tests can see them run.
func runManifest(t *testing.T, run string) string {
	return edit(t, func(m map[string]any) {
		c := contributes(m)
		c["commands"] = []any{map[string]any{"id": "hello", "title": "Say hello", "run": run}}
		c["stack"] = []any{map[string]any{"key": "watch", "title": "Watcher", "argv": []string{"mailpit", "--listen", "127.0.0.1:8025"}}}
	})
}

// installRun installs runManifest and a project to run it in.
func installRun(t *testing.T, run string) (*PluginService, store.Project) {
	t.Helper()
	reset(t)
	withPrograms(t)
	src := t.TempDir()
	writePlugin(t, src, runManifest(t, run))
	s := &PluginService{}
	if _, err := s.Install(src, horizonPerms); err != nil {
		t.Fatal(err)
	}
	proj := store.Project{ID: store.NewID(), Name: "app", Path: t.TempDir()}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, proj) })
	t.Cleanup(func() {
		store.Update(func(d *store.State) { d.Projects = nil })
		stopSessions("acme.horizon")
	})
	return s, proj
}

func waitOutput(t *testing.T, id, want string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if out, _ := terminal.Sessions.Output(id); strings.Contains(out, want) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	out, _ := terminal.Sessions.Output(id)
	t.Fatalf("%q never showed up in:\n%s", want, out)
}

func TestCommandNeedsConfirmationOnce(t *testing.T) {
	s, proj := installRun(t, "echo plugin-ran")
	cmds := s.Commands()
	if len(cmds) != 1 || !cmds[0].NeedsConfirm || cmds[0].Run != "echo plugin-ran" || cmds[0].PluginName != "Horizon" {
		t.Fatalf("commands: %+v", cmds)
	}
	if _, err := s.RunCommand(proj.ID, "acme.horizon", "hello", ""); err == nil || !strings.Contains(err.Error(), "confirm") {
		t.Fatalf("an unconfirmed command must not run: %v", err)
	}
	if _, err := s.RunCommand(proj.ID, "acme.horizon", "hello", "echo something-else"); err == nil {
		t.Fatal("a command other than the one shown must not run")
	}
	sess, err := s.RunCommand(proj.ID, "acme.horizon", "hello", "echo plugin-ran")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Cwd != proj.Path || !strings.HasPrefix(sess.Key, SessionKeyPrefix("acme.horizon")) {
		t.Errorf("session: %+v", sess)
	}
	waitOutput(t, sess.ID, "plugin-ran")
	if s.Commands()[0].NeedsConfirm {
		t.Error("a confirmed command should not ask again")
	}
	if _, err := s.RunCommand(proj.ID, "acme.horizon", "hello", ""); err != nil {
		t.Errorf("a confirmed command runs without asking: %v", err)
	}
}

func TestChangedCommandAsksAgain(t *testing.T) {
	s, proj := installRun(t, "echo one")
	if _, err := s.RunCommand(proj.ID, "acme.horizon", "hello", "echo one"); err != nil {
		t.Fatal(err)
	}
	// The plugin's files change after the user confirmed (an update, or a
	// linked plugin being edited).
	writePlugin(t, filepath.Join(Dir(), "acme.horizon"), runManifest(t, "echo two"))
	if c := s.Commands(); !c[0].NeedsConfirm {
		t.Error("a changed command must be confirmed again")
	}
	if _, err := s.RunCommand(proj.ID, "acme.horizon", "hello", ""); err == nil {
		t.Error("a changed command ran without confirmation")
	}
}

func TestOffPluginRunsNothing(t *testing.T) {
	s, proj := installRun(t, "echo plugin-ran")
	if err := s.Disable("acme.horizon"); err != nil {
		t.Fatal(err)
	}
	if len(s.Commands()) != 0 || len(StackItems()) != 0 {
		t.Error("a plugin that is off contributes nothing")
	}
	if _, err := s.RunCommand(proj.ID, "acme.horizon", "hello", "echo plugin-ran"); err == nil || !strings.Contains(err.Error(), "off") {
		t.Errorf("error = %v", err)
	}
}

func TestGateNeedsGrantedPermission(t *testing.T) {
	p := Plugin{Enabled: true, Manifest: Manifest{ID: "acme.x", Permissions: []string{"terminal.run"}}}
	if err := allow(p, "terminal.run"); err == nil {
		t.Error("asked but not granted must be refused")
	}
	p.Granted = []string{"terminal.run", "network"}
	if err := allow(p, "terminal.run"); err != nil {
		t.Errorf("asked and granted: %v", err)
	}
	if err := allow(p, "network"); err == nil {
		t.Error("granted but not asked for must be refused")
	}
	p.Enabled = false
	if err := allow(p, "terminal.run"); err == nil {
		t.Error("a plugin that is off must be refused")
	}
}

func TestDisableStopsWhatThePluginRuns(t *testing.T) {
	long := "sleep 30"
	if runtime.GOOS == "windows" {
		long = "ping -n 30 127.0.0.1"
	}
	s, proj := installRun(t, long)
	sess, err := s.RunCommand(proj.ID, "acme.horizon", "hello", long)
	if err != nil {
		t.Fatal(err)
	}
	s.Disable("acme.horizon")
	for _, x := range terminal.Sessions.List() {
		if x.ID == sess.ID {
			t.Errorf("the plugin's terminal is still open after turning it off: %+v", x)
		}
	}
}

func TestStackItems(t *testing.T) {
	s, _ := installRun(t, "echo x")
	items := StackItems()
	if len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
	it := items[0]
	if it.Key != "plugin:acme.horizon:watch" || it.Line != "mailpit --listen 127.0.0.1:8025" || !it.NeedsConfirm || it.Missing != "" {
		t.Errorf("item: %+v", it)
	}
	if err := s.ConfirmStackItem("acme.horizon", "watch", "mailpit --other"); err == nil {
		t.Error("confirming a different line must fail")
	}
	if err := s.ConfirmStackItem("acme.horizon", "watch", it.Line); err != nil {
		t.Fatal(err)
	}
	if StackItems()[0].NeedsConfirm {
		t.Error("confirmed item still needs confirmation")
	}
}

func TestStackItemMissingProgram(t *testing.T) {
	reset(t)
	withPrograms(t) // nothing installed
	writePlugin(t, filepath.Join(Dir(), "acme.horizon"), edit(t, func(m map[string]any) {
		m["requires"] = []any{map[string]any{"program": "mailpit"}}
		contributes(m)["stack"] = []any{map[string]any{"key": "watch", "title": "Watcher", "argv": []string{"mailpit"}}}
	}))
	(&PluginService{}).Enable("acme.horizon", horizonPerms)
	if items := StackItems(); len(items) != 1 || items[0].Missing != "mailpit" {
		t.Errorf("items: %+v", items)
	}
}

func TestCommandLine(t *testing.T) {
	for argv, want := range map[string]string{
		"php|artisan|horizon":  "php artisan horizon",
		"echo|a b":             "echo 'a b'",
		"echo|it's":            `echo 'it'\''s'`,
		"sh|-c|rm -rf ~; echo": `sh -c 'rm -rf ~; echo'`,
	} {
		if got := commandLine(strings.Split(argv, "|")); got != want {
			t.Errorf("commandLine(%q) = %q, want %q", argv, got, want)
		}
	}
}
