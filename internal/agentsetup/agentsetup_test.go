package agentsetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claudeSettings is a settings.json with the user's own keys, numbers and
// another tool's hooks, in an order a Go map would lose.
const claudeSettings = `{
  "theme": "dark",
  "permissions": {
    "allow": ["Bash(git status)"],
    "deny": []
  },
  "hooks": {
    "Stop": [
      {
        "matcher": "",
        "hooks": [{"type": "command", "command": "/opt/other/notify.sh", "timeout": 5}]
      }
    ],
    "SessionStart": [
      {
        "matcher": "startup",
        "hooks": [{"type": "command", "command": "/opt/other/start.sh", "timeout": 5}]
      }
    ]
  },
  "model": "opus",
  "cleanupPeriodDays": 30.5,
  "url": "https://example.com/?a=1&b=<2>"
}
`

func setup(t *testing.T) Options {
	t.Helper()
	dir := t.TempDir()
	o := Options{
		ClaudeDir: filepath.Join(dir, ".claude"),
		CodexDir:  filepath.Join(dir, ".codex"),
		AgentsDir: filepath.Join(dir, ".agents"),
		CLI:       "/home/me/.config/larvyn/bin/larvyn",
	}
	os.MkdirAll(o.ClaudeDir, 0o755)
	os.MkdirAll(o.CodexDir, 0o755)
	os.WriteFile(filepath.Join(o.ClaudeDir, "settings.json"), []byte(claudeSettings), 0o600)
	return o
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func actions(cs []Change) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.What+"="+c.Action)
	}
	return strings.Join(out, ", ")
}

func TestInstall(t *testing.T) {
	o := setup(t)
	cs, err := Install(o, false)
	if err != nil {
		t.Fatal(err)
	}
	want := "Claude Code skill=create, Claude Code session hook=create, Skill for Codex and other agents=create, Codex session hook=create"
	if got := actions(cs); got != want {
		t.Errorf("changes:\n%s\nwant:\n%s", got, want)
	}
	// Codex asks the user to review a new hook: the change says so.
	if !strings.Contains(cs[3].Note, "/hooks") {
		t.Errorf("codex note: %q", cs[3].Note)
	}
	for _, p := range []string{filepath.Join(o.ClaudeDir, "skills", "larvyn", "SKILL.md"), filepath.Join(o.AgentsDir, "skills", "larvyn", "SKILL.md")} {
		if s := read(t, p); !strings.HasPrefix(s, "---\nname: larvyn\n") || !strings.Contains(s, mark) {
			t.Errorf("%s: %.60q", p, s)
		}
	}

	settings := read(t, filepath.Join(o.ClaudeDir, "settings.json"))
	// The user's keys stay in their order, values as written.
	order := []string{`"theme"`, `"permissions"`, `"hooks"`, `"Stop"`, `"SessionStart"`, `"model"`, `"cleanupPeriodDays": 30.5`, `"url": "https://example.com/?a=1&b=<2>"`}
	last := -1
	for _, k := range order {
		i := strings.Index(settings, k)
		if i < 0 || i < last {
			t.Fatalf("%s out of place in:\n%s", k, settings)
		}
		last = i
	}
	// Larvyn's hook comes after the other tool's, which stays.
	other := strings.Index(settings, "/opt/other/start.sh")
	ourHook := strings.Index(settings, `"/home/me/.config/larvyn/bin/larvyn agents hook"`)
	if other < 0 || ourHook < other || !strings.Contains(settings, "/opt/other/notify.sh") {
		t.Errorf("hooks:\n%s", settings)
	}
	if b := read(t, filepath.Join(o.ClaudeDir, "settings.json.larvyn-backup")); b != claudeSettings {
		t.Error("the backup is not the old file")
	}
	codex := read(t, filepath.Join(o.CodexDir, "hooks.json"))
	if !strings.Contains(codex, `"SessionStart"`) || strings.Contains(codex, "matcher") {
		t.Errorf("codex hooks.json:\n%s", codex)
	}

	// Again: nothing to do, and nothing rewritten.
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
	if again := read(t, filepath.Join(o.ClaudeDir, "settings.json")); again != settings {
		t.Error("settings.json changed on the second run")
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	o := setup(t)
	cs, err := Install(o, true)
	if err != nil || len(cs) != 4 {
		t.Fatalf("%v %v", actions(cs), err)
	}
	if read(t, filepath.Join(o.ClaudeDir, "settings.json")) != claudeSettings {
		t.Error("dry run wrote settings.json")
	}
	for _, p := range []string{filepath.Join(o.CodexDir, "hooks.json"), filepath.Join(o.ClaudeDir, "skills"), filepath.Join(o.AgentsDir)} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("dry run made %s", p)
		}
	}
}

func TestRemove(t *testing.T) {
	o := setup(t)
	if _, err := Install(o, false); err != nil {
		t.Fatal(err)
	}
	if cs, _ := Remove(o, true); len(cs) != 4 {
		t.Errorf("remove dry run: %s", actions(cs))
	}
	cs, err := Remove(o, false)
	if err != nil || len(cs) != 4 {
		t.Fatalf("remove: %s %v", actions(cs), err)
	}
	settings := read(t, filepath.Join(o.ClaudeDir, "settings.json"))
	if strings.Contains(settings, "agents hook") || !strings.Contains(settings, "/opt/other/start.sh") || !strings.Contains(settings, `"model"`) {
		t.Errorf("after remove:\n%s", settings)
	}
	// Only Larvyn's hook was in Codex' file: its hooks key goes.
	if codex := read(t, filepath.Join(o.CodexDir, "hooks.json")); strings.TrimSpace(codex) != "{}" {
		t.Errorf("codex after remove: %s", codex)
	}
	if _, err := os.Stat(filepath.Join(o.ClaudeDir, "skills", "larvyn")); err == nil {
		t.Error("the skill folder is still there")
	}
	// Nothing left: removing again does nothing.
	if cs, err := Remove(o, false); err != nil || len(cs) != 0 {
		t.Errorf("second remove: %s %v", actions(cs), err)
	}
}

func TestAnOldHookIsReplaced(t *testing.T) {
	o := setup(t)
	if _, err := Install(o, false); err != nil {
		t.Fatal(err)
	}
	// Another state folder now: the hook points at it, once.
	o.CLI = "/other home/larvyn/bin/larvyn"
	cs, err := Install(o, false)
	if err != nil {
		t.Fatal(err)
	}
	if cs[1].Action != "update" {
		t.Errorf("changes: %s", actions(cs))
	}
	settings := read(t, filepath.Join(o.ClaudeDir, "settings.json"))
	if strings.Count(settings, "agents hook") != 1 || !strings.Contains(settings, `"\"/other home/larvyn/bin/larvyn\" agents hook"`) {
		t.Errorf("settings:\n%s", settings)
	}
}

func TestWhatIsNotLarvynsIsLeftAlone(t *testing.T) {
	o := setup(t)
	// A skill of the same name the user wrote.
	mine := filepath.Join(o.ClaudeDir, "skills", "larvyn", "SKILL.md")
	os.MkdirAll(filepath.Dir(mine), 0o755)
	os.WriteFile(mine, []byte("my own notes"), 0o644)
	// A Codex file that isn't JSON.
	os.WriteFile(filepath.Join(o.CodexDir, "hooks.json"), []byte("{oops"), 0o644)
	cs, err := Install(o, false)
	if err == nil || !strings.Contains(err.Error(), "left alone") {
		t.Errorf("err %v", err)
	}
	if read(t, mine) != "my own notes" || read(t, filepath.Join(o.CodexDir, "hooks.json")) != "{oops" {
		t.Error("a file that isn't Larvyn's was changed")
	}
	// The rest was still set up.
	if !strings.Contains(actions(cs), "Claude Code session hook=create") {
		t.Errorf("changes: %s", actions(cs))
	}
	if _, err := Remove(o, false); err == nil {
		t.Log("remove reports the bad file too")
	}
	if read(t, mine) != "my own notes" {
		t.Error("remove deleted the user's skill")
	}
}

func TestOnlyInstalledAgentsAreSetUp(t *testing.T) {
	dir := t.TempDir()
	o := Options{ClaudeDir: filepath.Join(dir, "c"), CodexDir: filepath.Join(dir, "x"), AgentsDir: filepath.Join(dir, "a"), CLI: "larvyn"}
	if _, err := Install(o, false); err == nil {
		t.Error("set up with no agent installed")
	}
	os.MkdirAll(o.CodexDir, 0o755)
	cs, err := Install(o, false)
	if err != nil || actions(cs) != "Skill for Codex and other agents=create, Codex session hook=create" {
		t.Errorf("codex only: %s %v", actions(cs), err)
	}
	if _, err := os.Stat(o.ClaudeDir); err == nil {
		t.Error("made a Claude Code folder")
	}
}

func TestDefaultsFollowTheEnvironment(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/x/claude")
	t.Setenv("CODEX_HOME", "/x/codex")
	o, err := Defaults("larvyn")
	if err != nil || o.ClaudeDir != "/x/claude" || o.CodexDir != "/x/codex" {
		t.Errorf("%+v %v", o, err)
	}
}
