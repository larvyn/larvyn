//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"larvyn/internal/proc"

	"github.com/aymanbagabas/go-pty"
)

// These reproduce issue #4: stopping a terminal used to signal only the
// program's direct children, so grandchildren (vite under npm run dev, the
// PHP server under artisan serve) kept running.

func newTestTerminals() *TerminalService {
	return &TerminalService{sessions: map[string]*session{}}
}

// startTree starts argv in a terminal and returns the session id and the
// pids of every descendant of the terminal's program, once want of them run.
func startTree(t *testing.T, ts *TerminalService, argv []string, want int) (string, []int) {
	t.Helper()
	info, err := ts.Start(SessionSpec{Kind: "service", Argv: argv, Cwd: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s, err := ts.get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	root := s.cmd.Process.Pid
	deadline := time.Now().Add(10 * time.Second)
	for {
		pids := descendants(root)
		if len(pids) >= want {
			return info.ID, pids
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d descendants of %d, got %v", want, root, pids)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func descendants(pid int) []int {
	out, _ := exec.Command("pgrep", "-P", strconv.Itoa(pid)).Output()
	var all []int
	for _, f := range strings.Fields(string(out)) {
		if c, err := strconv.Atoi(f); err == nil {
			all = append(all, c)
			all = append(all, descendants(c)...)
		}
	}
	return all
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// waitGone fails unless every pid has exited within d.
func waitGone(t *testing.T, pids []int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		var left []int
		for _, p := range pids {
			if alive(p) {
				left = append(left, p)
			}
		}
		if len(left) == 0 {
			return
		}
		if time.Now().After(deadline) {
			for _, p := range left {
				_ = syscall.Kill(p, syscall.SIGKILL)
			}
			t.Fatalf("still running after %s: %v", d, left)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Like npm run dev: the program starts a shell that starts the real servers.
// When a terminal's program dies the kernel hangs up its process group, so
// the servers here ignore SIGHUP, as they do when Larvyn itself was started
// ignoring it (nohup, some launchers) or when they handle it themselves.
var nestedTree = []string{"sh", "-c", `sh -c 'trap "" HUP; sleep 300 & sleep 300 & wait' & wait`}

func TestTerminalKillStopsGrandchildren(t *testing.T) {
	ts := newTestTerminals()
	id, pids := startTree(t, ts, nestedTree, 3)
	if err := ts.Kill(id); err != nil {
		t.Fatal(err)
	}
	waitGone(t, pids, 2*time.Second)
	_ = ts.Close(id)
}

func TestTerminalCloseKillsWhatIgnoresSIGTERM(t *testing.T) {
	ts := newTestTerminals()
	argv := []string{"sh", "-c", `trap '' TERM HUP; sh -c 'sleep 300 & sleep 300 & wait' & wait`}
	id, pids := startTree(t, ts, argv, 3)
	start := time.Now()
	if err := ts.Close(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	for _, p := range pids {
		if !alive(p) {
			t.Fatalf("%d exited on SIGTERM although it ignores it", p)
		}
	}
	waitGone(t, pids, 6*time.Second)
	t.Logf("killed after %s", time.Since(start).Round(100*time.Millisecond))
}

func TestServiceShutdownKillsEveryTree(t *testing.T) {
	ts := newTestTerminals()
	_, a := startTree(t, ts, nestedTree, 3)
	_, b := startTree(t, ts, []string{"sh", "-c", `trap '' TERM HUP; sh -c 'sleep 300 & wait' & wait`}, 2)
	if err := ts.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	// ServiceShutdown waits, so nothing may be left once it returns.
	waitGone(t, append(a, b...), 200*time.Millisecond)
}

// A command typed into an interactive shell runs in a process group of its
// own (job control); closing the tab must stop it too, and quickly.
func TestTerminalKillStopsShellJobs(t *testing.T) {
	ts := newTestTerminals()
	info, err := ts.Start(SessionSpec{Kind: "shell", Argv: []string{"sh", "-i"}, Cwd: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := ts.get(info.ID)
	shell := s.cmd.Process.Pid
	if err := ts.Write(info.ID, "sh -c 'sleep 300 & sleep 300 & wait'\n"); err != nil {
		t.Fatal(err)
	}
	var pids []int
	deadline := time.Now().Add(10 * time.Second)
	for len(pids) < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("job did not start: %v", pids)
		}
		time.Sleep(50 * time.Millisecond)
		pids = descendants(shell)
	}
	fg := 0
	_ = s.pty.(pty.UnixPty).Control(func(fd uintptr) { fg = proc.ForegroundGroup(fd) })
	if fg == 0 || fg == shell {
		t.Errorf("foreground group = %d, want the job's own group (shell %d)", fg, shell)
	}
	start := time.Now()
	if err := ts.Close(info.ID); err != nil {
		t.Fatal(err)
	}
	waitGone(t, append(pids, shell), 2*time.Second)
	t.Logf("shell and job gone after %s", time.Since(start).Round(10*time.Millisecond))
}
