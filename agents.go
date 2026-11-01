package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
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

type AgentService struct{}

// Kinds lists the known agents and whether each is on PATH.
func (a *AgentService) Kinds() []AgentKind {
	out := make([]AgentKind, len(agentKinds))
	for i, k := range agentKinds {
		k.Path = which(k.Binary)
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
func (a *AgentService) Launch(req LaunchRequest) (SessionInfo, error) {
	p, ok := store.Project(req.ProjectID)
	if !ok {
		return SessionInfo{}, errors.New("project not found")
	}
	var kind *AgentKind
	for _, k := range a.Kinds() {
		if k.ID == req.Agent {
			k := k
			kind = &k
		}
	}
	if kind == nil {
		return SessionInfo{}, errors.New("unknown agent " + req.Agent)
	}
	if !kind.Installed {
		return SessionInfo{}, errors.New(kind.Name + " is not installed: " + kind.Install)
	}
	title := req.Title
	if title == "" {
		title = firstLine(req.Prompt, 48)
	}
	if title == "" {
		title = kind.Name
	}
	cwd := firstNonEmpty(req.Cwd, p.Path)
	meta := map[string]string{"agent": kind.ID, "agentName": kind.Name, "color": kind.Color, "prompt": req.Prompt}
	if req.Worktree {
		wt, err := (&GitService{}).CreateWorktree(WorktreeRequest{ProjectID: p.ID, Name: kind.ID + "-" + title, Base: req.Base, CopyEnv: true})
		if err != nil {
			return SessionInfo{}, err
		}
		cwd = wt.Path
		meta["worktree"] = wt.Path
		meta["branch"] = wt.Branch
		if req.InstallDeps {
			a.installDeps(p, cwd)
		}
	}
	argv := []string{kind.Path}
	if f := yoloFlags[kind.ID]; len(f) > 0 && store.Settings().AgentPermissions != "manual" {
		argv = append(argv, f...)
		meta["yolo"] = "1"
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
	sess, err := terminals.Start(SessionSpec{ProjectID: p.ID, Kind: "agent", Key: kind.ID, Title: title, Cwd: cwd, Argv: argv, Meta: meta})
	if err != nil {
		return sess, err
	}
	if req.Prompt != "" && !kind.PromptAsArg {
		go func(id, prompt string) {
			// Give the TUI time to draw its input box before typing into it.
			time.Sleep(2500 * time.Millisecond)
			_ = terminals.Write(id, bracketedPaste(prompt))
			time.Sleep(150 * time.Millisecond)
			_ = terminals.Write(id, "\r")
		}(sess.ID, req.Prompt)
	}
	return sess, nil
}

func (a *AgentService) installDeps(p Project, dir string) {
	if _, err := os.Stat(filepath.Join(dir, "composer.json")); err == nil && composerArgv() != nil {
		_, _ = terminals.Start(SessionSpec{ProjectID: p.ID, Kind: "run", Key: "deps", Title: "composer install · " + filepath.Base(dir),
			Cwd: dir, Argv: []string{"composer", "install", "--no-interaction"}})
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		pm := packageManager(dir)
		_, _ = terminals.Start(SessionSpec{ProjectID: p.ID, Kind: "run", Key: "deps", Title: pm + " install · " + filepath.Base(dir),
			Cwd: dir, Argv: []string{pm, "install"}})
	}
}

// Fanout starts the same prompt in several agents, each in its own worktree.
func (a *AgentService) Fanout(req LaunchRequest, agents []string) ([]SessionInfo, error) {
	var out []SessionInfo
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
	if err := terminals.Write(sessionID, bracketedPaste(text)); err != nil {
		return err
	}
	time.Sleep(120 * time.Millisecond)
	return terminals.Write(sessionID, "\r")
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
