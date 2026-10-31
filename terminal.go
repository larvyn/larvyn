package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const scrollbackBytes = 1 << 20

// SessionSpec describes a terminal to start. Argv runs a program directly;
// Command runs a line through the shell; with neither, an interactive shell.
type SessionSpec struct {
	ProjectID string            `json:"projectId"`
	Kind      string            `json:"kind"` // shell, service, agent, run
	Key       string            `json:"key"`
	Title     string            `json:"title"`
	Cwd       string            `json:"cwd"`
	Argv      []string          `json:"argv"`
	Command   string            `json:"command"`
	Env       map[string]string `json:"env"`
	Cols      int               `json:"cols"`
	Rows      int               `json:"rows"`
	Meta      map[string]string `json:"meta"`
}

// SessionInfo is what the UI knows about a terminal.
type SessionInfo struct {
	ID           string            `json:"id"`
	ProjectID    string            `json:"projectId"`
	Kind         string            `json:"kind"`
	Key          string            `json:"key"`
	Title        string            `json:"title"`
	Cwd          string            `json:"cwd"`
	Command      string            `json:"command"`
	Running      bool              `json:"running"`
	ExitCode     int               `json:"exitCode"`
	StartedAt    int64             `json:"startedAt"`
	EndedAt      int64             `json:"endedAt"`
	LastActivity int64             `json:"lastActivity"`
	Bell         bool              `json:"bell"`
	Meta         map[string]string `json:"meta"`
}

type TermData struct {
	ID   string `json:"id"`
	Seq  uint64 `json:"seq"`
	Data string `json:"data"`
}

type TermExit struct {
	ID   string `json:"id"`
	Code int    `json:"code"`
}

type Snapshot struct {
	Data string `json:"data"`
	Seq  uint64 `json:"seq"`
}

type session struct {
	mu   sync.Mutex
	info SessionInfo
	pty  pty.Pty
	cmd  *pty.Cmd
	buf  []byte
	seq  uint64
}

type TerminalService struct {
	mu       sync.Mutex
	sessions map[string]*session
	order    []string
}

var terminals = &TerminalService{sessions: map[string]*session{}}

func init() {
	application.RegisterEvent[TermData]("term:data")
	application.RegisterEvent[TermExit]("term:exit")
	application.RegisterEvent[[]SessionInfo]("sessions:changed")
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func emit(name string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data)
	}
}

// Start launches a new terminal session.
func (t *TerminalService) Start(spec SessionSpec) (SessionInfo, error) {
	argv := spec.Argv
	display := spec.Command
	switch {
	case len(argv) > 0:
		argv = resolveProgram(argv)
		if display == "" {
			display = joinArgs(spec.Argv)
		}
	case spec.Command != "":
		argv = shellCommand(spec.Command)
	default:
		if sh := store.Settings().Shell; sh != "" {
			argv = []string{sh}
		} else {
			argv = defaultShell()
		}
	}
	// go-pty looks a bare program name up relative to the working directory,
	// so resolve it against PATH first.
	if p, err := exec.LookPath(argv[0]); err == nil {
		argv = append([]string{p}, argv[1:]...)
	}
	if spec.Cwd == "" {
		if p, ok := store.Project(spec.ProjectID); ok {
			spec.Cwd = p.Path
		}
	}
	if spec.Cols <= 0 {
		spec.Cols = 120
	}
	if spec.Rows <= 0 {
		spec.Rows = 32
	}
	if spec.Kind == "" {
		spec.Kind = "shell"
	}

	p, err := pty.New()
	if err != nil {
		return SessionInfo{}, err
	}
	_ = p.Resize(spec.Cols, spec.Rows)
	cmd := p.Command(argv[0], argv[1:]...)
	cmd.Dir = spec.Cwd
	// A terminal can show colour even if Larvyn itself was started with
	// NO_COLOR or TERM=dumb.
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !strings.EqualFold(k, "NO_COLOR") && !strings.EqualFold(k, "TERM") {
			env = append(env, kv)
		}
	}
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "LARVYN=1")
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		p.Close()
		return SessionInfo{}, err
	}

	now := time.Now().UnixMilli()
	s := &session{pty: p, cmd: cmd, info: SessionInfo{
		ID: newID(), ProjectID: spec.ProjectID, Kind: spec.Kind, Key: spec.Key,
		Title: spec.Title, Cwd: spec.Cwd, Command: display, Running: true,
		StartedAt: now, LastActivity: now, Meta: spec.Meta,
	}}
	if s.info.Title == "" {
		s.info.Title = firstNonEmpty(display, "Terminal")
	}
	t.mu.Lock()
	t.sessions[s.info.ID] = s
	t.order = append(t.order, s.info.ID)
	t.mu.Unlock()

	go t.pump(s)
	t.broadcast()
	return s.info, nil
}

func (t *TerminalService) pump(s *session) {
	// ConPTY keeps its output pipe open after the child exits, so the read
	// loop below only ends once the pseudo console is closed.
	exited := make(chan error, 1)
	go func() {
		err := s.cmd.Wait()
		exited <- err
		time.Sleep(300 * time.Millisecond) // let the last output drain
		s.pty.Close()
	}()
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.seq++
			seq := s.seq
			s.buf = append(s.buf, chunk...)
			if len(s.buf) > scrollbackBytes {
				s.buf = append([]byte(nil), s.buf[len(s.buf)-scrollbackBytes:]...)
			}
			s.info.LastActivity = time.Now().UnixMilli()
			bell := !s.info.Bell && containsBell(chunk)
			if bell {
				s.info.Bell = true
			}
			id := s.info.ID
			s.mu.Unlock()
			emit("term:data", TermData{ID: id, Seq: seq, Data: base64.StdEncoding.EncodeToString(chunk)})
			if bell {
				t.broadcast()
			}
		}
		if err != nil {
			break
		}
	}
	code := 0
	if err := <-exited; err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	if s.cmd.ProcessState != nil {
		code = s.cmd.ProcessState.ExitCode()
	}
	s.mu.Lock()
	s.info.Running = false
	s.info.ExitCode = code
	s.info.EndedAt = time.Now().UnixMilli()
	id := s.info.ID
	s.mu.Unlock()
	emit("term:exit", TermExit{ID: id, Code: code})
	t.broadcast()
}

// containsBell finds a BEL that is not the terminator of an OSC sequence
// (window titles end with BEL too and are not a request for attention).
func containsBell(b []byte) bool {
	inOSC := false
	for i, c := range b {
		if c == 0x1b && i+1 < len(b) && b[i+1] == ']' {
			inOSC = true
			continue
		}
		if c == 7 {
			if inOSC {
				inOSC = false
				continue
			}
			return true
		}
	}
	return false
}

func (t *TerminalService) get(id string) (*session, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[id]
	if !ok {
		return nil, errors.New("terminal not found")
	}
	return s, nil
}

// Write sends keyboard input to a terminal.
func (t *TerminalService) Write(id, data string) error {
	s, err := t.get(id)
	if err != nil {
		return err
	}
	// xterm answers terminal queries on its own, even after the program ended.
	s.mu.Lock()
	running := s.info.Running
	s.mu.Unlock()
	if !running {
		return nil
	}
	_, err = s.pty.Write([]byte(data))
	return err
}

// Resize changes a terminal's size in cells.
func (t *TerminalService) Resize(id string, cols, rows int) error {
	s, err := t.get(id)
	if err != nil || cols <= 0 || rows <= 0 {
		return err
	}
	s.mu.Lock()
	running := s.info.Running
	s.mu.Unlock()
	if !running {
		return nil
	}
	return s.pty.Resize(cols, rows)
}

// Snapshot returns the scrollback so a freshly mounted view can catch up.
func (t *TerminalService) Snapshot(id string) (Snapshot, error) {
	s, err := t.get(id)
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{Data: base64.StdEncoding.EncodeToString(s.buf), Seq: s.seq}, nil
}

// Output returns the scrollback as plain text without escape sequences.
func (t *TerminalService) Output(id string) (string, error) {
	s, err := t.get(id)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return stripANSI(string(s.buf)), nil
}

// ClearBell marks a terminal's attention flag as seen.
func (t *TerminalService) ClearBell(id string) {
	if s, err := t.get(id); err == nil {
		s.mu.Lock()
		was := s.info.Bell
		s.info.Bell = false
		s.mu.Unlock()
		if was {
			t.broadcast()
		}
	}
}

// Rename sets a terminal's title.
func (t *TerminalService) Rename(id, title string) {
	if s, err := t.get(id); err == nil {
		s.mu.Lock()
		s.info.Title = title
		s.mu.Unlock()
		t.broadcast()
	}
}

// Kill stops the program in a terminal; the session stays until Close.
func (t *TerminalService) Kill(id string) error {
	s, err := t.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	running := s.info.Running
	s.mu.Unlock()
	if running && s.cmd.Process != nil {
		killTree(s.cmd.Process.Pid)
		_ = s.cmd.Process.Kill()
	}
	return nil
}

// Close kills a terminal and forgets it.
func (t *TerminalService) Close(id string) error {
	_ = t.Kill(id)
	t.mu.Lock()
	delete(t.sessions, id)
	for i, o := range t.order {
		if o == id {
			t.order = append(t.order[:i], t.order[i+1:]...)
			break
		}
	}
	t.mu.Unlock()
	t.broadcast()
	return nil
}

// List returns every terminal, oldest first.
func (t *TerminalService) List() []SessionInfo {
	t.mu.Lock()
	all := make([]*session, 0, len(t.order))
	for _, id := range t.order {
		all = append(all, t.sessions[id])
	}
	t.mu.Unlock()
	out := make([]SessionInfo, 0, len(all))
	for _, s := range all {
		s.mu.Lock()
		out = append(out, s.info)
		s.mu.Unlock()
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	return out
}

func (t *TerminalService) find(projectID, kind, key string) *SessionInfo {
	for _, s := range t.List() {
		if s.ProjectID == projectID && s.Kind == kind && s.Key == key {
			s := s
			return &s
		}
	}
	return nil
}

func (t *TerminalService) broadcast() {
	emit("sessions:changed", t.List())
}

// ServiceShutdown kills every child when the app quits.
func (t *TerminalService) ServiceShutdown() error {
	for _, s := range t.List() {
		_ = t.Kill(s.ID)
	}
	return nil
}

func joinArgs(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\"") {
			parts[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}
