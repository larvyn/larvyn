package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"larvyn/internal/laravel"
	"larvyn/internal/proc"
	"larvyn/internal/stack"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "larvyn-test")
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func TestTerminalRunsCommand(t *testing.T) {
	cmd := []string{"cmd.exe", "/c", "echo larvyn-pty-ok"}
	if runtime.GOOS != "windows" {
		cmd = []string{"sh", "-c", "echo larvyn-pty-ok"}
	}
	s, err := terminals.Start(SessionSpec{Kind: "run", Argv: cmd, Cwd: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cur := terminal.Find("", "run", ""); cur != nil && !cur.Running {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	snap, err := terminals.Snapshot(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := base64.StdEncoding.DecodeString(snap.Data)
	if !strings.Contains(string(out), "larvyn-pty-ok") {
		t.Fatalf("output missing, got %q", out)
	}
	info := terminal.Find("", "run", "")
	if info == nil || info.Running || info.ExitCode != 0 {
		t.Fatalf("session did not exit cleanly: %+v", info)
	}
	terminals.Close(s.ID)
}

// LARVYN_TEST_PROJECT=/path/to/laravel go test -run Bridge -v
func TestBridgeAgainstRealProject(t *testing.T) {
	dir := os.Getenv("LARVYN_TEST_PROJECT")
	if dir == "" {
		t.Skip("LARVYN_TEST_PROJECT not set")
	}
	p, err := (&ProjectService{}).Add(dir)
	if err != nil {
		t.Fatal(err)
	}
	info := inspect(p)
	t.Logf("laravel=%s php=%s db=%s packages=%d branch=%s", info.LaravelVersion, info.PHPConstraint, info.DBConnection, len(info.Packages), info.Branch)
	if !info.IsLaravel {
		t.Fatal("not detected as Laravel")
	}
	for _, action := range []string{"ping", "models", "migrations", "schedule", "db.overview", "queue.failed"} {
		raw, err := (&laravel.LaravelService{}).Bridge(p.ID, action, map[string]any{})
		if err != nil {
			t.Errorf("%s: %v", action, err)
			continue
		}
		var r struct {
			OK    bool            `json:"ok"`
			Error json.RawMessage `json:"error"`
		}
		_ = json.Unmarshal(raw, &r)
		s := string(raw)
		if len(s) > 220 {
			s = s[:220] + "…"
		}
		t.Logf("%s ok=%v %s", action, r.OK, s)
	}
	raw, err := (&laravel.LaravelService{}).Bridge(p.ID, "tinker", map[string]any{"code": "$a = 20;\n$a + 22"})
	if err != nil || !strings.Contains(string(raw), `"dump":"42`) {
		t.Errorf("tinker: %v %s", err, raw)
	}
	l := &laravel.LaravelService{}
	if cmds, err := l.Commands(p.ID, false); err != nil || len(cmds) < 100 {
		t.Errorf("artisan list: %v", err)
	}
	if routes, err := l.Routes(p.ID); err != nil {
		t.Errorf("routes: %v", err)
	} else {
		t.Logf("routes json %d bytes", len(routes))
	}
	items, err := (&stack.StackService{}).Items(p.ID)
	if err != nil {
		t.Errorf("stack items: %v", err)
	}
	t.Logf("stack: %d items", len(items))
}

func TestTerminalShellCommand(t *testing.T) {
	s, err := terminals.Start(SessionSpec{Kind: "run", Key: "shellcmd", Command: "echo shell-ok", Cwd: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer terminals.Close(s.ID)
	for i := 0; i < 200; i++ {
		if cur := terminal.Find("", "run", "shellcmd"); cur != nil && !cur.Running {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	out, _ := terminals.Output(s.ID)
	if !strings.Contains(out, "shell-ok") {
		t.Fatalf("got %q", out)
	}
}

func TestUnwrapShim(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("batch shims are Windows only")
	}
	dir := t.TempDir()
	exe, _ := os.Executable()
	write := func(name, body string) string {
		p := dir + `\` + name
		os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	herd := write("php.bat", "@echo off\r\n\""+exe+"\" %*\r\n")
	if got := proc.UnwrapShim(herd); got != exe {
		t.Errorf("Herd-style shim: got %q", got)
	}
	complex := write("npm.cmd", "@echo off\r\nSETLOCAL\r\n\""+exe+"\" %*\r\n")
	if got := proc.UnwrapShim(complex); got != complex {
		t.Errorf("a script with more than one command must run as is, got %q", got)
	}
	if got := proc.UnwrapShim(exe); got != exe {
		t.Errorf("an .exe stays as is, got %q", got)
	}
}
