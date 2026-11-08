package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/clibin"
	"larvyn/internal/flock"
	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// The local API's methods (see internal/api): what an agent running in
// Larvyn can see and do, as `larvyn <command>` or MCP tools. Each one goes
// through the same service the UI uses.
func init() {
	api.Register("version", "Larvyn's version", func() string { return version })
	api.Register("context", "Where the caller runs: project, worktree, branch, local URL and the dev stack", apiContext)
	api.Register("terminal.list", "List the terminals of the caller's project", apiTerminalList,
		"all?: every project's terminals, not just the caller's")
	api.Register("terminal.read", "Print a terminal's last lines as plain text, or what its screen shows (the default for agents)", apiTerminalRead,
		"id: the terminal (terminal list shows them; a unique start of the id is enough)",
		"lines?: how many lines, 100 by default",
		"all?: the whole scrollback",
		"screen?: what the screen shows now, as a terminal draws it")
	api.Register("terminal.write", "Type text into a terminal", apiTerminalWrite,
		"id: the terminal",
		"text: what to type",
		"enter?: press enter after it")
	api.Register("terminal.open", "Open a terminal in the caller's project, running a command or a shell", apiTerminalOpen,
		"command?: the command line to run; a shell when left out",
		"cwd?: the folder to run in; the caller's own by default",
		"title?: the terminal's name")
	api.Register("terminal.close", "Close a terminal the caller opened", apiTerminalClose,
		"id: the terminal")
	api.MarkReadOnly("version", "context", "terminal.list", "terminal.read")
}

// startAPI writes the CLI where terminals find it and opens the socket.
// Neither is fatal: the app works without them.
func startAPI() func() {
	if dir, err := clibin.Install(); err == nil {
		terminal.BinDir = dir
	} else if !errors.Is(err, clibin.ErrNotBuilt) {
		store.AppendLog("warn", "larvyn CLI: "+err.Error())
	}
	srv, err := api.Listen()
	if err != nil {
		store.AppendLog("warn", "local API: "+err.Error())
		return func() {}
	}
	return func() { _ = srv.Close() }
}

// Context is what `larvyn context` prints.
type Context struct {
	Project   string `json:"project"`
	ProjectID string `json:"projectId"`
	// Path is the project's main folder; Dir is where the caller works, a
	// worktree of it or the folder itself.
	Path     string `json:"path"`
	Dir      string `json:"dir"`
	Worktree bool   `json:"worktree"`
	Branch   string `json:"branch,omitempty"`
	URL      string `json:"url,omitempty"`
	// Terminal and Agent are the caller's own terminal and, in an agent's,
	// which agent it is.
	Terminal string      `json:"terminal,omitempty"`
	Agent    string      `json:"agent,omitempty"`
	Stack    []StackItem `json:"stack"`
}

// StackItem is a dev stack service in Context.
type StackItem struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Running  bool   `json:"running"`
	Terminal string `json:"terminal"`
}

// callerProject finds the project a call comes from: the project of the
// caller's terminal, or the one whose folder or worktree holds its working
// directory.
func callerProject(c api.Caller) (p store.Project, dir string, self *terminal.SessionInfo, err error) {
	if c.Session != "" {
		for _, s := range terminal.Sessions.List() {
			if s.ID == c.Session {
				self = &s
				break
			}
		}
	}
	dir = c.Cwd
	if self != nil && self.ProjectID != "" {
		if p, ok := store.FindProject(self.ProjectID); ok {
			return p, proc.FirstNonEmpty(self.Cwd, p.Path), self, nil
		}
	}
	if dir == "" {
		return store.Project{}, "", self, errors.New("not in a Larvyn project: run it from a project's folder or one of its terminals")
	}
	// The git repository's main folder, which is the project's folder for a
	// worktree too.
	main := dir
	if common, err := proc.Git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
		main = filepath.Dir(strings.TrimSpace(common))
	}
	for _, p := range store.Projects() {
		if within(dir, p.Path) || within(main, p.Path) {
			return p, dir, self, nil
		}
	}
	return store.Project{}, "", self, fmt.Errorf("%s is not in a project Larvyn knows: add it in Larvyn first", dir)
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func apiContext(c api.Caller) (Context, error) {
	p, dir, self, err := callerProject(c)
	if err != nil {
		return Context{}, err
	}
	ctx := Context{Project: p.Name, ProjectID: p.ID, Path: p.Path, Dir: dir, Stack: []StackItem{}}
	if top, err := proc.Git(dir, "rev-parse", "--show-toplevel"); err == nil {
		ctx.Dir = filepath.FromSlash(strings.TrimSpace(top))
	}
	ctx.Worktree = !samePath(ctx.Dir, p.Path)
	if b, err := proc.Git(ctx.Dir, "branch", "--show-current"); err == nil {
		ctx.Branch = strings.TrimSpace(b)
	}
	if s, ok := flock.SiteFor(ctx.Dir); ok && s.Problem == "" {
		ctx.URL = s.URL
	}
	if self != nil {
		ctx.Terminal = self.ID
		ctx.Agent = self.Meta["agentName"]
	}
	for _, s := range terminal.Sessions.List() {
		if s.ProjectID == p.ID && s.Kind == "service" {
			ctx.Stack = append(ctx.Stack, StackItem{Key: s.Key, Title: s.Title, Running: s.Running, Terminal: s.ID})
		}
	}
	return ctx, nil
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// TermRow is a terminal in `larvyn terminal list`.
type TermRow struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Running bool   `json:"running"`
	Agent   string `json:"agent,omitempty"`
	Cwd     string `json:"cwd"`
	Project string `json:"project,omitempty"`
}

func termRow(s terminal.SessionInfo) TermRow {
	r := TermRow{ID: s.ID, Kind: s.Kind, Title: s.Title, Running: s.Running, Agent: s.Meta["agentName"], Cwd: s.Cwd}
	if p, ok := store.FindProject(s.ProjectID); ok {
		r.Project = p.Name
	}
	return r
}

func apiTerminalList(c api.Caller, all bool) ([]TermRow, error) {
	var projectID string
	if !all {
		p, _, _, err := callerProject(c)
		if err != nil {
			return nil, fmt.Errorf("%v (or pass --all)", err)
		}
		projectID = p.ID
	}
	out := []TermRow{}
	for _, s := range terminal.Sessions.List() {
		if all || s.ProjectID == projectID {
			out = append(out, termRow(s))
		}
	}
	return out, nil
}

// findTerminal takes a terminal's id or a unique start of it.
func findTerminal(id string) (terminal.SessionInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return terminal.SessionInfo{}, errors.New("no terminal id")
	}
	var found []terminal.SessionInfo
	for _, s := range terminal.Sessions.List() {
		if s.ID == id {
			return s, nil
		}
		if strings.HasPrefix(s.ID, id) {
			found = append(found, s)
		}
	}
	switch len(found) {
	case 0:
		return terminal.SessionInfo{}, fmt.Errorf("no terminal %q (larvyn terminal list shows them)", id)
	case 1:
		return found[0], nil
	}
	return terminal.SessionInfo{}, fmt.Errorf("%q matches %d terminals: give more of the id", id, len(found))
}

func apiTerminalRead(id string, lines int, all, screen bool) (string, error) {
	s, err := findTerminal(id)
	if err != nil {
		return "", err
	}
	// An agent's TUI draws with the cursor: its raw output is scrambled.
	if screen || (s.Kind == "agent" && lines == 0 && !all) {
		out, err := terminal.Screen(s.ID)
		if err != nil || lines <= 0 {
			return out, err
		}
		return lastLines(out, lines), nil
	}
	out, err := terminal.Sessions.Output(s.ID)
	if err != nil {
		return "", err
	}
	switch {
	case all:
		lines = -1
	case lines <= 0:
		lines = 100
	}
	return lastLines(out, lines), nil
}

// lastLines is the last n lines of terminal output, with each line as it
// ended up on screen: what a carriage return overwrote (progress bars,
// spinners) is dropped. n < 0 keeps every line.
func lastLines(out string, n int) string {
	out = strings.ReplaceAll(out, "\r\n", "\n")
	all := strings.Split(out, "\n")
	for i, l := range all {
		if j := strings.LastIndex(strings.TrimRight(l, "\r"), "\r"); j >= 0 {
			l = l[j+1:]
		}
		all[i] = strings.TrimRight(l, " \r")
	}
	for len(all) > 0 && all[len(all)-1] == "" {
		all = all[:len(all)-1]
	}
	if n >= 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return strings.Join(all, "\n")
}

func apiTerminalWrite(id, text string, enter bool) error {
	s, err := findTerminal(id)
	if err != nil {
		return err
	}
	if !s.Running {
		return fmt.Errorf("terminal %s is not running", s.ID)
	}
	if s.Setup {
		return errors.New("the worktree is still being set up: what is typed would go to its setup step")
	}
	if enter {
		text += "\r"
	}
	if s.Kind == "agent" {
		// An answer to its question: agents wait gives it time to go on.
		prompted.Store(s.ID, time.Now())
	}
	return terminal.Sessions.Write(s.ID, text)
}

func apiTerminalOpen(c api.Caller, command, cwd, title string) (TermRow, error) {
	p, dir, _, err := callerProject(c)
	if err != nil {
		return TermRow{}, err
	}
	if cwd == "" {
		cwd = dir
	} else if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(proc.FirstNonEmpty(c.Cwd, dir), cwd)
	}
	kind := "shell"
	if command != "" {
		kind = "run"
	}
	// openedBy lets the caller close it again, and nothing else.
	s, err := terminal.Sessions.Start(terminal.SessionSpec{ProjectID: p.ID, Kind: kind, Title: title, Cwd: cwd, Command: command,
		Meta: map[string]string{"openedBy": proc.FirstNonEmpty(c.Session, "cli")}})
	if err != nil {
		return TermRow{}, err
	}
	return termRow(s), nil
}

func apiTerminalClose(c api.Caller, id string) error {
	s, err := findTerminal(id)
	if err != nil {
		return err
	}
	if by := s.Meta["openedBy"]; by == "" || by != proc.FirstNonEmpty(c.Session, "cli") {
		return errors.New("only terminals opened with larvyn terminal open, from the same terminal, can be closed")
	}
	return terminal.Sessions.Close(s.ID)
}
