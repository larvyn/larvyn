package terminal

import (
	"encoding/base64"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTerminalRunsCommand(t *testing.T) {
	cmd := []string{"cmd.exe", "/c", "echo larvyn-pty-ok"}
	if runtime.GOOS != "windows" {
		cmd = []string{"sh", "-c", "echo larvyn-pty-ok"}
	}
	s, err := Sessions.Start(SessionSpec{Kind: "run", Argv: cmd, Cwd: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cur := Find("", "run", ""); cur != nil && !cur.Running {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	snap, err := Sessions.Snapshot(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := base64.StdEncoding.DecodeString(snap.Data)
	if !strings.Contains(string(out), "larvyn-pty-ok") {
		t.Fatalf("output missing, got %q", out)
	}
	info := Find("", "run", "")
	if info == nil || info.Running || info.ExitCode != 0 {
		t.Fatalf("session did not exit cleanly: %+v", info)
	}
	Sessions.Close(s.ID)
}

func TestTerminalShellCommand(t *testing.T) {
	s, err := Sessions.Start(SessionSpec{Kind: "run", Key: "shellcmd", Command: "echo shell-ok", Cwd: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer Sessions.Close(s.ID)
	for i := 0; i < 200; i++ {
		if cur := Find("", "run", "shellcmd"); cur != nil && !cur.Running {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	out, _ := Sessions.Output(s.ID)
	if !strings.Contains(out, "shell-ok") {
		t.Fatalf("got %q", out)
	}
}
