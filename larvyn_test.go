package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"larvyn/internal/proc"
	"larvyn/internal/store"
)

func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "larvyn-test")
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func TestParseEnvLine(t *testing.T) {
	cases := map[string][2]string{
		`APP_NAME="My App"`:        {"APP_NAME", "My App"},
		`DB_PASSWORD='s3cr#t'`:     {"DB_PASSWORD", "s3cr#t"},
		`CACHE_STORE=redis # note`: {"CACHE_STORE", "redis"},
		`export FOO=bar`:           {"FOO", "bar"},
		`EMPTY=`:                   {"EMPTY", ""},
	}
	for line, want := range cases {
		k, v, ok := parseEnvLine(line)
		if !ok || k != want[0] || v != want[1] {
			t.Errorf("%q: got %q=%q ok=%v", line, k, v, ok)
		}
	}
	if _, _, ok := parseEnvLine("# comment"); ok {
		t.Error("comment parsed as a key")
	}
}

func TestParseLog(t *testing.T) {
	log := `[2026-10-30 12:00:01] local.ERROR: Undefined variable $user {"exception":"[object] (ErrorException(code: 0): Undefined variable $user at /app/app/Http/Controllers/HomeController.php:14)
[stacktrace]
#0 /app/vendor/laravel/framework/src/Foundation/Bootstrap/HandleExceptions.php(256): handleError()
#1 /app/app/Http/Controllers/HomeController.php(14): foo()
"}
[2026-10-30 12:00:02] local.INFO: User logged in {"id":1}
`
	entries := parseLog([]byte(log), 0)
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	e := entries[0]
	if e.Level != "error" || e.Exception != "ErrorException" || e.Line != 14 || !strings.HasSuffix(e.File, "HomeController.php") {
		t.Errorf("bad error entry: %+v", e)
	}
	if !strings.Contains(e.Stack, "#1 /app/app/Http") || strings.Contains(e.Stack, `"}`) {
		t.Errorf("bad stack: %q", e.Stack)
	}
	win := parseLog([]byte(`[2026-10-30 12:00:01] local.ERROR: boom {"exception":"[object] (Illuminate\\View\\ViewException(code: 0): boom at C:\\app\\x.php:3)"}`+"\n"), 0)
	if win[0].Exception != `Illuminate\View\ViewException` || win[0].File != `C:\app\x.php` {
		t.Errorf("escaped names not unescaped: %+v", win[0])
	}
	if entries[1].Level != "info" || entries[1].Message != "User logged in" || entries[1].Context != `{"id":1}` {
		t.Errorf("bad info entry: %+v", entries[1])
	}
}

func TestContainsBell(t *testing.T) {
	if containsBell([]byte("\x1b]0;title\x07")) {
		t.Error("OSC terminator counted as a bell")
	}
	if !containsBell([]byte("done\x07")) {
		t.Error("plain bell missed")
	}
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
		if cur := terminals.find("", "run", ""); cur != nil && !cur.Running {
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
	info := terminals.find("", "run", "")
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
		raw, err := callBridge(p.ID, action, map[string]any{}, 90*time.Second)
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
	raw, err := callBridge(p.ID, "tinker", map[string]any{"code": "$a = 20;\n$a + 22"}, 60*time.Second)
	if err != nil || !strings.Contains(string(raw), `"dump":"42`) {
		t.Errorf("tinker: %v %s", err, raw)
	}
	l := &LaravelService{}
	if cmds, err := l.Commands(p.ID, false); err != nil || len(cmds) < 100 {
		t.Errorf("artisan list: %v", err)
	}
	if routes, err := l.Routes(p.ID); err != nil {
		t.Errorf("routes: %v", err)
	} else {
		t.Logf("routes json %d bytes", len(routes))
	}
	items := stackItems(p)
	t.Logf("stack: %d items", len(items))
}

func TestWorktreeLifecycle(t *testing.T) {
	if proc.Which("git") == "" {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		if _, err := proc.Git(repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(repo+"/README.md", []byte("hi\n"), 0o644)
	os.WriteFile(repo+"/.env", []byte("APP_NAME=Demo\n"), 0o644)
	proc.Git(repo, "add", "README.md")
	if _, err := proc.Git(repo, "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	p, err := (&ProjectService{}).Add(repo)
	if err != nil {
		t.Fatal(err)
	}
	g := &GitService{}
	wt, err := g.CreateWorktree(WorktreeRequest{ProjectID: p.ID, Name: "Add Feature!", CopyEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	if wt.Branch != "larvyn/add-feature" {
		t.Errorf("branch = %s", wt.Branch)
	}
	if b, _ := os.ReadFile(wt.Path + "/.env"); string(b) != "APP_NAME=Demo\n" {
		t.Error(".env not copied")
	}
	os.WriteFile(wt.Path+"/feature.php", []byte("<?php // agent work\n"), 0o644)
	diff, err := g.BranchDiff(wt.Path, "main")
	if err != nil || !strings.Contains(diff, "agent work") {
		t.Fatalf("diff missing new file: %v %q", err, diff)
	}
	if _, err := g.MergeWorktree(p.ID, wt.Path, "feat: add feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(repo + "/feature.php"); err != nil {
		t.Error("merge did not bring feature.php into main")
	}
	if err := g.RemoveWorktree(p.ID, wt.Path, true); err != nil {
		t.Fatal(err)
	}
	if out, _ := proc.Git(repo, "branch"); strings.Contains(out, "larvyn/") {
		t.Errorf("branch not deleted: %s", out)
	}
}

func TestTerminalShellCommand(t *testing.T) {
	s, err := terminals.Start(SessionSpec{Kind: "run", Key: "shellcmd", Command: "echo shell-ok", Cwd: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer terminals.Close(s.ID)
	for i := 0; i < 200; i++ {
		if cur := terminals.find("", "run", "shellcmd"); cur != nil && !cur.Running {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	out, _ := terminals.Output(s.ID)
	if !strings.Contains(out, "shell-ok") {
		t.Fatalf("got %q", out)
	}
}

func TestTestResults(t *testing.T) {
	p, err := (&ProjectService{}).Add(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(store.HomeDir()+"/junit", 0o755)
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="Tests\Feature" tests="3">
    <testsuite name="Tests\Feature\PostTest" file="tests/Feature/PostTest.php" tests="3">
      <testcase name="it lists posts" file="tests/Feature/PostTest.php::it lists posts" class="Tests\Feature\PostTest" time="0.12"/>
      <testcase name="it stores a post" file="tests/Feature/PostTest.php::it stores a post" class="Tests\Feature\PostTest" time="0.30">
        <failure type="PHPUnit\Framework\ExpectationFailedException">it stores a postExpected response status code [201] but received 422.
at tests/Feature/PostTest.php:21</failure>
      </testcase>
      <testcase name="it is skipped" class="Tests\Feature\PostTest" time="0"><skipped/></testcase>
    </testsuite>
  </testsuite>
</testsuites>`
	os.WriteFile(junitPath(p.ID), []byte(xml), 0o644)
	r, err := (&LaravelService{}).TestResults(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Tests != 3 || r.Passed != 1 || r.Failed != 1 || r.Skipped != 1 {
		t.Fatalf("counts: %+v", r)
	}
	f := r.Cases[1]
	if f.Status != "failed" || !strings.HasPrefix(f.Message, "Expected response") || !strings.Contains(f.Message, "422") || f.File != "tests/Feature/PostTest.php" || f.Line != 21 {
		t.Errorf("failure: %+v", f)
	}
}

func TestSplitCommand(t *testing.T) {
	for in, want := range map[string][]string{
		"php artisan serve":                      {"php", "artisan", "serve"},
		`php artisan queue:listen --queue="a b"`: {"php", "artisan", "queue:listen", "--queue=a b"},
		"npm run dev":                            {"npm", "run", "dev"},
	} {
		got, ok := splitCommand(in)
		if !ok || strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("splitCommand(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"npm run build && npm run dev", "php artisan foo | tee x", `echo "open`} {
		if _, ok := splitCommand(in); ok {
			t.Errorf("splitCommand(%q) should need a shell", in)
		}
	}
}

func TestDevStackItems(t *testing.T) {
	serve := StackItem{Key: "serve", Label: "HTTP server", Argv: []string{"php", "artisan", "serve", "--host=127.0.0.1", "--port=8040"}}
	vite := StackItem{Key: "vite", Label: "Vite"}
	items := devStackItems([]DevProcess{
		{Name: "server", Command: "php artisan serve --port=9000"},
		{Name: "queue", Command: "php artisan queue:listen --tries=1 --timeout=0"},
		{Name: "vite", Command: "npm run dev"},
		{Name: "ssr", Command: "php artisan inertia:start-ssr"},
		{Name: "docs", Command: "npm run docs:dev && echo done"},
	}, serve, vite)
	keys := []string{}
	for _, it := range items {
		keys = append(keys, it.Key)
		if !it.Dev {
			t.Errorf("%s is not marked as a dev process", it.Key)
		}
	}
	if strings.Join(keys, ",") != "serve,queue,vite,ssr,docs" {
		t.Fatalf("keys = %v", keys)
	}
	if got := strings.Join(items[0].Argv, " "); got != "php artisan serve --host=127.0.0.1 --port=8040" {
		t.Errorf("serve keeps the project's port, got %q", got)
	}
	if items[3].Argv[1] != "artisan" || items[3].Label != "Ssr" {
		t.Errorf("custom artisan process: %+v", items[3])
	}
	if items[4].Argv[0] == "npm" {
		t.Errorf("a chained command must run through the shell: %v", items[4].Argv)
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
