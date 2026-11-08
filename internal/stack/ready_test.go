package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"larvyn/internal/projectfile"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// TestHelperProcess is the program the stack items below run: this test
// binary again, told what to do after "--".
func TestHelperProcess(t *testing.T) {
	i := len(os.Args) - 1
	for i >= 0 && os.Args[i] != "--" {
		i--
	}
	if i < 0 || i+1 >= len(os.Args) {
		return
	}
	switch os.Args[i+1] {
	case "line":
		fmt.Println("booting")
		time.Sleep(400 * time.Millisecond)
		fmt.Println("  VITE v6  ready in 300 ms")
	case "port":
		ln, err := net.Listen("tcp", "127.0.0.1:"+os.Args[i+2])
		if err != nil {
			fmt.Println(err)
			os.Exit(2)
		}
		defer ln.Close()
	case "crash":
		fmt.Println("PHP Fatal error: boom")
		os.Exit(3)
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

// helperProject is a project whose .larvyn/project.json has one stack item
// per helper mode.
func helperProject(t *testing.T, port int) store.Project {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	item := func(key string, args ...string) map[string]any {
		return map[string]any{"key": key, "title": key, "argv": append([]string{exe, "-test.run=^TestHelperProcess$", "--"}, args...)}
	}
	file := map[string]any{"apiVersion": 1, "stack": []any{
		item("line", "line"), item("port", "port", strconv.Itoa(port)), item("crash", "crash"), item("quiet", "quiet"),
	}}
	b, _ := json.Marshal(file)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, projectfile.Dir), 0o755)
	os.WriteFile(projectfile.Path(dir), b, 0o644)
	p := store.Project{ID: "ready" + strconv.Itoa(port), Path: dir}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) })
	t.Cleanup(func() {
		(&StackService{}).StopAll(p.ID)
		for _, s := range terminal.Sessions.List() {
			if s.ProjectID == p.ID {
				_ = terminal.Sessions.Close(s.ID)
			}
		}
		store.Update(func(d *store.State) { d.Projects = nil })
	})
	Tail = func(id string, n int) string {
		out, _ := terminal.Sessions.Output(id)
		lines := strings.Split(strings.TrimRight(strings.ReplaceAll(out, "\r\n", "\n"), "\n"), "\n")
		return strings.Join(lines[max(0, len(lines)-n):], "\n")
	}
	return p
}

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func startItem(t *testing.T, p store.Project, key string) {
	t.Helper()
	if _, err := (&StackService{}).Start(p.ID, "project:"+key); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForALine(t *testing.T) {
	p := helperProject(t, freePort(t))
	startItem(t, p, "line")
	r, err := WaitReady(context.Background(), p.ID, "project:line", Ready{Line: `ready in \d+ ms`}, 15*time.Second)
	if err != nil || !r.Ready || r.Exited || r.Seconds < 0.3 {
		t.Fatalf("line: %+v %v", r, err)
	}
	if r.ExitCode() != 0 || !strings.Contains(r.Text(), "is ready") {
		t.Errorf("text %q", r.Text())
	}
}

func TestWaitForAPort(t *testing.T) {
	port := freePort(t)
	p := helperProject(t, port)
	startItem(t, p, "port")
	r, err := WaitReady(context.Background(), p.ID, "project:port", Ready{Port: port}, 15*time.Second)
	if err != nil || !r.Ready {
		t.Fatalf("port: %+v %v", r, err)
	}
}

func TestWaitSeesACrash(t *testing.T) {
	p := helperProject(t, freePort(t))
	startItem(t, p, "crash")
	r, err := WaitReady(context.Background(), p.ID, "project:crash", Ready{Line: "never"}, 15*time.Second)
	if err != nil || r.Ready || !r.Exited || r.Code != 3 || !strings.Contains(r.Tail, "boom") {
		t.Fatalf("crash: %+v %v", r, err)
	}
	if r.ExitCode() == 0 || !strings.Contains(r.Text(), "exit code 3") || !strings.Contains(r.Text(), "boom") {
		t.Errorf("text %q", r.Text())
	}
}

func TestWaitTimesOut(t *testing.T) {
	p := helperProject(t, freePort(t))
	startItem(t, p, "quiet")
	r, err := WaitReady(context.Background(), p.ID, "project:quiet", Ready{Line: "never"}, 700*time.Millisecond)
	if err != nil || r.Ready || r.Exited {
		t.Fatalf("timeout: %+v %v", r, err)
	}
	if r.ExitCode() == 0 || !strings.Contains(r.Text(), "not ready") {
		t.Errorf("text %q", r.Text())
	}
}

func TestWaitWithNothingToCheckSettles(t *testing.T) {
	p := helperProject(t, freePort(t))
	startItem(t, p, "quiet")
	r, err := WaitReady(context.Background(), p.ID, "project:quiet", Ready{}, 15*time.Second)
	if err != nil || !r.Ready || r.Seconds < 1 {
		t.Fatalf("settle: %+v %v", r, err)
	}
}

func TestWaitStopsWithTheCaller(t *testing.T) {
	p := helperProject(t, freePort(t))
	startItem(t, p, "quiet")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	if _, err := WaitReady(ctx, p.ID, "project:quiet", Ready{Line: "never"}, 15*time.Second); err == nil {
		t.Fatal("kept waiting after the caller left")
	}
}

func TestWaitNeedsAStartedItem(t *testing.T) {
	p := helperProject(t, freePort(t))
	if _, err := WaitReady(context.Background(), p.ID, "project:quiet", Ready{}, time.Second); err == nil || !strings.Contains(err.Error(), "start it first") {
		t.Errorf("not started: %v", err)
	}
	if _, err := WaitReady(context.Background(), p.ID, "nope", Ready{}, time.Second); err == nil {
		t.Error("an unknown item was waited for")
	}
	if _, err := WaitReady(context.Background(), p.ID, "project:quiet", Ready{Line: "("}, time.Second); err == nil {
		t.Error("a bad regexp was taken")
	}
}

func TestDefaultReady(t *testing.T) {
	if r := DefaultReady(StackItem{Key: "serve", URL: "http://127.0.0.1:8040"}); r.Port != 8040 {
		t.Errorf("serve: %+v", r)
	}
	if r := DefaultReady(StackItem{Key: "vite"}); r.Line == "" {
		t.Errorf("vite: %+v", r)
	}
	if r := DefaultReady(StackItem{Key: "project:x", Shared: true}); r != (Ready{}) {
		t.Errorf("shared: %+v", r)
	}
}
