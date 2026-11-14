package agentsetup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"larvyn/internal/proc"
)

// The MCP server is `<cli> mcp`, registered under this name. The CLI's full
// path goes in, so an agent started outside Larvyn's terminals, where
// larvyn is not on the PATH, finds it too.
const mcpName = "larvyn"

// mcpClient is an agent that keeps its MCP servers in a JSON file, in an
// object found under keys, by name.
type mcpClient struct {
	what string
	dir  string // the agent's config folder: only an agent that has one is set up
	path string
	keys []string
	// entry is Larvyn's server as this agent writes it.
	entry func(cli string) *node
}

func stdioEntry(withType bool) func(string) *node {
	return func(cli string) *node {
		n := newObject()
		if withType {
			n.obj.set("type", value("stdio"))
		}
		n.obj.set("command", value(cli))
		n.obj.set("args", value([]string{"mcp"}))
		return n
	}
}

// OpenCode 2 keeps its servers in mcp.servers, OpenCode 1 in mcp, with
// an enabled flag.
func openCodeEntry(v1 bool) func(string) *node {
	return func(cli string) *node {
		n := newObject()
		n.obj.set("type", value("local"))
		n.obj.set("command", value([]string{cli, "mcp"}))
		if v1 {
			n.obj.set("enabled", value(true))
		}
		return n
	}
}

func openCodeMCP(o Options, v1 bool) mcpClient {
	keys := []string{"mcp", "servers"}
	if v1 {
		keys = keys[:1]
	}
	return mcpClient{"OpenCode MCP server", o.OpenCodeDir, filepath.Join(o.OpenCodeDir, "opencode.json"), keys, openCodeEntry(v1)}
}

// claudeMCP is Claude Code's user MCP servers, in .claude.json.
func claudeMCP(o Options) mcpClient {
	return mcpClient{"Claude Code MCP server", o.ClaudeDir, o.ClaudeJSON, []string{"mcpServers"}, stdioEntry(true)}
}

// jsonClients are the other agents whose MCP file is JSON. Codex's is TOML
// (editCodexMCP).
func jsonClients(o Options) []mcpClient {
	var cs []mcpClient
	if o.CursorDir != "" {
		cs = append(cs, mcpClient{"Cursor MCP server", o.CursorDir, filepath.Join(o.CursorDir, "mcp.json"), []string{"mcpServers"}, stdioEntry(false)})
	}
	if o.GeminiDir != "" {
		cs = append(cs, mcpClient{"Gemini CLI MCP server", o.GeminiDir, filepath.Join(o.GeminiDir, "settings.json"), []string{"mcpServers"}, stdioEntry(false)})
	}
	if o.WindsurfDir != "" {
		cs = append(cs, mcpClient{"Windsurf MCP server", o.WindsurfDir, filepath.Join(o.WindsurfDir, "mcp_config.json"), []string{"mcpServers"}, stdioEntry(false)})
	}
	if o.OpenCodeDir != "" {
		cs = append(cs, openCodeMCP(o, o.OpenCodeMajor == 1))
	}
	return cs
}

// oursMCP tells Larvyn's server from another one named larvyn: its command
// is a larvyn binary run with `mcp`, as a command and args or, in OpenCode,
// as one list.
func oursMCP(n *node) bool {
	if n == nil || n.obj == nil {
		return false
	}
	cmd := n.obj.get("command")
	argv := []*node{cmd}
	if cmd != nil && cmd.arr != nil {
		argv = cmd.arr
	} else {
		argv = append(argv, n.obj.getList("args")...)
	}
	return len(argv) == 2 && strings.Contains(strings.ToLower(argv[0].str()), "larvyn") && argv[1].str() == "mcp"
}

// editMCP adds (add) or removes Larvyn's server in an agent's JSON file.
func editMCP(cl mcpClient, cli string, add, dryRun bool) (Change, error) {
	c := Change{Path: cl.path, What: cl.what, Action: "unchanged"}
	old, err := os.ReadFile(cl.path)
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
	servers := root
	for i, k := range cl.keys {
		next := servers.obj.get(k)
		if next == nil {
			if !add {
				return c, nil
			}
			next = newObject()
			servers.obj.set(k, next)
		}
		if next.obj == nil {
			return c, fmt.Errorf("%q is not an object: it was left alone", strings.Join(cl.keys[:i+1], "."))
		}
		servers = next
	}
	cur := servers.obj.get(mcpName)
	if cur != nil && !oursMCP(cur) {
		if add {
			return c, errors.New(`an MCP server named "larvyn" that Larvyn didn't add is already there; it was left alone`)
		}
		return c, nil
	}
	if add {
		want := cl.entry(cli)
		if cur != nil && bytes.Equal(format(cur), format(want)) {
			return c, nil
		}
		c.Action = "create"
		if cur != nil {
			c.Action = "update"
		}
		servers.obj.set(mcpName, want)
	} else {
		if cur == nil {
			return c, nil
		}
		c.Action = "remove"
		servers.obj.del(mcpName)
	}
	if dryRun {
		return c, nil
	}
	return c, save(cl.path, old, format(root))
}

// openCodeJSONC is the change for an OpenCode config kept as opencode.jsonc:
// its comments would be lost, so it is left for the user to edit.
func openCodeJSONC(o Options) (Change, bool) {
	if o.OpenCodeDir == "" || exists(filepath.Join(o.OpenCodeDir, "opencode.json")) {
		return Change{}, false
	}
	p := filepath.Join(o.OpenCodeDir, "opencode.jsonc")
	if _, err := os.Stat(p); err != nil {
		return Change{}, false
	}
	return Change{Path: p, What: "OpenCode MCP server", Action: "skipped",
		Note: "Larvyn doesn't edit a file with comments: add the server by hand (larvyn.dev/docs/mcp)"}, true
}

// Codex keeps its MCP servers in config.toml, as [mcp_servers.<name>]
// tables. Larvyn edits only its own table, as text, so the rest of the file
// stays exactly as written.

var (
	codexHeader = regexp.MustCompile(`^\s*\[`)
	codexOurs   = regexp.MustCompile(`^\s*\[\s*mcp_servers\s*\.\s*"?larvyn"?\s*[\].]`)
	// A larvyn server written inline, as `larvyn = {…}` in [mcp_servers] or
	// `mcp_servers.larvyn = {…}`: adding a table would make the file invalid.
	codexInline = regexp.MustCompile(`(?m)^\s*(mcp_servers\s*\.\s*)?"?larvyn"?\s*=`)
	codexCmd    = regexp.MustCompile(`^\s*command\s*=.*larvyn`)
)

func tomlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func codexTable(cli string) []string {
	return []string{"[mcp_servers." + mcpName + "]", "command = " + tomlString(cli), `args = ["mcp"]`}
}

func editCodexMCP(path, cli string, add, dryRun bool) (Change, error) {
	c := Change{Path: path, What: "Codex MCP server", Action: "unchanged"}
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return c, err
	}
	if err != nil && !add {
		return c, nil
	}
	text := string(old)
	crlf := strings.Contains(text, "\r\n")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}

	// Larvyn's tables: [mcp_servers.larvyn] and any [mcp_servers.larvyn.env].
	var keep, block []string
	first, found, ours := -1, false, false
	in := false
	for _, l := range lines {
		if codexHeader.MatchString(l) {
			in = codexOurs.MatchString(l)
			if in && !found {
				found, first = true, len(keep)
			}
		}
		if !in {
			keep = append(keep, l)
			continue
		}
		if codexCmd.MatchString(l) {
			ours = true
		}
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			block = append(block, t)
		}
	}
	inline := !found && codexInline.MatchString(text)
	if (found && !ours) || inline {
		if add {
			return c, errors.New(`an MCP server named "larvyn" that Larvyn didn't add is already there; it was left alone`)
		}
		return c, nil
	}

	want := codexTable(cli)
	if add {
		if found && strings.Join(block, "\n") == strings.Join(want, "\n") {
			return c, nil
		}
		c.Action = "create"
		if found {
			c.Action = "update"
		}
		if !found {
			first = len(keep)
		}
		table := append([]string{}, want...)
		if first < len(keep) {
			table = append(table, "")
		}
		if first > 0 && strings.TrimSpace(keep[first-1]) != "" {
			table = append([]string{""}, table...)
		}
		keep = append(keep[:first], append(table, keep[first:]...)...)
	} else {
		if !found {
			return c, nil
		}
		c.Action = "remove"
	}
	for len(keep) > 0 && strings.TrimSpace(keep[len(keep)-1]) == "" {
		keep = keep[:len(keep)-1]
	}
	out := strings.Join(keep, "\n")
	if out != "" {
		out += "\n"
	}
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	if dryRun {
		return c, nil
	}
	return c, save(path, old, []byte(out))
}

var semverMajor = regexp.MustCompile(`(\d+)\.\d+`)

// openCodeMajor asks the opencode command for its major version, or 0.
func openCodeMajor() int {
	bin := proc.Which("opencode")
	if bin == "" {
		return 0
	}
	res, err := proc.Run("", 10*time.Second, nil, bin, "--version")
	if err != nil || res.Code != 0 {
		return 0
	}
	m := semverMajor.FindStringSubmatch(res.Stdout)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
