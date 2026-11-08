package terminal

import (
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitExit(t *testing.T, key string) SessionInfo {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cur := Find("", "run", key); cur != nil && !cur.Running {
			// The program after the setup may not have started yet.
			time.Sleep(300 * time.Millisecond)
			if cur := Find("", "run", key); cur != nil && !cur.Running {
				return *cur
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the session did not finish")
	return SessionInfo{}
}

func echoArgv(word string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe", "/c", "echo " + word}
	}
	return []string{"sh", "-c", "echo " + word}
}

func TestSetupRunsBeforeTheProgram(t *testing.T) {
	var started atomic.Bool
	s, err := StartAfterSetup(SessionSpec{Kind: "run", Key: "setup-ok", Cwd: os.TempDir(), Argv: echoArgv("program-ran")},
		[]string{"echo step-one", "echo step-two"}, func(SessionInfo) { started.Store(true) })
	if err != nil {
		t.Fatal(err)
	}
	defer Sessions.Close(s.ID)
	if !s.Setup {
		t.Fatal("the session should say it is in its setup")
	}
	info := waitExit(t, "setup-ok")
	if info.Setup {
		t.Fatal("the setup is over")
	}
	out, _ := Sessions.Output(s.ID)
	one, two, prog := strings.Index(out, "step-one\r\n"), strings.Index(out, "step-two\r\n"), strings.Index(out, "program-ran")
	if one < 0 || two < one || prog < two {
		t.Fatalf("steps and program out of order: %q", out)
	}
	if !strings.Contains(out, "$ echo step-one") {
		t.Fatalf("no header for the step: %q", out)
	}
	if info.ExitCode != 0 || !started.Load() {
		t.Fatalf("program did not run after the setup: %+v started=%v", info, started.Load())
	}
}

func TestFailedSetupStopsThere(t *testing.T) {
	var started atomic.Bool
	s, err := StartAfterSetup(SessionSpec{Kind: "run", Key: "setup-fail", Cwd: os.TempDir(), Argv: echoArgv("program-ran")},
		[]string{"exit 3", "echo never"}, func(SessionInfo) { started.Store(true) })
	if err != nil {
		t.Fatal(err)
	}
	defer Sessions.Close(s.ID)
	info := waitExit(t, "setup-fail")
	if info.Setup {
		t.Fatal("a failed setup is over")
	}
	out, _ := Sessions.Output(s.ID)
	if strings.Contains(out, "never\r\n") || strings.Contains(out, "program-ran") || started.Load() {
		t.Fatalf("ran past a failed step: %q", out)
	}
	if info.ExitCode != 3 || !strings.Contains(out, "Setup failed (exit 3): exit 3") {
		t.Fatalf("failure not reported: %+v %q", info, out)
	}
}

func TestNoSetupIsAPlainStart(t *testing.T) {
	var started atomic.Bool
	s, err := StartAfterSetup(SessionSpec{Kind: "run", Key: "setup-none", Cwd: os.TempDir(), Argv: echoArgv("program-ran")}, nil,
		func(SessionInfo) { started.Store(true) })
	if err != nil {
		t.Fatal(err)
	}
	defer Sessions.Close(s.ID)
	waitExit(t, "setup-none")
	out, _ := Sessions.Output(s.ID)
	if !strings.Contains(out, "program-ran") || !started.Load() {
		t.Fatalf("got %q started=%v", out, started.Load())
	}
}

func TestProgramThatCannotStartIsNotACleanExit(t *testing.T) {
	s, err := StartAfterSetup(SessionSpec{Kind: "run", Key: "setup-nostart", Cwd: os.TempDir(), Argv: []string{"/nonexistent/larvyn-agent"}},
		[]string{"echo step"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer Sessions.Close(s.ID)
	info := waitExit(t, "setup-nostart")
	out, _ := Sessions.Output(s.ID)
	if info.ExitCode == 0 || !strings.Contains(out, "Could not start") {
		t.Fatalf("got %+v %q", info, out)
	}
}
