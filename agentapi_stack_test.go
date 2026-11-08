package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/projectfile"
	"larvyn/internal/stack"
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
	case "ready":
		time.Sleep(300 * time.Millisecond)
		fmt.Println("worker READY")
	case "crash":
		fmt.Println("cannot connect to redis")
		os.Exit(4)
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

// stackProject is a git project whose project file has a "worker" item that
// prints READY and a "broken" one that crashes.
func stackProject(t *testing.T) (store.Project, api.Caller) {
	t.Helper()
	main, _ := gitRepo(t)
	exe, _ := os.Executable()
	item := func(key, mode string) map[string]any {
		return map[string]any{"key": key, "title": key, "argv": []string{exe, "-test.run=^TestHelperProcess$", "--", mode}}
	}
	b, _ := json.Marshal(map[string]any{"apiVersion": 1, "stack": []any{item("worker", "ready"), item("broken", "crash")}})
	os.MkdirAll(filepath.Join(main, projectfile.Dir), 0o755)
	os.WriteFile(projectfile.Path(main), b, 0o644)
	p := addProject(t, main)
	t.Cleanup(func() {
		(&stack.StackService{}).StopAll(p.ID)
		for _, s := range terminal.Sessions.List() {
			if s.ProjectID == p.ID {
				_ = terminal.Sessions.Close(s.ID)
			}
		}
	})
	return p, api.Caller{Cwd: main}
}

// cli runs a command the way the larvyn CLI sends it.
func command(c api.Caller, argv ...string) api.Response {
	return api.Handle(api.Request{Caller: c, Argv: argv})
}

func TestStackThroughTheAPI(t *testing.T) {
	_, caller := stackProject(t)

	// "worker" finds the project file's "project:worker".
	r := command(caller, "stack", "start", "worker", "--wait", "--timeout", "15")
	if r.Error != "" || r.Exit != 0 || !strings.Contains(r.Text, "worker is ready") {
		t.Fatalf("start --wait: %+v", r)
	}
	r = command(caller, "stack", "wait", "worker", "--line", "READY")
	if r.Error != "" || r.Exit != 0 {
		t.Fatalf("wait on a ready item: %+v", r)
	}

	r = command(caller, "stack", "list")
	var rows []StackRow
	json.Unmarshal(r.Result, &rows)
	status := map[string]string{}
	for _, row := range rows {
		status[row.Key] = row.Status
	}
	if status["project:worker"] != "running" || status["project:broken"] != "stopped" {
		t.Errorf("list: %v", status)
	}

	// A crash ends the wait at once, with its output, and exits 1.
	r = command(caller, "stack", "start", "broken", "--wait")
	if r.Error != "" || r.Exit != 1 || !strings.Contains(r.Text, "exit code 4") || !strings.Contains(r.Text, "cannot connect to redis") {
		t.Fatalf("crash: %+v", r)
	}
	r = command(caller, "stack", "list")
	json.Unmarshal(r.Result, &rows)
	for _, row := range rows {
		if row.Key == "project:broken" && row.Status != "crashed (exit 4)" {
			t.Errorf("broken: %q", row.Status)
		}
	}

	// stop returns once it stopped.
	if r := command(caller, "stack", "stop", "worker"); r.Error != "" {
		t.Fatalf("stop: %+v", r)
	}
	for _, x := range terminal.Sessions.List() {
		if x.Key == "project:worker" && x.Running {
			t.Error("still running after stop returned")
		}
	}

	if r := command(caller, "stack", "start", "nope"); !strings.Contains(r.Error, "no stack item") {
		t.Errorf("unknown item: %+v", r)
	}
}

// fakeLaravel is a folder with an artisan script that echoes its command,
// fails on "boom", and writes a JUnit report for "test".
func fakeLaravel(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath(store.PHP()); err != nil {
		t.Skip("no php")
	}
	script := `<?php
$args = array_values(array_filter(array_slice($argv, 1), fn ($a) => !in_array($a, ['--no-interaction', '--no-ansi'])));
if (($args[0] ?? '') === 'boom') { fwrite(STDERR, "Command failed\n"); exit(2); }
if (($args[0] ?? '') === 'test') {
    foreach ($args as $a) if (str_starts_with($a, '--log-junit=')) $junit = substr($a, 12);
    file_put_contents($junit, '<?xml version="1.0"?><testsuites><testsuite name="all">'
        . '<testcase name="it logs in" class="Tests\\Feature\\LoginTest" file="tests/Feature/LoginTest.php" line="10" time="0.1"/>'
        . '<testcase name="it logs out" class="Tests\\Feature\\LoginTest" file="tests/Feature/LoginTest.php" line="20" time="0.2">'
        . '<failure>it logs out' . "\n" . 'Expected status 302 but got 500.' . "\n" . 'at tests/Feature/LoginTest.php:22</failure></testcase>'
        . '</testsuite></testsuites>');
    echo "Tests: 1 failed, 1 passed\n";
    exit(1);
}
echo 'ran ' . implode(' ', $args) . "\n";
`
	if err := os.WriteFile(filepath.Join(dir, "artisan"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestArtisanRunsInTheCallersWorktree(t *testing.T) {
	main, wt := gitRepo(t)
	addProject(t, main)
	fakeLaravel(t, wt)
	r := command(api.Caller{Cwd: wt}, "artisan", "migrate", "--force", "--seed")
	if r.Error != "" || r.Exit != 0 || r.Text != "ran migrate --force --seed" {
		t.Fatalf("artisan: %+v", r)
	}
	r = command(api.Caller{Cwd: wt}, "artisan", "boom")
	if r.Exit != 2 || !strings.Contains(r.Text, "Command failed") {
		t.Errorf("failing artisan: %+v", r)
	}
	if r := command(api.Caller{Cwd: wt}, "artisan", "serve"); !strings.Contains(r.Error, "doesn't finish") {
		t.Errorf("serve: %+v", r)
	}
	// Its own flags reach artisan; "--" in front is dropped.
	for _, argv := range [][]string{{"artisan", "route:list", "--json", "--help"}, {"artisan", "--", "route:list", "--json", "--help"}} {
		r = command(api.Caller{Cwd: wt}, argv...)
		if r.Text != "ran route:list --json --help" || r.JSON {
			t.Errorf("%v: %+v", argv, r)
		}
	}
	// --json before the command is Larvyn's.
	if r := command(api.Caller{Cwd: wt}, "--json", "artisan", "about"); !r.JSON || r.Text != "ran about" {
		t.Errorf("--json first: %+v", r)
	}
	// From a subfolder, artisan runs in the app's root.
	sub := filepath.Join(wt, "app", "Models")
	os.MkdirAll(sub, 0o755)
	if r := command(api.Caller{Cwd: sub}, "artisan", "about"); r.Error != "" || r.Text != "ran about" {
		t.Errorf("from a subfolder: %+v", r)
	}
}

func TestTestsReportTheirFailures(t *testing.T) {
	main, wt := gitRepo(t)
	p := addProject(t, main)
	fakeLaravel(t, wt)
	t.Cleanup(func() {
		for _, s := range terminal.Sessions.List() {
			if s.ProjectID == p.ID {
				_ = terminal.Sessions.Close(s.ID)
			}
		}
	})
	r := command(api.Caller{Cwd: wt}, "test", "--filter", "Login")
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	var run TestRun
	json.Unmarshal(r.Result, &run)
	if run.Tests != 2 || run.Failed != 1 || r.Exit != 1 || len(run.Failures) != 1 || run.Failures[0].Message != "Expected status 302 but got 500." {
		t.Fatalf("run: %+v exit %d", run, r.Exit)
	}
	if !strings.Contains(r.Text, "2 tests: 1 passed, 1 failed") || !strings.Contains(r.Text, "✗ Tests\\Feature\\LoginTest › it logs out") {
		t.Errorf("text:\n%s", r.Text)
	}
	// The run shows in a terminal of the project, in the worktree.
	s, err := findTerminal(run.Terminal)
	if err != nil || !samePath(s.Cwd, wt) || s.Kind != "run" {
		t.Errorf("terminal: %+v %v", s, err)
	}
	// A worktree's report doesn't replace the project's in the Tests view.
	if junitFor(p, wt) == junitFor(p, main) {
		t.Error("the worktree writes the project's report")
	}
}

func TestLogErrors(t *testing.T) {
	main, _ := gitRepo(t)
	addProject(t, main)
	logs := filepath.Join(main, "storage", "logs")
	os.MkdirAll(logs, 0o755)
	old := time.Now().Add(-3 * time.Hour).Format("2006-01-02 15:04:05")
	recent := time.Now().Add(-time.Minute).Format("2006-01-02 15:04:05")
	log := "[" + old + "] local.ERROR: Old failure {\"exception\":\"[object] (RuntimeException(code: 0): Old failure at /app/Old.php:1)\"}\n" +
		"[" + recent + "] local.INFO: Just info\n" +
		"[" + recent + "] local.ERROR: Undefined variable $user {\"exception\":\"[object] (ErrorException(code: 0): Undefined variable $user at /app/app/Http/Controllers/HomeController.php:14)\n[stacktrace]\n#0 /app/vendor/laravel/framework/src/Illuminate/Foundation/Bootstrap/HandleExceptions.php(255): handleError()\n#1 {main}\n\"}\n"
	os.WriteFile(filepath.Join(logs, "laravel.log"), []byte(log), 0o644)

	// From a subfolder too: the app's own storage/logs.
	sub := filepath.Join(main, "app", "Http")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(main, "artisan"), []byte("<?php"), 0o644)
	r := command(api.Caller{Cwd: sub}, "logs", "errors")
	var got LogErrors
	json.Unmarshal(r.Result, &got)
	if r.Error != "" || len(got.Errors) != 2 {
		t.Fatalf("all: %+v %s", got, r.Error)
	}
	e := got.Errors[1]
	if e.Exception != "ErrorException" || e.At != "/app/app/Http/Controllers/HomeController.php:14" || e.Trace != "" {
		t.Errorf("entry: %+v", e)
	}
	r = command(api.Caller{Cwd: main}, "logs", "errors", "--since", "1h", "--trace")
	json.Unmarshal(r.Result, &got)
	if len(got.Errors) != 1 || !strings.Contains(got.Errors[0].Trace, "HandleExceptions.php") {
		t.Fatalf("since 1h: %+v", got)
	}
	if !strings.Contains(r.Text, "error: Undefined variable $user") || !strings.Contains(r.Text, "ErrorException at /app/app/Http/Controllers/HomeController.php:14") {
		t.Errorf("text:\n%s", r.Text)
	}
	if r := command(api.Caller{Cwd: main}, "logs", "errors", "--since", "yesterday"); r.Error == "" {
		t.Error("a bad --since was taken")
	}
}

// A project without git: from a subfolder artisan still runs in the app's
// root, the project's folder.
func TestArtisanFromASubfolderWithoutGit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	sub := filepath.Join(dir, "app", "Models")
	os.MkdirAll(sub, 0o755)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	addProject(t, dir)
	fakeLaravel(t, dir)
	if got := appRoot(store.Project{Path: dir}, sub); !samePath(got, dir) {
		t.Errorf("app root %s, want %s", got, dir)
	}
	if r := command(api.Caller{Cwd: sub}, "artisan", "about"); r.Error != "" || r.Text != "ran about" {
		t.Errorf("artisan: %+v", r)
	}
}
