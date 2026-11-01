//go:build !windows

package proc

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startSession starts sh -c script as its own session leader, the way go-pty
// starts terminal programs, and returns its pid once its grandchildren run.
func startSession(t *testing.T, script string) (*exec.Cmd, []int) {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait() // reap it, as the terminal's pump does
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, _ := exec.Command("pgrep", "-g", strconv.Itoa(cmd.Process.Pid)).Output()
		var pids []int
		for _, f := range strings.Fields(string(out)) {
			if p, _ := strconv.Atoi(f); p != cmd.Process.Pid {
				pids = append(pids, p)
			}
		}
		if len(pids) >= 3 {
			return cmd, pids
		}
		if time.Now().After(deadline) {
			t.Fatalf("grandchildren did not start: %v", pids)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func assertGone(t *testing.T, pids []int) {
	t.Helper()
	for _, p := range pids {
		if syscall.Kill(p, 0) == nil {
			_ = syscall.Kill(p, syscall.SIGKILL)
			t.Errorf("%d survived KillTree", p)
		}
	}
}

func TestKillTreeKillsGrandchildren(t *testing.T) {
	cmd, pids := startSession(t, `sh -c 'sleep 300 & sleep 300 & wait' & wait`)
	KillTree(cmd.Process.Pid)
	assertGone(t, append(pids, cmd.Process.Pid))
}

func TestKillTreeFallsBackToSIGKILL(t *testing.T) {
	defer func(d time.Duration) { killGrace = d }(killGrace)
	killGrace = 500 * time.Millisecond
	cmd, pids := startSession(t, `trap '' TERM HUP; sh -c 'sleep 300 & sleep 300 & wait' & wait`)
	start := time.Now()
	KillTree(cmd.Process.Pid)
	if d := time.Since(start); d < killGrace {
		t.Fatalf("returned after %s, before the grace period", d)
	}
	time.Sleep(100 * time.Millisecond) // SIGKILL is delivered asynchronously
	assertGone(t, append(pids, cmd.Process.Pid))
}

func TestKillTreeOnDeadPid(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	KillTree(cmd.Process.Pid)
	KillTree(0)
	KillTree(1)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("KillTree on a dead pid took %s", d)
	}
}
