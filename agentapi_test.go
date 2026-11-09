package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

func TestLastLines(t *testing.T) {
	out := "one\r\ntwo\nprogress 10%\rprogress 100%\r\nthree  \n\n\n"
	if got := lastLines(out, -1); got != "one\ntwo\nprogress 100%\nthree" {
		t.Errorf("all: %q", got)
	}
	if got := lastLines(out, 2); got != "progress 100%\nthree" {
		t.Errorf("last 2: %q", got)
	}
}

func TestWithin(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.Join(sep+"a", "app")
	for path, want := range map[string]bool{
		root:                            true,
		filepath.Join(root, "src", "x"): true,
		filepath.Join(sep+"a", "app2"):  false,
		filepath.Join(sep + "a"):        false,
		filepath.Join(root, "..", "b"):  false,
	} {
		if got := within(path, root); got != want {
			t.Errorf("within(%s) = %v", path, got)
		}
	}
}

func TestPathsThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	os.MkdirAll(filepath.Join(real, "app"), 0o755)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	// The project saved through the link, git printing the real path.
	if !samePath(link, real) || !within(filepath.Join(real, "app"), link) {
		t.Error("a path through a symlink is not the same folder")
	}
	if runtime.GOOS == "linux" && samePath(filepath.Join(dir, "App"), filepath.Join(dir, "app")) {
		t.Error("Linux tells App and app apart")
	}
}

// Case counts as the file system says: two folders that differ only in
// case are two folders where they can both exist (Linux, a case-sensitive
// macOS volume), and one where they can't.
func TestCaseFollowsTheFileSystem(t *testing.T) {
	dir := t.TempDir()
	lower := filepath.Join(dir, "app")
	upper := filepath.Join(dir, "App")
	os.MkdirAll(filepath.Join(lower, "Models"), 0o755)
	if err := os.Mkdir(upper, 0o755); err == nil {
		// Case-sensitive: App is its own folder.
		if samePath(lower, upper) || within(filepath.Join(lower, "Models"), upper) {
			t.Error("App and app are two folders here")
		}
		return
	}
	// Case-insensitive: App is app.
	if !samePath(lower, upper) || !within(filepath.Join(upper, "Models"), lower) {
		t.Error("App and app are the same folder here")
	}
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

func TestContextFromAFolder(t *testing.T) {
	main, wt := gitRepo(t)
	p := addProject(t, main)

	ctx, err := apiContext(api.Caller{Cwd: filepath.Join(main, "app", "Models")})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.ProjectID != p.ID || ctx.Worktree || ctx.Branch != "main" || !samePath(ctx.Dir, main) {
		t.Errorf("main folder: %+v", ctx)
	}

	ctx, err = apiContext(api.Caller{Cwd: wt})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.ProjectID != p.ID || !ctx.Worktree || ctx.Branch != "larvyn/task" || !samePath(ctx.Dir, wt) {
		t.Errorf("worktree: %+v", ctx)
	}

	if _, err := apiContext(api.Caller{Cwd: t.TempDir()}); err == nil {
		t.Error("a folder outside every project has a context")
	}
	if _, err := apiContext(api.Caller{}); err == nil {
		t.Error("no session and no folder has a context")
	}
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

func TestTerminalsThroughTheAPI(t *testing.T) {
	main, _ := gitRepo(t)
	p := addProject(t, main)
	caller := api.Caller{Cwd: main}

	opened, err := apiTerminalOpen(caller, shellLine("hello-from-api"), "", "api test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = terminal.Sessions.Close(opened.ID) })
	if opened.Kind != "run" || opened.Project != p.Name || opened.Title != "api test" {
		t.Errorf("opened: %+v", opened)
	}

	list, err := apiTerminalList(caller, false)
	if err != nil || len(list) != 1 || list[0].ID != opened.ID {
		t.Fatalf("list: %+v %v", list, err)
	}

	// A unique start of the id is enough.
	waitFor(t, "the output", func() bool {
		out, _ := apiTerminalRead(opened.ID[:6], 0, false, false)
		return strings.Contains(out, "hello-from-api")
	})

	// Search: smart case, line numbers, plain text when not a regexp.
	if out, err := apiTerminalSearch(opened.ID, "hello-FROM", 0, 0); err != nil || !strings.HasPrefix(out, "No line matches") {
		t.Errorf("search with a capital is case-sensitive: %q %v", out, err)
	}
	if out, err := apiTerminalSearch(opened.ID, "HELLO-from-api|nothing", 0, 0); err != nil || !strings.HasPrefix(out, "No line") {
		t.Errorf("case-sensitive regexp: %q %v", out, err)
	}
	if out, err := apiTerminalSearch(opened.ID, "hello-from-(api)", 0, 0); err != nil || !regexp.MustCompile(`^\d+: .*hello-from-api`).MatchString(out) {
		t.Errorf("search: %q %v", out, err)
	}
	if _, err := apiTerminalSearch(opened.ID, "hello[", 0, 0); err != nil {
		t.Errorf("not a regexp, taken as text: %v", err)
	}

	// A server started in a terminal shows up in ports, with its URL. The
	// server is this test binary, run by its name from its own folder so
	// cmd.exe needs no quotes; python3 can be the Microsoft Store alias that
	// runs nothing.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := "./" + filepath.Base(exe)
	if runtime.GOOS == "windows" {
		run = filepath.Base(exe)
	}
	srv, err := apiTerminalOpen(caller, run+" larvyn-fake-server", filepath.Dir(exe), "server")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = terminal.Sessions.Close(srv.ID) })
	var rows PortRows
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		rows, err = apiPorts(caller, false)
		if err == nil && len(rows) == 1 && rows[0].Terminal == srv.ID && strings.HasPrefix(rows[0].URL, "http://127.0.0.1:") {
			break
		}
		if time.Now().After(deadline) {
			// What it saw, to tell a server that never started from one
			// that ports can't see.
			out, _ := apiTerminalRead(srv.ID, 0, true, false)
			s, _ := findTerminal(srv.ID)
			pid, _ := terminal.PID(srv.ID)
			all, _ := apiPorts(caller, true)
			t.Fatalf("the server's port: ports %+v (%v), all projects %+v; running=%v pid=%d; its output:\n%s", rows, err, all, s.Running, pid, out)
		}
	}
	out, _ := apiTerminalRead(srv.ID, 0, true, false)
	if want := fmt.Sprintf("listening on %d", rows[0].Port); !strings.Contains(out, want) {
		t.Errorf("ports says %d, the server says:\n%s", rows[0].Port, out)
	}

	// LARVYN_SESSION tells the CLI in it which terminal it runs in.
	sh, err := apiTerminalOpen(caller, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = terminal.Sessions.Close(sh.ID) })
	echo := "echo id=$LARVYN_SESSION"
	if runtime.GOOS == "windows" {
		echo = "echo id=%LARVYN_SESSION%"
	}
	waitFor(t, "the shell", func() bool {
		_ = apiTerminalWrite(sh.ID, echo, true)
		out, _ := apiTerminalRead(sh.ID, 0, true, false)
		return strings.Contains(out, "id="+sh.ID)
	})

	// Only who opened a terminal may close it.
	if err := apiTerminalClose(api.Caller{Session: "someone-else"}, opened.ID); err == nil {
		t.Error("another terminal closed it")
	}
	if err := apiTerminalClose(caller, opened.ID); err != nil {
		t.Errorf("close: %v", err)
	}
	if _, err := findTerminal(opened.ID); err == nil {
		t.Error("still listed after close")
	}
}

func TestTerminalsOpenedByAgentsStayTheirs(t *testing.T) {
	main, _ := gitRepo(t)
	addProject(t, main)
	// A terminal the user opened, not through the API.
	user, err := terminal.Sessions.Start(terminal.SessionSpec{Kind: "shell", Cwd: main})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = terminal.Sessions.Close(user.ID) })
	if err := apiTerminalClose(api.Caller{Session: user.ID, Cwd: main}, user.ID); err == nil {
		t.Error("a terminal the user opened was closed through the API")
	}
}

func TestFindTerminal(t *testing.T) {
	if _, err := findTerminal(""); err == nil {
		t.Error("empty id found a terminal")
	}
	if _, err := findTerminal("zzzz-none"); err == nil {
		t.Error("unknown id found a terminal")
	}
}

// fakeServer listens on a free port and says which, for the ports test. It
// gives up after a minute in case its terminal outlives the test.
func fakeServer() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer ln.Close()
	fmt.Printf("listening on %d\n", ln.Addr().(*net.TCPAddr).Port)
	time.Sleep(time.Minute)
}
