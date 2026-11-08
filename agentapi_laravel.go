package main

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/laravel"
	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// Laravel through the API. Each command runs in the caller's own folder,
// an agent's worktree included, with the PHP Larvyn uses for the project.
func init() {
	api.Register("artisan", "Run an artisan command that finishes and print its output; exits with its code", apiArtisan,
		"args...: the command and its options, as for php artisan (long-running ones like serve belong in the stack)")
	api.Register("test", "Run the tests in a terminal Larvyn shows, then print the failures; exits 1 if any failed", apiTest,
		"filter?: only the tests matching this, as artisan test --filter",
		"parallel?: run them in parallel",
		"timeout?: seconds, 1800 by default")
	api.Register("logs.errors", "Print the latest errors from the app's log in storage/logs", apiLogErrors,
		"since?: only errors newer than this: 10m, 2h…",
		"limit?: how many, 10 by default",
		"trace?: add each error's first stack frames")
}

// ArtisanResult is what artisan printed and how it exited.
type ArtisanResult struct {
	Code   int    `json:"code"`
	Output string `json:"output"`
}

func (r ArtisanResult) Text() string  { return r.Output }
func (r ArtisanResult) ExitCode() int { return r.Code }

func apiArtisan(c api.Caller, args []string) (ArtisanResult, error) {
	if len(args) == 0 {
		return ArtisanResult{}, errors.New("which artisan command? larvyn artisan list shows them")
	}
	switch args[0] {
	case "serve", "queue:work", "queue:listen", "horizon", "schedule:work", "pail", "reverb:start", "octane:start", "tinker":
		return ArtisanResult{}, fmt.Errorf("artisan %s doesn't finish: use larvyn stack start, or larvyn terminal open --command", args[0])
	}
	_, dir, _, err := callerProject(c)
	if err != nil {
		return ArtisanResult{}, err
	}
	res, err := proc.Artisan(dir, 10*time.Minute, args...)
	out := strings.TrimRight(proc.StripANSI(res.Stdout), "\n")
	if e := strings.TrimSpace(proc.StripANSI(res.Stderr)); e != "" {
		out = strings.TrimLeft(out+"\n"+e, "\n")
	}
	if err != nil {
		return ArtisanResult{}, fmt.Errorf("%v\n%s", err, out)
	}
	return ArtisanResult{Code: res.Code, Output: out}, nil
}

// TestRun is a test run's outcome.
type TestRun struct {
	Tests    int                `json:"tests"`
	Passed   int                `json:"passed"`
	Failed   int                `json:"failed"`
	Skipped  int                `json:"skipped"`
	Seconds  float64            `json:"seconds"`
	Code     int                `json:"code"`
	Terminal string             `json:"terminal"`
	Failures []laravel.TestCase `json:"failures"`
	// Output is the run's last lines when it wrote no report: it failed to
	// boot or was stopped.
	Output string `json:"output,omitempty"`
}

func (r TestRun) Text() string {
	var b strings.Builder
	if r.Tests == 0 {
		fmt.Fprintf(&b, "No test results (exit code %d).\n", r.Code)
		if r.Output != "" {
			b.WriteString("\n" + r.Output + "\n")
		}
		return b.String()
	}
	fmt.Fprintf(&b, "%d tests: %d passed, %d failed, %d skipped in %.1fs\n", r.Tests, r.Passed, r.Failed, r.Skipped, r.Seconds)
	for _, f := range r.Failures {
		fmt.Fprintf(&b, "\n✗ %s › %s", f.Class, f.Name)
		if f.File != "" {
			fmt.Fprintf(&b, "  (%s:%d)", f.File, f.Line)
		}
		b.WriteString("\n")
		if d := strings.TrimSpace(f.Details); d != "" {
			lines := strings.Split(d, "\n")
			if len(lines) > 12 {
				lines = append(lines[:12], "…")
			}
			for _, l := range lines {
				b.WriteString("  " + l + "\n")
			}
		}
	}
	return b.String()
}

func (r TestRun) ExitCode() int {
	if r.Failed > 0 || r.Tests == 0 || r.Code != 0 {
		return 1
	}
	return 0
}

// junitFor is where a run in dir writes its report: the project's own, so
// the Tests view shows it, unless dir is a worktree.
func junitFor(p store.Project, dir string) string {
	if samePath(dir, p.Path) {
		return laravel.JUnitPath(p.ID)
	}
	h := sha1.Sum([]byte(filepath.Clean(dir)))
	return filepath.Join(store.HomeDir(), "junit", p.ID+"-"+hex.EncodeToString(h[:6])+".xml")
}

func apiTest(c api.Caller, filter string, parallel bool, timeout int) (TestRun, error) {
	p, dir, _, err := callerProject(c)
	if err != nil {
		return TestRun{}, err
	}
	if top, err := proc.Git(dir, "rev-parse", "--show-toplevel"); err == nil {
		dir = filepath.FromSlash(strings.TrimSpace(top))
	}
	junit := junitFor(p, dir)
	_ = os.MkdirAll(filepath.Dir(junit), 0o700)
	_ = os.Remove(junit)
	title := "artisan test"
	if !samePath(dir, p.Path) {
		title += " · " + filepath.Base(dir)
	}
	if filter != "" {
		title += " --filter=" + filter
	}
	sess, err := terminal.Sessions.Start(terminal.SessionSpec{ProjectID: p.ID, Kind: "run", Key: "tests", Title: title, Cwd: dir,
		Argv: laravel.TestArgv(junit, filter, parallel), Meta: map[string]string{"openedBy": proc.FirstNonEmpty(c.Session, "cli")}})
	if err != nil {
		return TestRun{}, err
	}
	limit := time.Duration(timeout) * time.Second
	if timeout <= 0 {
		limit = 30 * time.Minute
	}
	deadline := time.After(limit)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	run := TestRun{Terminal: sess.ID}
	for done := false; !done; {
		select {
		case <-c.Ctx.Done():
			_ = terminal.Sessions.Kill(sess.ID)
			return run, errors.New("stopped: the test run was cancelled")
		case <-deadline:
			_ = terminal.Sessions.Kill(sess.ID)
			return run, fmt.Errorf("the tests took longer than %s and were stopped", limit)
		case <-tick.C:
			s, err := findTerminal(sess.ID)
			if err != nil {
				return run, errors.New("the test terminal was closed")
			}
			if !s.Running {
				run.Code, done = s.ExitCode, true
			}
		}
	}
	rep, err := laravel.ReadJUnit(junit, dir)
	if err != nil {
		return run, err
	}
	run.Tests, run.Passed, run.Failed, run.Skipped, run.Seconds = rep.Tests, rep.Passed, rep.Failed, rep.Skipped, rep.Time
	run.Failures = []laravel.TestCase{}
	for _, tc := range rep.Cases {
		if tc.Status == "failed" || tc.Status == "error" {
			run.Failures = append(run.Failures, tc)
		}
	}
	if rep.Tests == 0 {
		out, _ := terminal.Sessions.Output(sess.ID)
		run.Output = lastLines(out, 30)
	}
	return run, nil
}

// LogError is an error from the app's log.
type LogError struct {
	Time      string `json:"time"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Exception string `json:"exception,omitempty"`
	At        string `json:"at,omitempty"`
	Trace     string `json:"trace,omitempty"`
}

// LogErrors is what larvyn logs errors prints.
type LogErrors struct {
	File   string     `json:"file"`
	Errors []LogError `json:"errors"`
}

func (l LogErrors) Text() string {
	if len(l.Errors) == 0 {
		if l.File == "" {
			return "No log files in storage/logs.\n"
		}
		return "No errors in " + l.File + ".\n"
	}
	var b strings.Builder
	for i, e := range l.Errors {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%s] %s: %s\n", e.Time, e.Level, e.Message)
		if e.At != "" {
			fmt.Fprintf(&b, "  %s at %s\n", proc.FirstNonEmpty(e.Exception, "thrown"), e.At)
		}
		for _, l := range strings.Split(e.Trace, "\n") {
			if l != "" {
				b.WriteString("  " + l + "\n")
			}
		}
	}
	return b.String()
}

var errorLevels = map[string]bool{"error": true, "critical": true, "alert": true, "emergency": true}

// logTime reads a log entry's time, written in the app's timezone, which
// is taken to be this machine's.
func logTime(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05.999999Z07:00", "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05.999999"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func apiLogErrors(c api.Caller, since string, limit int, trace bool) (LogErrors, error) {
	_, dir, _, err := callerProject(c)
	if err != nil {
		return LogErrors{}, err
	}
	var after time.Time
	if since != "" {
		d, err := time.ParseDuration(since)
		if err != nil || d <= 0 {
			return LogErrors{}, fmt.Errorf("--since %q: give a duration like 10m or 2h", since)
		}
		after = time.Now().Add(-d)
	}
	if limit <= 0 {
		limit = 10
	}
	files := laravel.LogFilesIn(dir)
	if len(files) == 0 {
		return LogErrors{Errors: []LogError{}}, nil
	}
	// The newest file: laravel.log, or today's with the daily channel.
	chunk, err := laravel.ReadLogIn(dir, files[0].Name, -1)
	if err != nil {
		return LogErrors{}, err
	}
	out := LogErrors{File: filepath.Join("storage", "logs", files[0].Name), Errors: []LogError{}}
	for _, e := range chunk.Entries {
		if !errorLevels[e.Level] {
			continue
		}
		if !after.IsZero() {
			if t, ok := logTime(e.Time); ok && t.Before(after) {
				continue
			}
		}
		le := LogError{Time: e.Time, Level: e.Level, Message: e.Message, Exception: e.Exception}
		if e.File != "" {
			le.At = fmt.Sprintf("%s:%d", e.File, e.Line)
		}
		if trace {
			le.Trace = firstLines(e.Stack, 8)
		}
		out.Errors = append(out.Errors, le)
	}
	if len(out.Errors) > limit {
		out.Errors = out.Errors[len(out.Errors)-limit:]
	}
	return out, nil
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
