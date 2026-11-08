package agents

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
