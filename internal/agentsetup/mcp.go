package agentsetup

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

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
	return len(argv) == 2 && larvynBinary(argv[0].str()) && argv[1].str() == "mcp"
}

// larvynBinary tells a path to a larvyn binary, in any folder, on any OS.
func larvynBinary(path string) bool {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		path = path[i+1:]
	}
	path = strings.ToLower(path)
	return path == "larvyn" || path == "larvyn.exe"
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
// stays exactly as written. A TOML parser says what the file holds, before
// and after: whether a larvyn server is there and Larvyn's, and that the
// edit left a valid file with nothing else changed. TOML has too many ways
// to write the same table (quoted keys, inline tables, look-alike lines in
// multiline strings) for the text alone to be trusted; any edit the parser
// doesn't confirm is not saved.

var (
	codexHeader = regexp.MustCompile(`^\s*\[`)
	codexOurs   = regexp.MustCompile(`^\s*\[\s*mcp_servers\s*\.\s*(?:larvyn|"larvyn"|'larvyn')\s*[\].]`)
	// mcp_servers or a larvyn server written inline (`mcp_servers = {…}`,
	// `larvyn = {…}`): no table can be added to or replace them, and the
	// parser doesn't catch every such redefinition.
	codexInline = regexp.MustCompile(`(?m)^\s*(?:(?:mcp_servers|"mcp_servers"|'mcp_servers')\s*=|(?:(?:mcp_servers|"mcp_servers"|'mcp_servers')\s*\.\s*)?(?:larvyn|"larvyn"|'larvyn')\s*=)`)
)

var errCodexUnsafe = errors.New(`config.toml writes its MCP servers in a way Larvyn can't edit safely: it was left alone; add the server by hand (larvyn.dev/docs/mcp)`)

func tomlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func codexTable(cli string) []string {
	return []string{"[mcp_servers." + mcpName + "]", "command = " + tomlString(cli), `args = ["mcp"]`}
}

// codexServer splits a parsed config.toml into Larvyn's server, if any, and
// everything else.
func codexServer(text string) (server any, rest map[string]any, err error) {
	rest = map[string]any{}
	if _, err := toml.Decode(text, &rest); err != nil {
		return nil, nil, err
	}
	servers, ok := rest["mcp_servers"].(map[string]any)
	if !ok {
		return nil, rest, nil
	}
	server = servers[mcpName]
	others := maps.Clone(servers)
	delete(others, mcpName)
	if len(others) == 0 {
		delete(rest, "mcp_servers")
	} else {
		rest["mcp_servers"] = others
	}
	return server, rest, nil
}

// codexOursServer is Larvyn's server: a larvyn binary run with just `mcp`.
func codexOursServer(s any) bool {
	m, ok := s.(map[string]any)
	if !ok {
		return false
	}
	cmd, _ := m["command"].(string)
	args, _ := m["args"].([]any)
	return larvynBinary(cmd) && len(args) == 1 && args[0] == "mcp"
}

func codexWant(cli string) map[string]any {
	return map[string]any{"command": cli, "args": []any{"mcp"}}
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
	cur, rest, err := codexServer(text)
	if err != nil {
		if add {
			return c, fmt.Errorf("not valid TOML (%v): it was left alone", err)
		}
		return c, nil
	}
	if cur != nil && !codexOursServer(cur) {
		if add {
			return c, errors.New(`an MCP server named "larvyn" that Larvyn didn't add is already there; it was left alone`)
		}
		return c, nil
	}
	if add && cur != nil && reflect.DeepEqual(cur, codexWant(cli)) {
		return c, nil
	}
	if !add && cur == nil {
		return c, nil
	}
	if codexInline.MatchString(text) {
		return c, errCodexUnsafe
	}

	crlf := strings.Contains(text, "\r\n")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	// Drop Larvyn's tables, [mcp_servers.larvyn] and any [mcp_servers.larvyn.env].
	var keep []string
	first, in := -1, false
	for _, l := range lines {
		if codexHeader.MatchString(l) {
			in = codexOurs.MatchString(l)
			if in && first < 0 {
				first = len(keep)
			}
		}
		if !in {
			keep = append(keep, l)
		}
	}
	if add {
		c.Action = "create"
		if cur != nil {
			c.Action = "update"
		}
		if first < 0 {
			first = len(keep)
		}
		table := codexTable(cli)
		if first < len(keep) {
			table = append(table, "")
		}
		if first > 0 && strings.TrimSpace(keep[first-1]) != "" {
			table = append([]string{""}, table...)
		}
		keep = append(keep[:first], append(table, keep[first:]...)...)
	} else {
		c.Action = "remove"
	}
	for len(keep) > 0 && strings.TrimSpace(keep[len(keep)-1]) == "" {
		keep = keep[:len(keep)-1]
	}
	out := strings.Join(keep, "\n")
	if out != "" {
		out += "\n"
	}

	// The parser confirms the edit: Larvyn's server as wanted, the rest as it was.
	got, gotRest, err := codexServer(out)
	if err != nil || !reflect.DeepEqual(gotRest, rest) {
		return c, errCodexUnsafe
	}
	if (add && !reflect.DeepEqual(got, codexWant(cli))) || (!add && got != nil) {
		return c, errCodexUnsafe
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
