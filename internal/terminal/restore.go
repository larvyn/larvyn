package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"larvyn/internal/proc"
	"larvyn/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Restoring the workspace after a restart (issue #21): on quit, and every few
// seconds while something changes, the terminals, their layout and the tail of
// their scrollback are written to workspace.json. On the next start shells
// come back in the same folder below their old output, agents resume their
// conversation, services that ran start again and one-off runs only show
// their last output.

const (
	workspaceFile    = "workspace.json"
	workspaceVersion = 1
	// restoreTailBytes is how much scrollback each terminal keeps across a
	// restart.
	restoreTailBytes = 200 << 10
	// autosaveEvery is how often a change to the sessions or the layout is
	// saved; new output alone is saved at most every scrollbackSaveEvery,
	// since busy services print all the time.
	autosaveEvery       = 15 * time.Second
	scrollbackSaveEvery = 2 * time.Minute
	// resolveEvery spaces out the searches for an agent's conversation id.
	resolveEvery = time.Minute
)

// TabLayout is one terminal tab: its panes (session ids) and split direction.
type TabLayout struct {
	ID    string   `json:"id"`
	Panes []string `json:"panes"`
	Dir   string   `json:"dir"`
}

// ProjectLayout is a project's terminal tabs and the one in front.
type ProjectLayout struct {
	Tabs   []TabLayout `json:"tabs"`
	Active string      `json:"active"`
}

type savedSession struct {
	ID        string            `json:"id"`
	ProjectID string            `json:"projectId"`
	Kind      string            `json:"kind"`
	Key       string            `json:"key"`
	Title     string            `json:"title"`
	Cwd       string            `json:"cwd"`
	Argv      []string          `json:"argv,omitempty"`
	Command   string            `json:"command,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	Cols      int               `json:"cols,omitempty"`
	Rows      int               `json:"rows,omitempty"`
	Running   bool              `json:"running"`
	ExitCode  int               `json:"exitCode"`
	Stopped   bool              `json:"stopped"`
	// LastActivity tells which of the agents sharing a folder talked last.
	LastActivity int64  `json:"lastActivity,omitempty"`
	Scrollback   []byte `json:"scrollback,omitempty"`
}

type savedWorkspace struct {
	Version  int                      `json:"version"`
	SavedAt  int64                    `json:"savedAt"`
	Sessions []savedSession           `json:"sessions"`
	Layout   map[string]ProjectLayout `json:"layout"`
}

// workspaceState is the TerminalService's part in saving and restoring.
type workspaceState struct {
	mu     sync.Mutex
	layout map[string]ProjectLayout
	// restored is set once Restore ran; nothing is saved before, or a start
	// whose window never loaded would wipe the previous workspace.
	restored atomic.Bool
	// closed is set once the save on quit is written: what the dying
	// processes do afterwards must not overwrite it.
	closed atomic.Bool
	// dirty: sessions or layout changed; outDirty: only new output.
	dirty    atomic.Bool
	outDirty atomic.Bool
	once     sync.Once
	saveMu   sync.Mutex
	lastSave time.Time // under saveMu
}

// AgentRelaunch builds the command that starts a stopped agent session
// again, and tells whether it resumes the agent's conversation. shared is set
// when another session of the same agent in the same folder talked more
// recently: an agent that resumes "the latest conversation here" would pick
// that one up, so it must start afresh. main sets it: the agents live there.
var AgentRelaunch = func(spec SessionSpec, shared bool) (argv []string, resumes bool, err error) {
	return nil, false, errors.New("agents cannot be relaunched")
}

// ResolveResumeID finds the conversation id of an agent that does not let
// Larvyn pick it (Codex), for a session of agent key in cwd whose process
// group is pid (0 once it stopped), started at startedAt (Unix ms), skipping
// the ids in claimed. exact is set when the id comes from the file the
// agent has open rather than a guess by folder and time. main sets it.
var ResolveResumeID = func(key, cwd string, pid int, startedAt int64, claimed map[string]bool) (id string, exact bool) {
	return "", false
}

func firstNonZero(a, b int64) int64 {
	if a != 0 {
		return a
	}
	return b
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func workspacePath() string { return filepath.Join(store.HomeDir(), workspaceFile) }

// touch marks the sessions or the layout as changed since the last save.
func (t *TerminalService) touch() { t.ws.dirty.Store(true) }

// touchOutput marks new output since the last save.
func (t *TerminalService) touchOutput() { t.ws.outDirty.Store(true) }

// autosave saves when the sessions changed, or when only output did and the
// last save is a while ago.
func (t *TerminalService) autosave() {
	t.ws.saveMu.Lock()
	stale := time.Since(t.ws.lastSave) >= scrollbackSaveEvery
	t.ws.saveMu.Unlock()
	if t.ws.dirty.Swap(false) || (stale && t.ws.outDirty.Load()) {
		t.saveWorkspace()
	}
}

// ServiceStartup saves the workspace now and then, so a crash loses little.
func (t *TerminalService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if !t.persist {
		return nil
	}
	go func() {
		tick := time.NewTicker(autosaveEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				t.autosave()
			}
		}
	}()
	return nil
}

// SetLayout records the terminal tabs of every project, so they can be put
// back after a restart.
func (t *TerminalService) SetLayout(layout map[string]ProjectLayout) {
	t.ws.mu.Lock()
	same := reflect.DeepEqual(t.ws.layout, layout)
	t.ws.layout = layout
	t.ws.mu.Unlock()
	if !same {
		t.touch()
	}
}

// Restore brings back the workspace saved when Larvyn last quit; it does so
// once per run. It returns the terminal layout to show, with the ids of the
// restored sessions, also when the window loads again later.
func (t *TerminalService) Restore() map[string]ProjectLayout {
	t.ws.once.Do(func() {
		defer t.ws.restored.Store(true)
		if !t.persist {
			return
		}
		if store.CurrentSettings().NoRestore {
			_ = os.Remove(workspacePath())
			return
		}
		b, err := os.ReadFile(workspacePath())
		if err != nil {
			return
		}
		var ws savedWorkspace
		if err := json.Unmarshal(b, &ws); err != nil || ws.Version != workspaceVersion {
			store.AppendLog("warn", "workspace.json is unreadable, starting empty")
			return
		}
		layout := t.restoreWorkspace(ws)
		t.ws.mu.Lock()
		if len(t.ws.layout) == 0 {
			t.ws.layout = layout
		}
		t.ws.mu.Unlock()
	})
	return t.currentLayout()
}

// currentLayout is the last layout, without panes whose session is gone.
func (t *TerminalService) currentLayout() map[string]ProjectLayout {
	t.mu.Lock()
	alive := make(map[string]bool, len(t.sessions))
	for id := range t.sessions {
		alive[id] = true
	}
	t.mu.Unlock()
	t.ws.mu.Lock()
	defer t.ws.mu.Unlock()
	return remapLayout(t.ws.layout, func(id string) (string, bool) { return id, alive[id] })
}

// remapLayout renames every pane with fn and drops the panes fn rejects, the
// tabs left empty and an active tab that no longer exists.
func remapLayout(in map[string]ProjectLayout, fn func(string) (string, bool)) map[string]ProjectLayout {
	out := map[string]ProjectLayout{}
	for pid, pl := range in {
		var tabs []TabLayout
		active := ""
		for _, tab := range pl.Tabs {
			var panes []string
			for _, p := range tab.Panes {
				if id, ok := fn(p); ok {
					panes = append(panes, id)
				}
			}
			if len(panes) == 0 {
				continue
			}
			if tab.Dir != "col" {
				tab.Dir = "row"
			}
			tabs = append(tabs, TabLayout{ID: tab.ID, Panes: panes, Dir: tab.Dir})
			if tab.ID == pl.Active {
				active = tab.ID
			}
		}
		if len(tabs) == 0 {
			continue
		}
		if active == "" {
			active = tabs[0].ID
		}
		out[pid] = ProjectLayout{Tabs: tabs, Active: active}
	}
	return out
}

// restoreWorkspace recreates the saved sessions and returns the saved layout
// with their new ids.
func (t *TerminalService) restoreWorkspace(ws savedWorkspace) map[string]ProjectLayout {
	ids := map[string]string{}
	var all []*session
	runs := map[*session]bool{}
	for _, saved := range ws.Sessions {
		s, run := t.restoreSession(saved, ws.SavedAt)
		if s == nil {
			continue
		}
		ids[saved.ID] = s.info.ID
		all = append(all, s)
		runs[s] = run
	}
	// Decide who shares a folder before anything starts: starting refreshes
	// the activity that decision is based on.
	shared := map[*session]bool{}
	for _, s := range all {
		shared[s] = sharesFolder(s, all)
	}
	// Only an agent that picks its conversation up comes back on its own;
	// the rest wait for Relaunch. Agents never replay their saved output
	// (see restoreSession), so one that cannot come back says why.
	for _, s := range all {
		if !runs[s] || s.spec.Kind != "agent" {
			continue
		}
		_, resumes, err := AgentRelaunch(s.spec, shared[s])
		switch {
		case err != nil:
			runs[s] = false
			s.buf = []byte("\x1b[31m" + err.Error() + "\x1b[0m\r\n")
		case !resumes:
			runs[s] = false
		default:
			s.buf = []byte("\x1b[2m── resuming the previous conversation ──\x1b[0m\r\n")
		}
	}
	for _, s := range all {
		t.add(s)
	}
	for _, s := range all {
		if !runs[s] {
			continue
		}
		if err := t.relaunch(s, shared[s]); err != nil {
			t.output(s, []byte("\x1b[31mCould not restart: "+err.Error()+"\x1b[0m\r\n"))
			store.AppendLog("warn", "restore "+s.info.Title+": "+err.Error())
		}
	}
	return remapLayout(ws.Layout, func(old string) (string, bool) {
		id, ok := ids[old]
		return id, ok
	})
}

// restoreSession turns a saved session into a stopped one holding its old
// output, or nil when its project or folder is gone. run tells whether to
// start it again right away.
func (t *TerminalService) restoreSession(saved savedSession, savedAt int64) (s *session, run bool) {
	p, ok := store.FindProject(saved.ProjectID)
	if !ok || saved.Key == "create" {
		return nil, false
	}
	cwd := saved.Cwd
	if !isDir(cwd) {
		// An agent's worktree that was removed takes the agent with it; the
		// rest falls back to the project folder.
		if saved.Kind == "agent" || !isDir(p.Path) {
			return nil, false
		}
		cwd = p.Path
	}
	spec := SessionSpec{
		ProjectID: saved.ProjectID, Kind: saved.Kind, Key: saved.Key, Title: saved.Title, Cwd: cwd,
		Argv: saved.Argv, Command: saved.Command, Env: saved.Env, Cols: saved.Cols, Rows: saved.Rows, Meta: saved.Meta,
	}
	if spec.Kind == "" {
		spec.Kind = "shell"
	}
	now := time.Now().UnixMilli()
	command := saved.Command
	if command == "" && len(saved.Argv) > 0 {
		command = joinArgs(saved.Argv)
	}
	s = &session{spec: spec, info: SessionInfo{
		ID: store.NewID(), ProjectID: spec.ProjectID, Kind: spec.Kind, Key: spec.Key, Title: spec.Title, Cwd: cwd,
		Command: command, ExitCode: saved.ExitCode, Stopped: saved.Stopped || saved.Running, Restored: true,
		StartedAt: now, EndedAt: now, LastActivity: firstNonZero(saved.LastActivity, now), Meta: spec.Meta,
	}}
	// An agent's TUI redraws in place with relative cursor moves: a cut tail
	// of that, replayed at another size, is unreadable. Its conversation is
	// kept by the agent and comes back on resume, so it starts clean.
	if spec.Kind != "agent" {
		s.buf = restoredScrollback(saved.Scrollback, savedAt)
	}
	s.seq = 1
	if !saved.Running {
		return s, false
	}
	switch spec.Kind {
	case "service":
		run = true
	case "shell":
		// A shell comes back; a program opened as a shell tab (tinker, a
		// command from LARVYN_RUN) waits for Relaunch.
		run = len(spec.Argv) == 0 && spec.Command == ""
	case "agent":
		run = true // if it resumes; see restoreWorkspace
	}
	return s, run
}

// sharesFolder tells whether another session of the same agent works in the
// same folder and talked after s (or at the same time and started later).
func sharesFolder(s *session, all []*session) bool {
	s.mu.Lock()
	kind, key, cwd, last := s.info.Kind, s.info.Key, s.info.Cwd, s.info.LastActivity
	s.mu.Unlock()
	if kind != "agent" {
		return false
	}
	after := false
	for _, o := range all {
		if o == s {
			after = true
			continue
		}
		o.mu.Lock()
		same := o.info.Kind == "agent" && o.info.Key == key && proc.SamePath(o.info.Cwd, cwd)
		olast := o.info.LastActivity
		o.mu.Unlock()
		if same && (olast > last || (olast == last && after)) {
			return true
		}
	}
	return false
}

// restoredScrollback is the saved output to show again: cut at a line start,
// then the terminal modes a TUI may have left on are reset, and a dim line
// marks where the old output ends.
func restoredScrollback(b []byte, savedAt int64) []byte {
	if len(b) == 0 {
		return nil
	}
	if len(b) > restoreTailBytes {
		b = cleanCut(b[len(b)-restoreTailBytes:])
	}
	when := ""
	if savedAt > 0 {
		when = " · " + time.UnixMilli(savedAt).Format("Jan 2 15:04")
	}
	out := append([]byte(nil), b...)
	// Leave the alternate screen, show the cursor, stop mouse and focus
	// reporting, bracketed paste and application keys, reset colours. Focus
	// reporting left on would type ESC[I into the new shell on every focus.
	out = append(out, "\x1b[?1049l\x1b[?25h\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1004l\x1b[?2004l\x1b[?1l\x1b>\x1b[0m"...)
	out = append(out, "\r\n\x1b[2m── restored from the previous session"+when+" ──\x1b[0m\r\n"...)
	return out
}

// cleanCut drops the start of a tail cut out of a longer output, so the
// replay does not begin in the middle of a line, an escape sequence or a
// UTF-8 character: it starts after the first newline, or else at the first
// escape, or else at the first whole character. Full-screen programs redraw
// with cursor moves and may print no newline for a long time.
func cleanCut(b []byte) []byte {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return b[i+1:]
	}
	if i := bytes.IndexByte(b, 0x1b); i >= 0 {
		return b[i:]
	}
	for len(b) > 0 && !utf8.RuneStart(b[0]) {
		b = b[1:]
	}
	return b
}

// Relaunch starts a stopped terminal's program again in the same session:
// an agent resumes its conversation when it can, anything else runs its
// command again.
func (t *TerminalService) Relaunch(id string) (SessionInfo, error) {
	s, err := t.get(id)
	if err != nil {
		return SessionInfo{}, err
	}
	if err := t.relaunch(s, sharesFolder(s, t.all())); err != nil {
		return SessionInfo{}, err
	}
	return s.snapshotInfo(), nil
}

// all returns every session, oldest first.
func (t *TerminalService) all() []*session {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*session, 0, len(t.order))
	for _, id := range t.order {
		out = append(out, t.sessions[id])
	}
	return out
}

func (t *TerminalService) relaunch(s *session, shared bool) error {
	s.mu.Lock()
	if s.info.Running || s.starting {
		s.mu.Unlock()
		return errors.New("the terminal is already running")
	}
	// starting keeps a second click from spawning a second process while
	// this one looks up its program; spawn sets Running before it clears.
	s.starting = true
	spec := s.spec
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.starting = false
		s.mu.Unlock()
	}()
	if spec.Key == "create" {
		return errors.New("start the installer again from New app")
	}
	if !isDir(spec.Cwd) {
		return errors.New("folder not found: " + spec.Cwd)
	}
	// At boot the PATH from the login shell may still be loading.
	proc.WaitShellEnv()
	if spec.Kind == "agent" {
		a, _, err := AgentRelaunch(spec, shared)
		if err != nil {
			return err
		}
		spec.Argv = a
	}
	// Through resolveArgv like any start, so a Windows .cmd shim is
	// unwrapped instead of running under cmd.exe.
	argv, _ := resolveArgv(spec)
	if err := t.spawn(s, argv, spec.Cols, spec.Rows); err != nil {
		return err
	}
	t.touch()
	t.broadcast()
	return nil
}

// saveWorkspace writes the sessions and the layout to workspace.json, or
// removes it when restoring is turned off.
func (t *TerminalService) saveWorkspace() {
	if !t.persist || !t.ws.restored.Load() {
		return
	}
	t.ws.saveMu.Lock()
	defer t.ws.saveMu.Unlock()
	if t.ws.closed.Load() {
		return
	}
	if store.CurrentSettings().NoRestore {
		_ = os.Remove(workspacePath())
		return
	}
	all := t.all()
	ws := savedWorkspace{Version: workspaceVersion, SavedAt: time.Now().UnixMilli(), Sessions: []savedSession{}}
	claimed := map[string]bool{}
	for _, s := range all {
		if id := s.resumeID(); id != "" {
			claimed[id] = true
		}
	}
	for _, s := range all {
		s.resolveResumeID(claimed)
		cwd := s.shellCwd()
		s.mu.Lock()
		if s.info.ProjectID == "" || s.info.Key == "create" {
			s.mu.Unlock()
			continue
		}
		if cwd == "" {
			cwd = s.info.Cwd
		}
		buf := s.buf
		if len(buf) > restoreTailBytes {
			buf = buf[len(buf)-restoreTailBytes:]
		}
		ws.Sessions = append(ws.Sessions, savedSession{
			ID: s.info.ID, ProjectID: s.info.ProjectID, Kind: s.info.Kind, Key: s.info.Key, Title: s.info.Title,
			Cwd: cwd, Argv: s.spec.Argv, Command: s.spec.Command, Env: s.spec.Env, Meta: s.info.Meta,
			Cols: s.spec.Cols, Rows: s.spec.Rows, Running: s.info.Running, ExitCode: s.info.ExitCode, Stopped: s.info.Stopped,
			LastActivity: s.info.LastActivity, Scrollback: append([]byte(nil), buf...),
		})
		s.mu.Unlock()
	}
	alive := map[string]bool{}
	for _, s := range ws.Sessions {
		alive[s.ID] = true
	}
	t.ws.mu.Lock()
	ws.Layout = remapLayout(t.ws.layout, func(id string) (string, bool) { return id, alive[id] })
	t.ws.mu.Unlock()

	b, err := json.Marshal(ws)
	if err != nil {
		return
	}
	if err := writePrivate(workspacePath(), b); err != nil {
		store.AppendLog("warn", "save workspace: "+err.Error())
		return
	}
	t.ws.lastSave = time.Now()
	t.ws.outDirty.Store(false)
}

// shellCwd is the folder an interactive shell is in now (after any cd), or
// "" when it is not one, stopped, or the system does not tell.
func (s *session) shellCwd() string {
	s.mu.Lock()
	interactive := s.info.Kind == "shell" && len(s.spec.Argv) == 0 && s.spec.Command == ""
	pid := 0
	if interactive && s.info.Running && s.cmd != nil && s.cmd.Process != nil {
		pid = s.cmd.Process.Pid
	}
	s.mu.Unlock()
	if pid == 0 {
		return ""
	}
	if cwd := proc.Cwd(pid); isDir(cwd) {
		return cwd
	}
	return ""
}

// writePrivate replaces a file atomically, readable only by the user: the
// scrollback may hold secrets.
func writePrivate(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *session) resumeID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info.Meta["resumeId"]
}

// resolveResumeID finds the conversation of an agent that does not let
// Larvyn pick its session id (Codex) and keeps it in the session's meta.
//
// A guess (by folder and time) is checked again while the agent runs, since
// a codex started elsewhere in the same folder could have been taken for it.
// Searches are spaced out by resolveEvery.
func (s *session) resolveResumeID(claimed map[string]bool) {
	s.mu.Lock()
	kind, key, cwd, started := s.info.Kind, s.info.Key, s.info.Cwd, s.info.StartedAt
	cur, guess := s.info.Meta["resumeId"], s.info.Meta["resumeGuess"] == "1"
	pid := 0
	if s.info.Running && s.cmd != nil && s.cmd.Process != nil {
		pid = s.cmd.Process.Pid
	}
	due := time.Since(s.resolvedAt) >= resolveEvery
	if kind == "agent" && (cur == "" || guess) && due {
		s.resolvedAt = time.Now()
	}
	s.mu.Unlock()
	if kind != "agent" || (cur != "" && !guess) || !due {
		return
	}
	if cur != "" && pid == 0 {
		return // a guess can only be checked against a running agent
	}
	delete(claimed, cur)
	id, exact := ResolveResumeID(key, cwd, pid, started, claimed)
	if id == "" || (cur != "" && !exact) {
		if cur != "" {
			claimed[cur] = true
		}
		return
	}
	claimed[id] = true
	s.mu.Lock()
	// The map is shared with snapshots handed out by List: copy, never write.
	meta := make(map[string]string, len(s.info.Meta)+1)
	for k, v := range s.info.Meta {
		meta[k] = v
	}
	meta["resumeId"] = id
	if exact {
		delete(meta, "resumeGuess")
	} else {
		meta["resumeGuess"] = "1"
	}
	s.info.Meta, s.spec.Meta = meta, meta
	s.mu.Unlock()
}
