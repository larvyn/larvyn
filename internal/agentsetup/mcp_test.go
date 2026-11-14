package agentsetup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// mcpSetup has every agent that gets the MCP server, each with a file of
// its own that Larvyn must keep.
func mcpSetup(t *testing.T) Options {
	t.Helper()
	o := setup(t)
	dir := filepath.Dir(o.ClaudeDir)
	o.CursorDir = filepath.Join(dir, ".cursor")
	o.GeminiDir = filepath.Join(dir, ".gemini")
	o.WindsurfDir = filepath.Join(dir, ".codeium", "windsurf")
	o.OpenCodeDir = filepath.Join(dir, ".config", "opencode")
	for _, d := range []string{o.CursorDir, o.GeminiDir, o.WindsurfDir, o.OpenCodeDir} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(o.ClaudeJSON, []byte(`{"numStartups": 12, "mcpServers": {"other": {"type": "stdio", "command": "other", "args": []}}, "projects": {}}`), 0o600)
	os.WriteFile(filepath.Join(o.GeminiDir, "settings.json"), []byte(`{"theme": "GitHub"}`), 0o600)
	os.WriteFile(filepath.Join(o.CodexDir, "config.toml"), []byte("model = \"gpt-5\"\n\n[mcp_servers.other]\ncommand = \"other\"\n\n[profiles.fast]\nmodel = \"mini\"\n"), 0o600)
	return o
}

func server(t *testing.T, path string, keys ...string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(read(t, path)), &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	for _, k := range append(keys, "larvyn") {
		m, _ = m[k].(map[string]any)
	}
	return m
}

func TestMCPServersAreAdded(t *testing.T) {
	o := mcpSetup(t)
	cs, err := Install(o, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"Claude Code MCP server=create", "Codex MCP server=create", "Cursor MCP server=create", "Gemini CLI MCP server=create", "Windsurf MCP server=create", "OpenCode MCP server=create"} {
		if !strings.Contains(actions(cs), w) {
			t.Errorf("no %s in %s", w, actions(cs))
		}
	}
	// The CLI's full path, so it works outside Larvyn's terminals.
	for path, key := range map[string]string{
		o.ClaudeJSON:                                    "mcpServers",
		filepath.Join(o.CursorDir, "mcp.json"):          "mcpServers",
		filepath.Join(o.GeminiDir, "settings.json"):     "mcpServers",
		filepath.Join(o.WindsurfDir, "mcp_config.json"): "mcpServers",
	} {
		s := server(t, path, key)
		if s["command"] != o.CLI || len(s["args"].([]any)) != 1 || s["args"].([]any)[0] != "mcp" {
			t.Errorf("%s: %v", path, s)
		}
	}
	// OpenCode 2 nests its servers in mcp.servers.
	if s := server(t, filepath.Join(o.OpenCodeDir, "opencode.json"), "mcp", "servers"); s["type"] != "local" || s["command"].([]any)[0] != o.CLI || s["enabled"] != nil {
		t.Errorf("opencode: %v", s)
	}
	// What was there stays, in its order.
	claude := read(t, o.ClaudeJSON)
	if !strings.Contains(claude, `"other"`) || strings.Index(claude, "numStartups") > strings.Index(claude, "projects") {
		t.Errorf(".claude.json:\n%s", claude)
	}
	if !strings.Contains(read(t, filepath.Join(o.GeminiDir, "settings.json")), `"theme": "GitHub"`) {
		t.Error("gemini settings lost the theme")
	}
	codex := read(t, filepath.Join(o.CodexDir, "config.toml"))
	if !strings.Contains(codex, "model = \"gpt-5\"\n\n[mcp_servers.other]") || !strings.Contains(codex, "[profiles.fast]\nmodel = \"mini\"") ||
		!strings.HasSuffix(codex, "\n\n[mcp_servers.larvyn]\ncommand = \""+o.CLI+"\"\nargs = [\"mcp\"]\n") {
		t.Errorf("config.toml:\n%s", codex)
	}

	// Again: nothing to do.
	cs, err = Install(o, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Action != "unchanged" {
			t.Errorf("second run: %s", actions(cs))
			break
		}
	}

	// Another state folder: the servers follow it, once.
	o.CLI = `C:\Users\me\AppData\Roaming\larvyn\bin\larvyn.exe`
	if _, err := Install(o, false); err != nil {
		t.Fatal(err)
	}
	codex = read(t, filepath.Join(o.CodexDir, "config.toml"))
	if strings.Count(codex, "[mcp_servers.larvyn]") != 1 || !strings.Contains(codex, `command = "C:\\Users\\me\\AppData\\Roaming\\larvyn\\bin\\larvyn.exe"`) {
		t.Errorf("config.toml after a new path:\n%s", codex)
	}
	if s := server(t, filepath.Join(o.CursorDir, "mcp.json"), "mcpServers"); s["command"] != o.CLI {
		t.Errorf("cursor after a new path: %v", s)
	}

	// Remove takes out only Larvyn's servers.
	if _, err := Remove(o, false); err != nil {
		t.Fatal(err)
	}
	if s := server(t, o.ClaudeJSON, "mcpServers"); s != nil || !strings.Contains(read(t, o.ClaudeJSON), `"other"`) {
		t.Errorf(".claude.json after remove:\n%s", read(t, o.ClaudeJSON))
	}
	if codex := read(t, filepath.Join(o.CodexDir, "config.toml")); codex != "model = \"gpt-5\"\n\n[mcp_servers.other]\ncommand = \"other\"\n\n[profiles.fast]\nmodel = \"mini\"\n" {
		t.Errorf("config.toml after remove:\n%q", codex)
	}
}

func TestACodexTableInTheMiddleIsReplacedInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[mcp_servers.larvyn]\ncommand = \"larvyn\"\nargs = [\"mcp\"]\n\n[mcp_servers.larvyn.env]\nA = \"1\"\n\n[profiles.fast]\nmodel = \"mini\"\n"), 0o600)
	c, err := editCodexMCP(path, "/bin/larvyn", true, false)
	if err != nil || c.Action != "update" {
		t.Fatalf("%+v %v", c, err)
	}
	if got := read(t, path); got != "[mcp_servers.larvyn]\ncommand = \"/bin/larvyn\"\nargs = [\"mcp\"]\n\n[profiles.fast]\nmodel = \"mini\"\n" {
		t.Errorf("%q", got)
	}
}

func TestAnotherLarvynServerIsLeftAlone(t *testing.T) {
	o := mcpSetup(t)
	mine := `{"mcpServers": {"larvyn": {"command": "npx", "args": ["my-larvyn-thing"]}}}`
	os.WriteFile(filepath.Join(o.CursorDir, "mcp.json"), []byte(mine), 0o600)
	toml := "[mcp_servers]\nlarvyn = { command = \"npx\" }\n"
	os.WriteFile(filepath.Join(o.CodexDir, "config.toml"), []byte(toml), 0o600)
	cs, err := Install(o, false)
	if err == nil || !strings.Contains(err.Error(), "left alone") {
		t.Errorf("err %v", err)
	}
	if read(t, filepath.Join(o.CursorDir, "mcp.json")) != mine || read(t, filepath.Join(o.CodexDir, "config.toml")) != toml {
		t.Error("a server that isn't Larvyn's was changed")
	}
	if !strings.Contains(actions(cs), "Gemini CLI MCP server=create") {
		t.Errorf("the rest wasn't set up: %s", actions(cs))
	}
	if _, err := Remove(o, false); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(o.CursorDir, "mcp.json")) != mine || read(t, filepath.Join(o.CodexDir, "config.toml")) != toml {
		t.Error("remove changed a server that isn't Larvyn's")
	}
}

func TestOpenCodeJSONCIsLeftToTheUser(t *testing.T) {
	o := mcpSetup(t)
	jsonc := filepath.Join(o.OpenCodeDir, "opencode.jsonc")
	os.WriteFile(jsonc, []byte("{\n  // mine\n}\n"), 0o600)
	cs, err := Install(o, false)
	if err != nil || !strings.Contains(actions(cs), "OpenCode MCP server=skipped") {
		t.Fatalf("%s %v", actions(cs), err)
	}
	if _, err := os.Stat(filepath.Join(o.OpenCodeDir, "opencode.json")); err == nil {
		t.Error("made an opencode.json next to the .jsonc")
	}
}

func TestAnyAgentGetsTheSharedSkill(t *testing.T) {
	dir := t.TempDir()
	o := Options{AgentsDir: filepath.Join(dir, ".agents"), CursorDir: filepath.Join(dir, ".cursor"), CLI: "larvyn"}
	os.MkdirAll(o.CursorDir, 0o755)
	cs, err := Install(o, false)
	if err != nil || actions(cs) != "Skill for Codex and other agents=create, Cursor MCP server=create" {
		t.Errorf("%s %v", actions(cs), err)
	}
}

func TestOpenCode1KeepsServersInMCP(t *testing.T) {
	o := mcpSetup(t)
	o.OpenCodeMajor = 1
	if _, err := Install(o, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(o.OpenCodeDir, "opencode.json")
	if s := server(t, path, "mcp"); s["enabled"] != true || s["command"].([]any)[1] != "mcp" {
		t.Errorf("opencode 1: %s", read(t, path))
	}
	// Updated to OpenCode 2 since: remove still finds it.
	o.OpenCodeMajor = 2
	if _, err := Remove(o, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, path), "larvyn") {
		t.Errorf("after remove: %s", read(t, path))
	}
}

func TestOnlyALarvynBinaryWithMCPIsOurs(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/home/me/.config/larvyn/bin/larvyn":                true,
		`C:\Users\me\AppData\Roaming\larvyn\bin\larvyn.exe`: true,
		"larvyn":                   true,
		"/opt/larvyn-tools/server": false,
		"/usr/bin/larvynx":         false,
	} {
		n, _ := parse([]byte(`{"command": ` + strconv.Quote(cmd) + `, "args": ["mcp"]}`))
		if oursMCP(n) != want {
			t.Errorf("%s: ours %v", cmd, !want)
		}
	}
}

func TestCodexTablesThatArentOursAreLeftAlone(t *testing.T) {
	for name, toml := range map[string]string{
		"comment mentions larvyn": "[mcp_servers.larvyn]\ncommand = \"npx\" # larvyn\nargs = [\"mcp\"]\n",
		"other args":              "[mcp_servers.larvyn]\ncommand = \"/bin/larvyn\"\nargs = [\"mcp\", \"--other\"]\n",
		"literal key, other cmd":  "[mcp_servers.'larvyn']\ncommand = \"npx\"\n",
		"inline larvyn":           "[mcp_servers]\n'larvyn' = { command = \"npx\" }\n",
		"inline mcp_servers":      "model = \"x\"\nmcp_servers = { other = { command = \"other\" } }\n",
		"after a nested array":    "example = [\n  [1, 2],\n]\nmcp_servers = { other = { command = \"other\" } }\n",
		"quoted parent key":       "[\"mcp_servers\".larvyn]\ncommand = \"npx\"\n",
		"literal parent key":      "['mcp_servers'.larvyn]\ncommand = \"npx\"\n",
		"a table in a string":     "developer_instructions = '''\nExample:\n[mcp_servers.larvyn]\ncommand = \"/bin/larvyn\"\nargs = [\"mcp\"]\n'''\nmodel = \"example\"\n",
		"not TOML":                "[oops\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		os.WriteFile(path, []byte(toml), 0o600)
		if _, err := editCodexMCP(path, "/bin/larvyn", true, false); err == nil || !strings.Contains(err.Error(), "left alone") {
			t.Errorf("%s: err %v", name, err)
		}
		if _, err := editCodexMCP(path, "/bin/larvyn", false, false); err != nil {
			t.Errorf("%s: remove: %v", name, err)
		}
		if read(t, path) != toml {
			t.Errorf("%s: changed:\n%s", name, read(t, path))
		}
	}
}

func TestALiteralQuotedCodexTableIsUpdated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[mcp_servers.'larvyn']\ncommand = 'C:\\old\\larvyn.exe'\nargs = ['mcp']\n"), 0o600)
	c, err := editCodexMCP(path, "/bin/larvyn", true, false)
	if err != nil || c.Action != "update" {
		t.Fatalf("%+v %v", c, err)
	}
	if got := read(t, path); got != "[mcp_servers.larvyn]\ncommand = \"/bin/larvyn\"\nargs = [\"mcp\"]\n" {
		t.Errorf("%q", got)
	}
}
