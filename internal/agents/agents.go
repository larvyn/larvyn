package agents

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// AgentKind is a coding agent CLI Larvyn knows how to launch.
type AgentKind struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Binary      string `json:"binary"`
	Installed   bool   `json:"installed"`
	Path        string `json:"path"`
	Install     string `json:"install"`
	PromptAsArg bool   `json:"promptAsArg"`
	// PromptFlag goes before the prompt argument, for CLIs that take the
	// first message through an option (agy -i "…").
	PromptFlag string `json:"promptFlag"`
	Color      string `json:"color"`
}

var agentKinds = []AgentKind{
	{ID: "claude", Name: "Claude Code", Binary: "claude", Install: "npm i -g @anthropic-ai/claude-code", PromptAsArg: true, Color: "#D97757"},
	{ID: "codex", Name: "Codex", Binary: "codex", Install: "npm i -g @openai/codex", PromptAsArg: true, Color: "#10A37F"},
	{ID: "opencode", Name: "OpenCode", Binary: "opencode", Install: "npm i -g opencode-ai", Color: "#F5A623"},
	{ID: "antigravity", Name: "Antigravity CLI", Binary: "agy", Install: "curl -fsSL https://antigravity.google/cli | bash", PromptAsArg: true, PromptFlag: "-i", Color: "#4285F4"},
	{ID: "cursor-agent", Name: "Cursor Agent", Binary: "cursor-agent", Install: "see cursor.com/cli", PromptAsArg: true, Color: "#A3A3A3"},
	{ID: "aider", Name: "Aider", Binary: "aider", Install: "pip install aider-install", Color: "#14B8A6"},
	{ID: "pi", Name: "Pi", Binary: "pi", Install: "npm i -g @mariozechner/pi-coding-agent", Color: "#8B5CF6"},
}

// yoloFlags start each agent without approval prompts (the same flags Orca
// uses for its "Yolo" agent permissions). Agents missing here have none.
var yoloFlags = map[string][]string{
	"claude":       {"--dangerously-skip-permissions"},
	"codex":        {"--dangerously-bypass-approvals-and-sandbox"},
	"opencode":     {"--dangerously-skip-permissions"},
	"antigravity":  {"--dangerously-skip-permissions"},
	"cursor-agent": {"--yolo"},
	"aider":        {"--yes-always"},
}

// resumeArgs are the arguments that make each agent continue its previous
// conversation in the same folder. Agents missing here start afresh.
var resumeArgs = map[string]func(meta map[string]string) []string{
	"claude": func(meta map[string]string) []string {
		id := meta["resumeId"]
		switch {
		case id == "":
			return []string{"--continue"}
		case claudeSessionExists(id):
			return []string{"--resume", id}
		default:
			// Nothing was said yet, so there is nothing to resume: start the
			// conversation under the same id.
			return []string{"--session-id", id}
		}
	},
	"codex": func(meta map[string]string) []string {
		if id := meta["resumeId"]; id != "" {
			return []string{"resume", id}
		}
		return nil // no conversation was recorded: start a new one
	},
	"opencode":    func(map[string]string) []string { return []string{"--continue"} },
	"antigravity": func(map[string]string) []string { return []string{"--continue"} },
	"aider":       func(map[string]string) []string { return []string{"--restore-chat-history"} },
	"pi":          func(map[string]string) []string { return []string{"--continue"} },
}

// resumesByFolder are the agents that resume "the latest conversation in this
// folder" rather than one by id: of several sessions sharing a folder, only
// the one that talked last may resume, or they would all continue its
// conversation.
var resumesByFolder = map[string]bool{"opencode": true, "antigravity": true, "aider": true, "pi": true}

// RelaunchArgv is the command that starts a stopped agent session again,
// with its permission flags and without the first prompt. resumes tells
// whether the agent can pick its conversation up.
func RelaunchArgv(spec terminal.SessionSpec, shared bool) (argv []string, resumes bool, err error) {
	var kind *AgentKind
	for _, k := range agentKinds {
		if k.ID == spec.Key {
			k := k
			kind = &k
		}
	}
	if kind == nil {
		return nil, false, errors.New("unknown agent " + spec.Key)
	}
	path := proc.Which(kind.Binary)
	if path == "" {
		return nil, false, errors.New(kind.Name + " is not installed: " + kind.Install)
	}
	argv = []string{path}
	if spec.Meta["yolo"] == "1" {
		argv = append(argv, yoloFlags[kind.ID]...)
	}
	if fn, ok := resumeArgs[kind.ID]; ok && !(shared && resumesByFolder[kind.ID]) {
		resumes = true
		argv = append(argv, fn(spec.Meta)...)
	}
	return argv, resumes, nil
}

// claudeConfigDir is where Claude Code keeps its conversations.
func claudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// claudeSessionExists tells whether Claude Code recorded a conversation
// under id (in projects/<folder>/<id>.jsonl).
func claudeSessionExists(id string) bool {
	m, _ := filepath.Glob(filepath.Join(claudeConfigDir(), "projects", "*", id+".jsonl"))
	return len(m) > 0
}

// codexHome is where Codex keeps its conversations.
func codexHome() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// isCodexRollout tells whether path is one of Codex's conversation files.
func isCodexRollout(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, "rollout-") && strings.HasSuffix(base, ".jsonl")
}

// CodexResumeID finds the conversation of a Codex session. While it runs,
// the rollout file its processes have open names it exactly; otherwise (or
// where open files cannot be listed) it is guessed by folder and time.
func CodexResumeID(cwd string, pid int, startedAt int64, claimed map[string]bool) (id string, exact bool) {
	if pid > 0 {
		if files, ok := proc.GroupOpenFiles(pid, isCodexRollout); ok {
			for _, f := range files {
				if id, _, _, ok := codexSessionMeta(f); ok {
					return id, true
				}
			}
		}
	}
	return codexSessionID(cwd, startedAt, claimed), false
}

// codexSessionID finds the Codex conversation started in cwd at or after
// startedAt (Unix ms) that no other session claimed: Codex writes each one to
// sessions/YYYY/MM/DD/rollout-*.jsonl, whose first line holds its id and cwd.
func codexSessionID(cwd string, startedAt int64, claimed map[string]bool) string {
	root := codexHome()
	start := time.UnixMilli(startedAt)
	// Folders and file names are in local time, like start.
	from := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.Local)
	best, bestAt := "", time.Time{}
	for day := from; !day.After(time.Now()); day = day.AddDate(0, 0, 1) {
		files, _ := filepath.Glob(filepath.Join(root, "sessions", day.Format("2006"), day.Format("01"), day.Format("02"), "rollout-*.jsonl"))
		for _, f := range files {
			// rollout-2006-01-02T15-04-05-<id>.jsonl: skip older ones unread.
			if name := strings.TrimPrefix(filepath.Base(f), "rollout-"); len(name) >= 19 {
				if at, err := time.ParseInLocation("2006-01-02T15-04-05", name[:19], time.Local); err == nil && at.Before(start.Add(-time.Minute)) {
					continue
				}
			}
			id, dir, at, ok := codexSessionMeta(f)
			if !ok || claimed[id] || !proc.SamePath(dir, cwd) || at.UnixMilli() < startedAt-5000 {
				continue
			}
			if best == "" || at.Before(bestAt) {
				best, bestAt = id, at
			}
		}
	}
	return best
}

func codexSessionMeta(path string) (id, cwd string, at time.Time, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(f, 64<<10).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var rec struct {
		Type    string `json:"type"`
		Payload struct {
			ID        string `json:"id"`
			Cwd       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &rec) != nil || rec.Type != "session_meta" || rec.Payload.ID == "" {
		return
	}
	at, err = time.Parse(time.RFC3339Nano, rec.Payload.Timestamp)
	if err != nil {
		return
	}
	return rec.Payload.ID, rec.Payload.Cwd, at, true
}

// newUUID returns a random (version 4) UUID.
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

type AgentService struct{}

// CreateWorktree makes a git worktree for an agent and returns its folder and
// branch. main wires it to the git service, so this package does not import it.
var CreateWorktree = func(projectID, name, base string) (path, branch string, err error) {
	return "", "", errors.New("worktrees are not available")
}

// HasComposer reports whether Composer can run (from PATH or Larvyn's own
// phar). main wires it; the default only looks on PATH.
var HasComposer = func() bool { return proc.Which("composer") != "" }

// Kinds lists the known agents and whether each is on PATH.
func (a *AgentService) Kinds() []AgentKind {
	out := make([]AgentKind, len(agentKinds))
	for i, k := range agentKinds {
		k.Path = proc.Which(k.Binary)
		k.Installed = k.Path != ""
		out[i] = k
	}
	return out
}

// LaunchRequest starts one agent, optionally in a fresh worktree.
type LaunchRequest struct {
	ProjectID   string `json:"projectId"`
	Agent       string `json:"agent"`
	Prompt      string `json:"prompt"`
	Title       string `json:"title"`
	Worktree    bool   `json:"worktree"`
	Cwd         string `json:"cwd"`
	InstallDeps bool   `json:"installDeps"`
	Base        string `json:"base"`
}

// Launch starts an agent session. With Worktree set the agent works on its
// own branch, so several can attack one task in parallel.
func (a *AgentService) Launch(req LaunchRequest) (terminal.SessionInfo, error) {
	p, ok := store.FindProject(req.ProjectID)
	if !ok {
		return terminal.SessionInfo{}, errors.New("project not found")
	}
	var kind *AgentKind
	for _, k := range a.Kinds() {
		if k.ID == req.Agent {
			k := k
			kind = &k
		}
	}
	if kind == nil {
		return terminal.SessionInfo{}, errors.New("unknown agent " + req.Agent)
	}
	if !kind.Installed {
		return terminal.SessionInfo{}, errors.New(kind.Name + " is not installed: " + kind.Install)
	}
	title := req.Title
	if title == "" {
		title = firstLine(req.Prompt, 48)
	}
	if title == "" {
		title = kind.Name
	}
	cwd := proc.FirstNonEmpty(req.Cwd, p.Path)
	meta := map[string]string{"agent": kind.ID, "agentName": kind.Name, "color": kind.Color, "prompt": req.Prompt}
	if req.Worktree {
		path, branch, err := CreateWorktree(p.ID, kind.ID+"-"+title, req.Base)
		if err != nil {
			return terminal.SessionInfo{}, err
		}
		cwd = path
		meta["worktree"] = path
		meta["branch"] = branch
		if req.InstallDeps {
			a.installDeps(p, cwd)
		}
	}
	argv := []string{kind.Path}
	if f := yoloFlags[kind.ID]; len(f) > 0 && store.CurrentSettings().AgentPermissions != "manual" {
		argv = append(argv, f...)
		meta["yolo"] = "1"
	}
	// Claude Code takes the conversation id from us, so it can be resumed
	// after Larvyn restarts.
	if kind.ID == "claude" {
		id := newUUID()
		argv = append(argv, "--session-id", id)
		meta["resumeId"] = id
	}
	// cmd.exe re-parses the arguments of .cmd shims, so a prompt with & or |
	// in it would run as a command: type it into the agent instead.
	ext := strings.ToLower(filepath.Ext(kind.Path))
	if ext == ".cmd" || ext == ".bat" {
		kind.PromptAsArg = false
	}
	if req.Prompt != "" && kind.PromptAsArg {
		if kind.PromptFlag != "" {
			argv = append(argv, kind.PromptFlag)
		}
		argv = append(argv, req.Prompt)
	}
	sess, err := terminal.Sessions.Start(terminal.SessionSpec{ProjectID: p.ID, Kind: "agent", Key: kind.ID, Title: title, Cwd: cwd, Argv: argv, Meta: meta})
	if err != nil {
		return sess, err
	}
	if req.Prompt != "" && !kind.PromptAsArg {
		go func(id, prompt string) {
			// Give the TUI time to draw its input box before typing into it.
			time.Sleep(2500 * time.Millisecond)
			_ = terminal.Sessions.Write(id, bracketedPaste(prompt))
			time.Sleep(150 * time.Millisecond)
			_ = terminal.Sessions.Write(id, "\r")
		}(sess.ID, req.Prompt)
	}
	return sess, nil
}

func (a *AgentService) installDeps(p store.Project, dir string) {
	if _, err := os.Stat(filepath.Join(dir, "composer.json")); err == nil && HasComposer() {
		_, _ = terminal.Sessions.Start(terminal.SessionSpec{ProjectID: p.ID, Kind: "run", Key: "deps", Title: "composer install · " + filepath.Base(dir),
			Cwd: dir, Argv: []string{"composer", "install", "--no-interaction"}})
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		pm := proc.PackageManager(dir)
		_, _ = terminal.Sessions.Start(terminal.SessionSpec{ProjectID: p.ID, Kind: "run", Key: "deps", Title: pm + " install · " + filepath.Base(dir),
			Cwd: dir, Argv: []string{pm, "install"}})
	}
}

// Fanout starts the same prompt in several agents, each in its own worktree.
func (a *AgentService) Fanout(req LaunchRequest, agents []string) ([]terminal.SessionInfo, error) {
	var out []terminal.SessionInfo
	var errs []error
	for _, id := range agents {
		r := req
		r.Agent = id
		r.Worktree = true
		s, err := a.Launch(r)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, s)
	}
	return out, errors.Join(errs...)
}

// Send types a follow-up message into an agent and presses enter.
func (a *AgentService) Send(sessionID, text string) error {
	if err := terminal.Sessions.Write(sessionID, bracketedPaste(text)); err != nil {
		return err
	}
	time.Sleep(120 * time.Millisecond)
	return terminal.Sessions.Write(sessionID, "\r")
}

func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return s
}

// bracketedPaste wraps text so a TUI takes newlines in it as text, not enter.
func bracketedPaste(s string) string {
	return "\x1b[200~" + s + "\x1b[201~"
}
