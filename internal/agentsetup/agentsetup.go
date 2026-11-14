// Package agentsetup installs what tells coding agents about Larvyn: a
// skill that teaches the larvyn command, a session-start hook that tells an
// agent it runs in Larvyn and where (#107), and the larvyn MCP server.
// Claude Code gets all three in its config folder (CLAUDE_CONFIG_DIR or
// ~/.claude, and .claude.json for MCP); Codex gets the hook and the server
// in CODEX_HOME (or ~/.codex), and the skill goes to ~/.agents/skills, which
// Codex and other agents read. Cursor, Gemini CLI, Windsurf and OpenCode get
// the MCP server.
//
// Only the agents whose config folder exists are set up. Running it again
// changes nothing, a dry run lists the changes without making them, and
// Remove takes out only what carries Larvyn's mark. JSON settings keep the
// user's key order, and get a backup before they are changed.
package agentsetup

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed SKILL.md
var skill []byte

// mark is in the skill Larvyn writes; Remove deletes only a skill with it.
const mark = "Installed by Larvyn"

// hookArgs end the hook's command line: `<cli> agents hook`.
const hookArgs = " agents hook"

// Options say where things go. Defaults reads them from the environment.
type Options struct {
	ClaudeDir string // CLAUDE_CONFIG_DIR, ~/.claude
	CodexDir  string // CODEX_HOME, ~/.codex
	AgentsDir string // ~/.agents
	// ClaudeJSON is where Claude Code keeps its user MCP servers:
	// ~/.claude.json, or .claude.json in CLAUDE_CONFIG_DIR.
	ClaudeJSON  string
	CursorDir   string // ~/.cursor
	GeminiDir   string // ~/.gemini
	WindsurfDir string // ~/.codeium/windsurf
	OpenCodeDir string // $XDG_CONFIG_HOME/opencode, ~/.config/opencode
	// OpenCodeMajor is OpenCode's major version, which says where its MCP
	// servers go; 0 is unknown, taken as the newest.
	OpenCodeMajor int
	// CLI is the larvyn command the hooks and the MCP server run.
	CLI string
}

// Defaults are the folders of this user's agents.
func Defaults(cli string) (Options, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Options{}, err
	}
	o := Options{
		ClaudeDir:   filepath.Join(home, ".claude"),
		CodexDir:    filepath.Join(home, ".codex"),
		AgentsDir:   filepath.Join(home, ".agents"),
		ClaudeJSON:  filepath.Join(home, ".claude.json"),
		CursorDir:   filepath.Join(home, ".cursor"),
		GeminiDir:   filepath.Join(home, ".gemini"),
		WindsurfDir: filepath.Join(home, ".codeium", "windsurf"),
		OpenCodeDir: filepath.Join(home, ".config", "opencode"),
		CLI:         cli,
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		o.ClaudeDir = d
		o.ClaudeJSON = filepath.Join(d, ".claude.json")
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		o.OpenCodeDir = filepath.Join(d, "opencode")
	}
	if exists(o.OpenCodeDir) {
		o.OpenCodeMajor = openCodeMajor()
	}
	if d := os.Getenv("CODEX_HOME"); d != "" {
		o.CodexDir = d
	}
	return o, nil
}

// Change is one file the setup writes or removes.
type Change struct {
	Path string `json:"path"`
	// What it is: "Claude Code skill", "Codex session hook"…
	What string `json:"what"`
	// Action is create, update, remove, unchanged or skipped (left for the
	// user, as Note says).
	Action string `json:"action"`
	// Note says what else the user needs to know, like Codex asking to
	// trust a new hook.
	Note string `json:"note,omitempty"`
}

// exists is false only for a path that isn't there: a folder that can't be
// read still counts, so the read or write after it reports why.
func exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// Skill is the larvyn skill, for an agent that keeps skills somewhere
// Install doesn't write (larvyn agents skill prints it).
func Skill() []byte { return skill }

// Install writes the skills, hooks and MCP servers, or with dryRun only says
// what it would change.
func Install(o Options, dryRun bool) ([]Change, error) {
	if o.CLI == "" {
		return nil, errors.New("no larvyn command to run from the hooks and the MCP server")
	}
	var out []Change
	var errs []error
	add := func(c Change, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Path, err))
			return
		}
		out = append(out, c)
	}
	if exists(o.ClaudeDir) {
		add(writeSkill(filepath.Join(o.ClaudeDir, "skills", "larvyn", "SKILL.md"), "Claude Code skill", dryRun))
		add(editHooks(filepath.Join(o.ClaudeDir, "settings.json"), "Claude Code session hook", o.CLI, true, dryRun))
	}
	if exists(o.ClaudeDir) && o.ClaudeJSON != "" {
		add(editMCP(claudeMCP(o), o.CLI, true, dryRun))
	}
	clients := jsonClients(o)
	others := exists(o.CodexDir) || exists(o.AgentsDir)
	for _, cl := range clients {
		others = others || exists(cl.dir)
	}
	if others {
		add(writeSkill(filepath.Join(o.AgentsDir, "skills", "larvyn", "SKILL.md"), "Skill for Codex and other agents", dryRun))
	}
	if exists(o.CodexDir) {
		c, err := editHooks(filepath.Join(o.CodexDir, "hooks.json"), "Codex session hook", o.CLI, true, dryRun)
		if c.Action == "create" || c.Action == "update" {
			c.Note = "Codex asks you to review new hooks once: accept it with /hooks"
		}
		add(c, err)
		add(editCodexMCP(filepath.Join(o.CodexDir, "config.toml"), o.CLI, true, dryRun))
	}
	for _, cl := range clients {
		if !exists(cl.dir) {
			continue
		}
		if c, ok := openCodeJSONC(o); ok && cl.dir == o.OpenCodeDir {
			add(c, nil)
			continue
		}
		add(editMCP(cl, o.CLI, true, dryRun))
	}
	if len(out) == 0 && len(errs) == 0 {
		return nil, errors.New("no agent to set up: none of Claude Code, Codex, Cursor, Gemini CLI, Windsurf or OpenCode is installed (and there is no " + o.AgentsDir + " folder)")
	}
	return out, errors.Join(errs...)
}

// Remove takes out what Install wrote, or with dryRun says what it would.
func Remove(o Options, dryRun bool) ([]Change, error) {
	var out []Change
	var errs []error
	add := func(c Change, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Path, err))
		} else if c.Action != "unchanged" {
			out = append(out, c)
		}
	}
	add(removeSkill(filepath.Join(o.ClaudeDir, "skills", "larvyn", "SKILL.md"), "Claude Code skill", dryRun))
	add(editHooks(filepath.Join(o.ClaudeDir, "settings.json"), "Claude Code session hook", o.CLI, false, dryRun))
	add(removeSkill(filepath.Join(o.AgentsDir, "skills", "larvyn", "SKILL.md"), "Skill for Codex and other agents", dryRun))
	add(editHooks(filepath.Join(o.CodexDir, "hooks.json"), "Codex session hook", o.CLI, false, dryRun))
	if o.CodexDir != "" {
		add(editCodexMCP(filepath.Join(o.CodexDir, "config.toml"), o.CLI, false, dryRun))
	}
	if o.ClaudeJSON != "" {
		add(editMCP(claudeMCP(o), o.CLI, false, dryRun))
	}
	for _, cl := range jsonClients(o) {
		add(editMCP(cl, o.CLI, false, dryRun))
	}
	// Either OpenCode layout, in case OpenCode was updated since.
	if o.OpenCodeDir != "" {
		add(editMCP(openCodeMCP(o, o.OpenCodeMajor != 1), o.CLI, false, dryRun))
	}
	return out, errors.Join(errs...)
}

func writeSkill(path, what string, dryRun bool) (Change, error) {
	c := Change{Path: path, What: what, Action: "create"}
	if old, err := os.ReadFile(path); err == nil {
		if bytes.Equal(old, skill) {
			c.Action = "unchanged"
			return c, nil
		}
		if !bytes.Contains(old, []byte(mark)) {
			return c, errors.New("a larvyn skill that Larvyn didn't write is already there; it was left alone")
		}
		c.Action = "update"
	}
	if dryRun {
		return c, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return c, err
	}
	return c, os.WriteFile(path, skill, 0o644)
}

func removeSkill(path, what string, dryRun bool) (Change, error) {
	c := Change{Path: path, What: what, Action: "unchanged"}
	old, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(old, []byte(mark)) {
		return c, nil
	}
	c.Action = "remove"
	if dryRun {
		return c, nil
	}
	if err := os.Remove(path); err != nil {
		return c, err
	}
	// The skill's folder, when nothing else is in it.
	_ = os.Remove(filepath.Dir(path))
	return c, nil
}

// hookCommand is the hook's command line. A path with spaces is quoted.
func hookCommand(cli string) string {
	cli = filepath.ToSlash(cli)
	if strings.ContainsAny(cli, " \t") {
		cli = `"` + cli + `"`
	}
	return cli + hookArgs
}

// ours tells Larvyn's hook from the rest: any `… larvyn… agents hook`, so a
// hook from another state folder (LARVYN_HOME) or an older install is
// replaced, not added a second time.
func ours(command string) bool {
	return strings.HasSuffix(command, hookArgs) && strings.Contains(strings.ToLower(command), "larvyn")
}

// editHooks adds (add) or removes Larvyn's SessionStart hook in an agent's
// JSON settings, which Claude Code and Codex shape the same way:
//
//	{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "…"}]}]}}
func editHooks(path, what, cli string, add, dryRun bool) (Change, error) {
	c := Change{Path: path, What: what, Action: "unchanged"}
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return c, err
	}
	if err != nil && !add {
		return c, nil
	}
	root, err := parse(old)
	if err != nil || root.obj == nil {
		return c, fmt.Errorf("not a JSON object (%v): it was left alone", err)
	}
	hooks := root.obj.get("hooks")
	if hooks == nil {
		if !add {
			return c, nil
		}
		hooks = newObject()
		root.obj.set("hooks", hooks)
	}
	if hooks.obj == nil {
		return c, errors.New(`"hooks" is not an object: it was left alone`)
	}
	start := hooks.obj.get("SessionStart")
	if start == nil {
		start = &node{arr: []*node{}}
	}
	if start.arr == nil {
		return c, errors.New(`"hooks.SessionStart" is not a list: it was left alone`)
	}

	// Take Larvyn's hooks out, keeping every other one where it was.
	want := hookCommand(cli)
	found, current := false, false
	var groups []*node
	for _, g := range start.arr {
		list := g.obj.getList("hooks")
		if list == nil {
			groups = append(groups, g)
			continue
		}
		keep := []*node{}
		for _, h := range list {
			cmd := h.obj.getString("command")
			if h.obj != nil && ours(cmd) {
				found = true
				current = current || cmd == want
				continue
			}
			keep = append(keep, h)
		}
		if len(keep) == 0 && len(list) > 0 {
			continue
		}
		g.obj.set("hooks", &node{arr: keep})
		groups = append(groups, g)
	}
	if add && found && current && countOurs(start) == 1 {
		return c, nil
	}
	if !add && !found {
		return c, nil
	}
	if add {
		hook := newObject()
		hook.obj.set("type", value("command"))
		hook.obj.set("command", value(want))
		hook.obj.set("timeout", value(10))
		entry := newObject()
		if strings.HasSuffix(path, "settings.json") {
			// Claude Code's groups have a matcher: startup, resume, clear,
			// compact. "" is every one of them.
			entry.obj.set("matcher", value(""))
		}
		entry.obj.set("hooks", &node{arr: []*node{hook}})
		groups = append(groups, entry)
		c.Action = "create"
		if found {
			c.Action = "update"
		}
	} else {
		c.Action = "remove"
	}
	if len(groups) == 0 {
		hooks.obj.del("SessionStart")
	} else {
		hooks.obj.set("SessionStart", &node{arr: groups})
	}
	if len(hooks.obj.keys) == 0 {
		root.obj.del("hooks")
	}
	if dryRun {
		return c, nil
	}
	return c, save(path, old, format(root))
}

func countOurs(start *node) int {
	n := 0
	for _, g := range start.arr {
		for _, h := range g.obj.getList("hooks") {
			if ours(h.obj.getString("command")) {
				n++
			}
		}
	}
	return n
}

// save writes a settings file, after a backup of what was there.
func save(path string, old, data []byte) error {
	// A settings file kept in a dotfiles repo is a symlink: write the file
	// it points to, so the link stays.
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
		if err := os.WriteFile(path+".larvyn-backup", old, mode); err != nil {
			return err
		}
	}
	tmp := path + ".larvyn-tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (o *object) getList(k string) []*node {
	if o == nil {
		return nil
	}
	if n := o.vals[k]; n != nil {
		return n.arr
	}
	return nil
}

func (o *object) getString(k string) string {
	if o == nil {
		return ""
	}
	return o.vals[k].str()
}
