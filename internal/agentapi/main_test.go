package agentapi

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/store"
	"larvyn/internal/wiring"
)

func TestMain(m *testing.M) {
	// The fake agent of agents_test.go: this binary, started by Larvyn as a
	// custom agent.
	if os.Getenv("LARVYN_FAKE_AGENT") != "" {
		fakeAgent()
		os.Exit(0)
	}
	// The dev server of ports_test.go: this binary, run in a terminal.
	if len(os.Args) > 1 && os.Args[1] == "larvyn-fake-server" {
		fakeServer()
		os.Exit(0)
	}
	home, _ := os.MkdirTemp("", "larvyn-test")
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	wiring.Connect()
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// gitRepo makes a repository with one commit and a worktree next to it.
func gitRepo(t *testing.T) (main, wt string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	main = filepath.Join(dir, "app")
	wt = filepath.Join(dir, "app-wt")
	run := func(cwd string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(main, "app", "Models"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(main, "init", "-q", "-b", "main")
	run(main, "commit", "-q", "--allow-empty", "-m", "init")
	run(main, "worktree", "add", "-q", "-b", "larvyn/task", wt)
	return main, wt
}

func addProject(t *testing.T, path string) store.Project {
	t.Helper()
	p := store.Project{ID: store.NewID(), Name: filepath.Base(path), Path: path}
	if err := store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.Update(func(d *store.State) {
			for i, q := range d.Projects {
				if q.ID == p.ID {
					d.Projects = append(d.Projects[:i], d.Projects[i+1:]...)
					break
				}
			}
		})
	})
	return p
}

// command runs a command the way the larvyn CLI sends it.
func command(c api.Caller, argv ...string) api.Response {
	return api.Handle(api.Request{Caller: c, Argv: argv})
}

// shellLine is a command that prints text and waits, in this system's shell.
func shellLine(text string) string {
	if runtime.GOOS == "windows" {
		return "echo " + text + " & ping -n 30 127.0.0.1 >NUL"
	}
	return "echo " + text + "; sleep 30"
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}
