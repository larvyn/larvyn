package terminal

import (
	"errors"
	"fmt"
	"time"

	"larvyn/internal/proc"
	"larvyn/internal/store"
)

// StartAfterSetup starts a session that first runs setup, shell command
// lines one after another in the same terminal, and then the program spec
// describes. Each step's output shows in the session, under its command
// line. A step that fails or is stopped ends the session there, and the
// program does not start; Relaunch starts it later without the setup.
// started is called once the program runs (it may be nil).
//
// It is a function rather than a method so Wails does not bind it.
func StartAfterSetup(spec SessionSpec, setup []string, started func(SessionInfo)) (SessionInfo, error) {
	return Sessions.startAfterSetup(spec, setup, started)
}

func (t *TerminalService) startAfterSetup(spec SessionSpec, setup []string, started func(SessionInfo)) (SessionInfo, error) {
	if len(setup) == 0 {
		info, err := t.Start(spec)
		if err == nil && started != nil {
			started(info)
		}
		return info, err
	}
	proc.WaitShellEnv()
	if len(spec.Argv) == 0 && spec.Command == "" {
		return SessionInfo{}, errors.New("nothing to run after the setup")
	}
	_, display := resolveArgv(spec)
	if spec.Cwd == "" {
		if p, ok := store.FindProject(spec.ProjectID); ok {
			spec.Cwd = p.Path
		}
	}
	if spec.Kind == "" {
		spec.Kind = "shell"
	}
	now := time.Now().UnixMilli()
	s := &session{spec: spec, info: SessionInfo{
		ID: store.NewID(), ProjectID: spec.ProjectID, Kind: spec.Kind, Key: spec.Key,
		Title: spec.Title, Cwd: spec.Cwd, Command: display,
		StartedAt: now, LastActivity: now, Meta: spec.Meta, Setup: true,
	}}
	if s.info.Title == "" {
		s.info.Title = proc.FirstNonEmpty(display, "Terminal")
	}
	s.setup = append([]string(nil), setup[1:]...)
	s.started = started
	s.inSetup = true
	if err := t.runStep(s, setup[0]); err != nil {
		return SessionInfo{}, err
	}
	t.add(s)
	return s.snapshotInfo(), nil
}

// runStep runs one setup line in s, under a header that shows it.
func (t *TerminalService) runStep(s *session, line string) error {
	s.mu.Lock()
	s.step = line
	cols, rows := s.spec.Cols, s.spec.Rows
	s.mu.Unlock()
	t.output(s, []byte("\x1b[2m$ "+line+"\x1b[0m\r\n"))
	argv, _ := resolveArgv(SessionSpec{Command: line})
	return t.spawn(s, argv, cols, rows)
}

// nextStep is called when a session's process ended with code. During the
// setup it starts the next step, or the program after the last one, and
// reports handled: the session goes on. Otherwise exit is the code the
// session ended with: code, or -1 when the next step or the program could
// not start, so a session whose agent never ran doesn't look like it exited
// cleanly.
func (t *TerminalService) nextStep(s *session, code int) (handled bool, exit int) {
	s.mu.Lock()
	if !s.inSetup {
		s.mu.Unlock()
		return false, code
	}
	stopped := s.info.Stopped || (!s.interruptAt.IsZero() && time.Since(s.interruptAt) < 15*time.Second)
	step := s.step
	if code != 0 || stopped {
		s.inSetup, s.setup, s.started = false, nil, nil
		s.info.Setup = false
		s.mu.Unlock()
		if !stopped {
			t.output(s, []byte(fmt.Sprintf("\r\n\x1b[31mSetup failed (exit %d): %s\x1b[0m\r\n"+
				"The agent did not start. Fix the problem, then Relaunch to start it without the setup.\r\n", code, step)))
		}
		return false, code
	}
	if len(s.setup) > 0 {
		line := s.setup[0]
		s.setup = s.setup[1:]
		s.mu.Unlock()
		if err := t.runStep(s, line); err != nil {
			t.output(s, []byte("\r\n\x1b[31mCould not run "+line+": "+err.Error()+"\x1b[0m\r\n"))
			s.mu.Lock()
			s.inSetup, s.setup, s.started = false, nil, nil
			s.info.Setup = false
			s.mu.Unlock()
			return false, -1
		}
		t.broadcast()
		return true, code
	}
	s.inSetup = false
	s.info.Setup = false
	spec, started := s.spec, s.started
	s.started = nil
	s.mu.Unlock()
	t.output(s, []byte("\r\n"))
	argv, _ := resolveArgv(spec)
	if err := t.spawn(s, argv, spec.Cols, spec.Rows); err != nil {
		t.output(s, []byte("\r\n\x1b[31mCould not start "+spec.Title+": "+err.Error()+"\x1b[0m\r\n"))
		return false, -1
	}
	t.touch()
	t.broadcast()
	if started != nil {
		go started(s.snapshotInfo())
	}
	return true, code
}
