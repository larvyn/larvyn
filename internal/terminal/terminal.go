package terminal

import (
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"larvyn/internal/composer"
	"larvyn/internal/proc"
	"larvyn/internal/store"

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
	ID           string `json:"id"`
	ProjectID    string `json:"projectId"`
	Kind         string `json:"kind"`
	Key          string `json:"key"`
	Title        string `json:"title"`
	Cwd          string `json:"cwd"`
	Command      string `json:"command"`
	Running      bool   `json:"running"`
	ExitCode     int    `json:"exitCode"`
	Stopped      bool   `json:"stopped"`
	StartedAt    int64  `json:"startedAt"`
	EndedAt      int64  `json:"endedAt"`
	LastActivity int64  `json:"lastActivity"`
	Bell         bool   `json:"bell"`
	// Restored is set on a session brought back from the previous run that
	// has not been started again.
	Restored bool              `json:"restored"`
	Meta     map[string]string `json:"meta"`
}

type TermData struct {
	ID   string `json:"id"`
	Seq  uint64 `json:"seq"`
	Data string `json:"data"`
}

type TermExit struct {
	ID      string `json:"id"`
	Code    int    `json:"code"`
	Stopped bool   `json:"stopped"`
}

type Snapshot struct {
	Data string `json:"data"`
	Seq  uint64 `json:"seq"`
}

type session struct {
	mu   sync.Mutex
	info SessionInfo
	// spec is what the session was started with; Relaunch runs it again.
	spec SessionSpec
	argv []string
	pty  pty.Pty
	cmd  *pty.Cmd
	buf  []byte
	seq  uint64
	// starting is set while Relaunch prepares a new process.
	starting bool
	// resolvedAt is when the agent's conversation id was last looked for.
	resolvedAt time.Time
	// interruptAt is when Ctrl+C was last typed; an exit right after it is a stop, not a crash.
	interruptAt time.Time
	// setup are the steps still to run before the session's own program
	// (see StartAfterSetup); started is called once that program runs.
	setup   []string
	started func(SessionInfo)
	// inSetup is set while a setup step runs; step is its command line.
	inSetup bool
	step    string
}

type TerminalService struct {
	mu       sync.Mutex
	sessions map[string]*session
	order    []string
	// persist saves the workspace to disk for the next start (see restore.go);
	// off for the throwaway services tests make.
	persist bool
	ws      workspaceState
}

// Sessions is the app's terminal service; main registers it with Wails and
// the other services start their terminals through it.
var Sessions = &TerminalService{sessions: map[string]*session{}, persist: true}

// Find returns the newest session of a project with this kind and key, or nil.
// It is a function rather than a method so Wails does not bind it.
func Find(projectID, kind, key string) *SessionInfo { return Sessions.find(projectID, kind, key) }

// PID returns the process id of a session's program.
func PID(id string) (int, error) {
	s, err := Sessions.get(id)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.cmd.Process == nil {
		return 0, errors.New("session has no process")
	}
	return s.cmd.Process.Pid, nil
}

func init() {
	application.RegisterEvent[TermData]("term:data")
	application.RegisterEvent[TermExit]("term:exit")
	application.RegisterEvent[[]SessionInfo]("sessions:changed")
}

func emit(name string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data)
	}
}

// Start launches a new terminal session.
func (t *TerminalService) Start(spec SessionSpec) (SessionInfo, error) {
	proc.WaitShellEnv()
	argv, display := resolveArgv(spec)
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
		StartedAt: now, LastActivity: now, Meta: spec.Meta,
	}}
	if s.info.Title == "" {
		s.info.Title = proc.FirstNonEmpty(display, "Terminal")
	}
	if err := t.spawn(s, argv, spec.Cols, spec.Rows); err != nil {
		return SessionInfo{}, err
	}
	t.add(s)
	return s.snapshotInfo(), nil
}

// resolveArgv turns a spec into the program to run and the command line to
// show for it.
func resolveArgv(spec SessionSpec) (argv []string, display string) {
	argv = spec.Argv
	display = spec.Command
	switch {
	case len(argv) > 0:
		argv = composer.Resolve(argv)
		if display == "" {
			display = joinArgs(spec.Argv)
		}
	case spec.Command != "":
		argv = proc.ShellCommand(spec.Command)
	default:
		if sh := store.CurrentSettings().Shell; sh != "" {
			argv = []string{sh}
		} else {
			argv = proc.DefaultShell()
		}
	}
	// go-pty looks a bare program name up relative to the working directory,
	// so resolve it against PATH first.
	if p, err := exec.LookPath(argv[0]); err == nil {
		argv = append([]string{proc.UnwrapShim(p)}, argv[1:]...)
	}
	return argv, display
}

// add registers a session and tells the UI.
func (t *TerminalService) add(s *session) {
	t.mu.Lock()
	t.sessions[s.info.ID] = s
	t.order = append(t.order, s.info.ID)
	t.mu.Unlock()
	t.broadcast()
}

func (s *session) snapshotInfo() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

// spawn runs argv in a new pseudo terminal attached to s, which must not be
// running. The scrollback is kept, so a relaunched session continues below
// its previous output.
func (t *TerminalService) spawn(s *session, argv []string, cols, rows int) error {
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}
	p, err := pty.New()
	if err != nil {
		return err
	}
	_ = p.Resize(cols, rows)
	cmd := p.Command(argv[0], argv[1:]...)
	s.mu.Lock()
	cmd.Dir = s.info.Cwd
	extra := s.spec.Env
	s.mu.Unlock()
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
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		p.Close()
		return err
	}
	now := time.Now().UnixMilli()
	s.mu.Lock()
	s.pty, s.cmd = p, cmd
	s.argv = argv
	s.interruptAt = time.Time{}
	s.info.Running = true
	s.info.Restored = false
	s.info.Stopped = false
	s.info.ExitCode = 0
	s.info.EndedAt = 0
	s.info.StartedAt = now
	s.info.LastActivity = now
	s.mu.Unlock()
	go t.pump(s, p, cmd)
	return nil
}

func (t *TerminalService) pump(s *session, p pty.Pty, cmd *pty.Cmd) {
	// ConPTY keeps its output pipe open after the child exits, so the read
	// loop below only ends once the pseudo console is closed.
	exited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		exited <- err
		time.Sleep(300 * time.Millisecond) // let the last output drain
		p.Close()
	}()
	buf := make([]byte, 32*1024)
	for {
		n, err := p.Read(buf)
		if n > 0 {
			t.output(s, buf[:n])
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
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if t.nextStep(s, code) {
		return
	}
	s.mu.Lock()
	s.info.Running = false
	s.info.ExitCode = code
	s.info.EndedAt = time.Now().UnixMilli()
	if !s.interruptAt.IsZero() && time.Since(s.interruptAt) < 15*time.Second {
		s.info.Stopped = true
	}
	id, stopped := s.info.ID, s.info.Stopped
	s.mu.Unlock()
	t.touch()
	emit("term:exit", TermExit{ID: id, Code: code, Stopped: stopped})
	t.broadcast()
}

// output appends bytes to a session's scrollback and sends them to the UI.
func (t *TerminalService) output(s *session, b []byte) {
	chunk := append([]byte(nil), b...)
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
	t.touchOutput()
	emit("term:data", TermData{ID: id, Seq: seq, Data: base64.StdEncoding.EncodeToString(chunk)})
	if bell {
		t.broadcast()
	}
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
	running, p := s.info.Running, s.pty
	if running && strings.ContainsRune(data, 3) {
		s.interruptAt = time.Now()
	}
	s.mu.Unlock()
	if !running {
		return nil
	}
	_, err = p.Write([]byte(data))
	return err
}

// Resize changes a terminal's size in cells.
func (t *TerminalService) Resize(id string, cols, rows int) error {
	s, err := t.get(id)
	if err != nil || cols <= 0 || rows <= 0 {
		return err
	}
	s.mu.Lock()
	running, p := s.info.Running, s.pty
	s.spec.Cols, s.spec.Rows = cols, rows
	s.mu.Unlock()
	if !running {
		return nil
	}
	return p.Resize(cols, rows)
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
	return proc.StripANSI(string(s.buf)), nil
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
		t.touch()
		t.broadcast()
	}
}

// Kill stops the program in a terminal; the session stays until Close. It
// returns right away; the program and its descendants get a few seconds to
// exit before they are killed outright.
func (t *TerminalService) Kill(id string) error {
	_, err := t.kill(id)
	return err
}

// kill stops a terminal's program and every process it started; done closes
// once they are gone.
func (t *TerminalService) kill(id string) (done <-chan struct{}, err error) {
	s, err := t.get(id)
	if err != nil {
		return nil, err
	}
	ch := make(chan struct{})
	s.mu.Lock()
	running, p, cmd := s.info.Running, s.pty, s.cmd
	if running {
		// A program we kill exits non-zero; Stopped tells it apart from a crash.
		s.info.Stopped = true
	}
	s.mu.Unlock()
	if !running || cmd == nil || cmd.Process == nil {
		close(ch)
		return ch, nil
	}
	pid := cmd.Process.Pid
	// The program leads its own process group (see proc.KillTree); a command
	// typed into an interactive shell runs in the terminal's foreground group.
	groups := []int{pid}
	if up, ok := p.(pty.UnixPty); ok {
		_ = up.Control(func(fd uintptr) {
			if fg := proc.ForegroundGroup(fd); fg != 0 && fg != pid {
				groups = append(groups, fg)
			}
		})
	}
	go func() {
		defer close(ch)
		var wg sync.WaitGroup
		for _, g := range groups {
			wg.Add(1)
			go func() {
				defer wg.Done()
				proc.KillTree(g)
			}()
		}
		wg.Wait()
		_ = cmd.Process.Kill() // no-op once it exited
	}()
	return ch, nil
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
	t.touch()
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

// ServiceShutdown kills every child, with its descendants, when the app quits
// and waits for them so none outlives Larvyn.
func (t *TerminalService) ServiceShutdown() error {
	// Save before killing: what ran is what comes back on the next start.
	t.saveWorkspace()
	t.ws.closed.Store(true)
	var pending []<-chan struct{}
	for _, s := range t.List() {
		if done, err := t.kill(s.ID); err == nil {
			pending = append(pending, done)
		}
	}
	for _, done := range pending {
		<-done
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
