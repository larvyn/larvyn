//go:build !windows

package agents

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A running Codex is matched by the rollout file it has open, not by a
// guess: here another rollout in the same folder starts closer in time.
func TestCodexResumeIDUsesTheOpenRollout(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	now := time.Now()
	day := filepath.Join(root, "sessions", now.Format("2006"), now.Format("01"), now.Format("02"))
	_ = os.MkdirAll(day, 0o755)
	write := func(name, id string, at time.Time) string {
		p := filepath.Join(day, "rollout-"+name+".jsonl")
		line := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"/app","timestamp":"` + at.UTC().Format(time.RFC3339Nano) + `"}}` + "\n"
		_ = os.WriteFile(p, []byte(line), 0o644)
		return p
	}
	write("a", "external", now.Add(time.Second))
	mine := write("b", "mine", now.Add(5*time.Second))

	cmd := exec.Command("/bin/sh", "-c", "exec 3<\"$0\"; sleep 30", mine)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	var id string
	var exact bool
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if id, exact = codexResumeID(codexHome(nil), "/app", cmd.Process.Pid, now.UnixMilli(), map[string]bool{}); exact {
			break
		}
	}
	if id != "mine" || !exact {
		t.Fatalf("got %q exact=%v, want the open rollout", id, exact)
	}
	// Once stopped it can only guess: the earliest match.
	if id, exact := codexResumeID(codexHome(nil), "/app", 0, now.UnixMilli(), map[string]bool{}); id != "external" || exact {
		t.Fatalf("guess %q exact=%v", id, exact)
	}
}
