package terminal

import (
	"os"
	"runtime"
	"testing"
	"time"
)

func TestStateFromOutput(t *testing.T) {
	// BEL and ST terminators, extra fields, and idle clearing the state.
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"hello\x1b]7501;state=blocked\x07world", StateBlocked, true},
		{"\x1b]7501;agent=claude;state=done\x1b\\", StateDone, true},
		{"\x1b]7501;state=working\x07\x1b]7501;state=idle\x1b\\", "", true},
		{"\x1b]0;window title\x07plain text", "", false},
		{"\x1b]7501;state=bogus\x07", "", false},
	} {
		got, ok, rest := stateFromOutput(nil, []byte(tc.in))
		if ok != tc.ok || got != tc.want || len(rest) != 0 {
			t.Errorf("%q: got (%q, %v, %q), want (%q, %v, nil)", tc.in, got, ok, rest, tc.want, tc.ok)
		}
	}

	// Split across several reads, including right inside the prefix and ST.
	parts := []string{"out\x1b]", "750", "1;state=blo", "cked\x1b", "\\more"}
	var pending []byte
	var got string
	var found bool
	for _, p := range parts {
		s, ok, rest := stateFromOutput(pending, []byte(p))
		if ok {
			got, found = s, true
		}
		pending = rest
	}
	if !found || got != StateBlocked || len(pending) != 0 {
		t.Fatalf("split: got (%q, %v, %q)", got, found, pending)
	}
}

func TestSessionStateTransitions(t *testing.T) {
	t0 := time.Now()
	s := &session{info: SessionInfo{Running: true, StartedAt: t0.UnixMilli(), LastActivity: t0.UnixMilli()}}

	// OSC sequence sets the state.
	if !s.noteOutput([]byte("\x1b]7501;state=blocked\x1b\\"), s.info.LastActivity, t0) || s.info.State != StateBlocked {
		t.Fatalf("expected blocked, got %q", s.info.State)
	}

	// An xterm escape reply does not clear blocked; typing an answer does.
	if s.noteInput("\x1b[?1;2c", t0) || s.info.State != StateBlocked {
		t.Fatalf("escape reply cleared blocked: %q", s.info.State)
	}
	if !s.noteInput("y\r", t0) || s.info.State != "" {
		t.Fatalf("answer did not clear blocked: %q", s.info.State)
	}

	// Done stays through a brief redraw, then drops if output keeps streaming.
	s.setStateLocked(StateDone, t0)
	t1 := t0.Add(500 * time.Millisecond)
	s.noteOutput([]byte("prompt> "), t0.UnixMilli(), t1)
	if s.info.State != StateDone {
		t.Fatalf("short redraw dropped done: %q", s.info.State)
	}
	prev := t1.UnixMilli()
	for d := 3 * time.Second; d <= 8*time.Second; d += time.Second {
		ts := t0.Add(d)
		s.noteOutput([]byte("still working..."), prev, ts)
		prev = ts.UnixMilli()
	}
	if s.info.State != "" {
		t.Fatalf("sustained output did not drop done: %q", s.info.State)
	}

	// Ctrl+C drops working.
	s.setStateLocked(StateWorking, t0)
	if !s.noteInput("\x03", t0) || s.info.State != "" {
		t.Fatalf("Ctrl+C did not drop working: %q", s.info.State)
	}
}

func TestLivePTYOSCState(t *testing.T) {
	cmd := []string{"sh", "-c", `printf '\033]7501;state=blocked\033\\'; sleep 30`}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	s, err := Sessions.Start(SessionSpec{Kind: "agent", Key: "osctest", Argv: cmd, Cwd: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer Sessions.Close(s.ID)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cur := Find("", "agent", "osctest"); cur != nil && cur.State == StateBlocked {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	cur := Find("", "agent", "osctest")
	if cur == nil || cur.State != StateBlocked {
		t.Fatalf("PTY did not pick up OSC 7501 blocked state: %+v", cur)
	}
	if err := Sessions.Write(s.ID, "y\n"); err != nil {
		t.Fatal(err)
	}
	cur = Find("", "agent", "osctest")
	if cur == nil || cur.State != "" {
		t.Fatalf("typing into PTY did not clear state: %+v", cur)
	}
}
