package agents

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"larvyn/internal/projectfile"
	"larvyn/internal/terminal"
)

func TestWorktreeSetupPrefersTheBranchFile(t *testing.T) {
	write := func(dir, body string) {
		os.MkdirAll(filepath.Join(dir, projectfile.Dir), 0o755)
		os.WriteFile(projectfile.Path(dir), []byte(body), 0o644)
	}
	root, wt := t.TempDir(), t.TempDir()
	write(root, `{"apiVersion": 1, "worktree": {"setup": ["composer install"]}}`)
	// No file in the worktree yet (not committed): the main folder's.
	if got := worktreeSetup(wt, root); len(got) != 1 || got[0] != "composer install" {
		t.Fatalf("got %q", got)
	}
	// The branch's own file wins, even when it has no setup.
	other := t.TempDir()
	write(other, `{"apiVersion": 1}`)
	if got := worktreeSetup(other, root); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestWorktreeSetupSkipsABrokenBranchFile(t *testing.T) {
	root, wt := t.TempDir(), t.TempDir()
	for dir, body := range map[string]string{root: `{"apiVersion": 1, "worktree": {"setup": ["composer install"]}}`, wt: `{"apiVersion": 1,`} {
		os.MkdirAll(filepath.Join(dir, projectfile.Dir), 0o755)
		os.WriteFile(projectfile.Path(dir), []byte(body), 0o644)
	}
	if got := worktreeSetup(wt, root); len(got) != 1 || got[0] != "composer install" {
		t.Fatalf("a branch file that doesn't parse should fall back to the main folder's: %q", got)
	}
}

func TestSendWaitsForTheSetup(t *testing.T) {
	sleep := "sleep 5"
	if runtime.GOOS == "windows" {
		sleep = "ping -n 6 127.0.0.1"
	}
	s, err := terminal.StartAfterSetup(terminal.SessionSpec{Kind: "run", Key: "send-setup", Cwd: os.TempDir(), Command: "echo agent"},
		[]string{sleep}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Sessions.Close(s.ID)
	if err := (&AgentService{}).Send(s.ID, "hello"); err == nil || !strings.Contains(err.Error(), "set up") {
		t.Fatalf("a follow-up during the setup should be refused: %v", err)
	}
}

func TestSendToAnExitedAgentFails(t *testing.T) {
	s, err := terminal.Sessions.Start(terminal.SessionSpec{Kind: "run", Key: "send-exited", Cwd: os.TempDir(), Command: "echo done"})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Sessions.Close(s.ID)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if !running(s.ID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the program didn't end")
		}
	}
	// The terminal would drop the message: Send says so instead of
	// reporting it sent.
	if err := (&AgentService{}).Send(s.ID, "hello"); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("a follow-up to an exited agent should fail: %v", err)
	}
}

func running(id string) bool {
	for _, s := range terminal.Sessions.List() {
		if s.ID == id {
			return s.Running
		}
	}
	return false
}
