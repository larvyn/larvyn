package terminal

import (
	"bytes"
	"errors"
	"strings"
	"time"
)

// A program can say what it is doing instead of leaving Larvyn to guess
// from its output: working, blocked (waiting for the user) or done. It
// prints an OSC 7501 sequence, or its hooks run `larvyn agents hook <state>`
// (internal/agentsetup), which ends in SetState:
//
//	printf '\e]7501;state=blocked\e\\'
//
// idle hands the state back to the guess. A reported state lasts until the
// program reports another, the user answers (input that isn't an escape
// sequence), interrupts it (Ctrl+C, Esc), or it keeps printing for a while
// after saying it was blocked or done (busy again without saying so).

// States a program can report; "" is none.
const (
	StateWorking = "working"
	StateBlocked = "blocked"
	StateDone    = "done"
)

// oscState starts the sequence.
var oscState = []byte("\x1b]7501;")

// maxOSC caps a sequence still waiting for its end across reads.
const maxOSC = 256

// busyAgain is how long a program that reported blocked or done must print
// without a pause for its state to be dropped; settleAfter is how long after
// the report its own redraw may last before that counts.
const (
	busyAgain   = 5 * time.Second
	settleAfter = 2 * time.Second
)

// burstGap is the pause that ends a burst of output.
const burstGap = 2500 * time.Millisecond

// normalizeState returns the state to keep for a reported one: "" for idle,
// ok false for anything unknown.
func normalizeState(s string) (string, bool) {
	switch s = strings.ToLower(strings.TrimSpace(s)); s {
	case StateWorking, StateBlocked, StateDone:
		return s, true
	case "idle", "":
		return "", true
	}
	return "", false
}

// SetState records the state a terminal's program reported. It is a
// function rather than a method so Wails does not bind it.
func SetState(id, state string) error {
	s, err := Sessions.get(id)
	if err != nil {
		return err
	}
	st, ok := normalizeState(state)
	if !ok {
		return errors.New("unknown state " + state + ": working, blocked, done or idle")
	}
	if Sessions.setState(s, st) {
		Sessions.broadcast()
	}
	return nil
}

// setState changes s's state and reports whether it changed.
func (t *TerminalService) setState(s *session, state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setStateLocked(state, time.Now())
}

func (s *session) setStateLocked(state string, now time.Time) bool {
	if !s.info.Running && state != "" {
		return false
	}
	changed := s.info.State != state
	s.info.State = state
	if state == "" {
		s.info.StateAt = 0
	} else {
		s.info.StateAt = now.UnixMilli()
	}
	return changed
}

// stateFromOutput looks for OSC 7501 sequences in what a program printed
// and returns the last state one reports. pending is the start of a sequence
// the previous read cut; rest is what to keep for the next one.
func stateFromOutput(pending, chunk []byte) (state string, found bool, rest []byte) {
	data := chunk
	if len(pending) > 0 {
		data = append(append([]byte(nil), pending...), chunk...)
	}
	for {
		i := bytes.Index(data, oscState)
		if i < 0 {
			return state, found, partialPrefix(data)
		}
		body := data[i+len(oscState):]
		end, skip := oscEnd(body)
		if end < 0 {
			if len(body) > maxOSC {
				// Never ended: not ours to wait for.
				data = body
				continue
			}
			return state, found, append([]byte(nil), data[i:]...)
		}
		if st, ok := parseOSCState(string(body[:end])); ok {
			state, found = st, true
		}
		data = body[end+skip:]
	}
}

// oscEnd finds the end of an OSC body: BEL, or ESC \ (ST). It returns the
// body's length and the terminator's, or -1 when it hasn't come yet.
func oscEnd(b []byte) (end, skip int) {
	for i, c := range b {
		switch c {
		case 7:
			return i, 1
		case 0x1b:
			if i+1 < len(b) {
				if b[i+1] == '\\' {
					return i, 2
				}
				// Another sequence starts: this one was cut short.
				return i, 0
			}
			return -1, 0
		}
	}
	return -1, 0
}

// parseOSCState reads "state=blocked" out of "state=blocked;agent=x".
func parseOSCState(body string) (string, bool) {
	for _, field := range strings.Split(body, ";") {
		k, v, ok := strings.Cut(field, "=")
		if ok && strings.TrimSpace(k) == "state" {
			return normalizeState(v)
		}
	}
	return "", false
}

// partialPrefix returns the end of data when it is the start of oscState,
// so a sequence cut right at its beginning is still found.
func partialPrefix(data []byte) []byte {
	for n := min(len(oscState)-1, len(data)); n > 0; n-- {
		if bytes.Equal(data[len(data)-n:], oscState[:n]) {
			return append([]byte(nil), data[len(data)-n:]...)
		}
	}
	return nil
}

// noteOutput updates s's state for output printed at now: a reported state
// in it, or a blocked or done one dropped because the program has been
// printing without a pause since a little after it said so. s.mu is held;
// prev is the output's previous LastActivity.
func (s *session) noteOutput(chunk []byte, prev int64, now time.Time) bool {
	ms := now.UnixMilli()
	if ms-prev > burstGap.Milliseconds() || s.burstStart == 0 {
		s.burstStart = ms
	}
	changed := false
	if len(s.osc) > 0 || bytes.Contains(chunk, oscState) || partialPrefix(chunk) != nil {
		var st string
		var found bool
		st, found, s.osc = stateFromOutput(s.osc, chunk)
		if found {
			changed = s.setStateLocked(st, now)
		}
	}
	if s.info.State == StateBlocked || s.info.State == StateDone {
		from := max(s.burstStart, s.info.StateAt+settleAfter.Milliseconds())
		if ms-from >= busyAgain.Milliseconds() {
			changed = s.setStateLocked("", now) || changed
		}
	}
	return changed
}

// noteInput drops a reported state that input makes stale: an interrupt
// (Ctrl+C, a lone Esc) ends any; an answer (anything but an escape
// sequence, which is also how xterm replies to queries) ends blocked and
// done. s.mu is held.
func (s *session) noteInput(data string, now time.Time) bool {
	if s.info.State == "" || data == "" {
		return false
	}
	interrupt := strings.ContainsRune(data, 3) || data == "\x1b"
	answer := data[0] != 0x1b && (s.info.State == StateBlocked || s.info.State == StateDone)
	if interrupt || answer {
		return s.setStateLocked("", now)
	}
	return false
}
