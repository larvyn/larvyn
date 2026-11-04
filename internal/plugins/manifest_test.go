package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"larvyn/internal/updater"
)

const validManifest = `{
  "apiVersion": 1,
  "id": "acme.horizon",
  "name": "Horizon",
  "version": "1.0.0",
  "larvyn": ">=0.1.0",
  "permissions": ["project.read", "terminal.run"],
  "contributes": {
    "commands": [{"id": "restart", "title": "Restart Horizon", "run": "php artisan horizon:terminate"}],
    "stack": [{"key": "horizon", "title": "Horizon", "argv": ["php", "artisan", "horizon"]}],
    "views": [{"id": "dash", "title": "Horizon", "entry": "ui/index.html"}]
  }
}`

// writePlugin makes a plugin folder holding manifest and ui/index.html.
func writePlugin(t *testing.T, dir, manifest string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "ui", "index.html"), []byte("<!doctype html>"), 0o644)
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// edit returns validManifest with fn applied to its decoded form.
func edit(t *testing.T, fn func(m map[string]any)) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(validManifest), &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	b, _ := json.Marshal(m)
	return string(b)
}

func contributes(m map[string]any) map[string]any { return m["contributes"].(map[string]any) }

func TestValidManifest(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, validManifest)
	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "acme.horizon" || len(m.Contributes.Commands) != 1 || m.Contributes.Stack[0].Argv[2] != "horizon" {
		t.Errorf("manifest read wrong: %+v", m)
	}
}

func TestInvalidManifests(t *testing.T) {
	cases := []struct {
		name, want string
		fn         func(m map[string]any)
	}{
		{"newer api", "update Larvyn", func(m map[string]any) { m["apiVersion"] = 2 }},
		{"no api", "apiVersion must be 1", func(m map[string]any) { delete(m, "apiVersion") }},
		{"unknown field", "unknown field", func(m map[string]any) { m["permisions"] = []string{} }},
		{"bad id", "publisher.name", func(m map[string]any) { m["id"] = "Horizon" }},
		{"id without publisher", "publisher.name", func(m map[string]any) { m["id"] = "horizon" }},
		{"no name", "name is missing", func(m map[string]any) { m["name"] = " " }},
		{"bad version", "semantic version", func(m map[string]any) { m["version"] = "1.0" }},
		{"no range", "larvyn is missing", func(m map[string]any) { delete(m, "larvyn") }},
		{"javascript homepage", "http or https", func(m map[string]any) { m["homepage"] = "javascript:alert(1)" }},
		{"bad range", "comparator", func(m map[string]any) { m["larvyn"] = "~1.0" }},
		{"backend", "not supported yet", func(m map[string]any) { m["backend"] = map[string]any{"run": []string{"php", "main.php"}} }},
		{"unknown permission", "unknown permission", func(m map[string]any) { m["permissions"] = []string{"terminal.run", "root"} }},
		{"duplicate permission", "listed twice", func(m map[string]any) { m["permissions"] = []string{"terminal.run", "terminal.run"} }},
		{"network without domains", "go together", func(m map[string]any) { m["permissions"] = []string{"terminal.run", "network"} }},
		{"domains without network", "go together", func(m map[string]any) { m["network"] = []string{"api.example.com"} }},
		{"bad domain", "not a host", func(m map[string]any) {
			m["permissions"] = []string{"terminal.run", "network"}
			m["network"] = []string{"https://example.com/x"}
		}},
		{"domain with path", "not a host", func(m map[string]any) {
			m["permissions"] = []string{"terminal.run", "network"}
			m["network"] = []string{"localhost:8025/api"}
		}},
		{"require a path", "not a path", func(m map[string]any) {
			m["requires"] = []any{map[string]any{"program": "/usr/bin/mailpit"}}
		}},
		{"require twice", "listed twice", func(m map[string]any) {
			m["requires"] = []any{map[string]any{"program": "mailpit"}, map[string]any{"program": "mailpit"}}
		}},
		{"require unknown os", "use mac, linux or windows", func(m map[string]any) {
			m["requires"] = []any{map[string]any{"program": "mailpit", "install": map[string]string{"macos": "brew install mailpit"}}}
		}},
		{"require two-line install", "one line", func(m map[string]any) {
			m["requires"] = []any{map[string]any{"program": "mailpit", "install": map[string]string{"mac": "brew install mailpit\nrm -rf ~"}}}
		}},
		{"require bad url", "http or https", func(m map[string]any) {
			m["requires"] = []any{map[string]any{"program": "mailpit", "url": "file:///etc/passwd"}}
		}},
		{"env keys without permission", "go together", func(m map[string]any) { m["env"] = []string{"MAIL_HOST"} }},
		{"env permission without keys", "go together", func(m map[string]any) { m["permissions"] = []string{"terminal.run", "project.env.keys"} }},
		{"bad env key", "capital letters", func(m map[string]any) {
			m["permissions"] = []string{"terminal.run", "project.env.keys"}
			m["env"] = []string{"mail_host"}
		}},
		{"env key twice", "listed twice", func(m map[string]any) {
			m["permissions"] = []string{"terminal.run", "project.env.keys"}
			m["env"] = []string{"MAIL_HOST", "MAIL_HOST"}
		}},
		{"nothing contributed", "adds nothing", func(m map[string]any) { m["contributes"] = map[string]any{} }},
		{"command without terminal.run", "terminal.run", func(m map[string]any) { m["permissions"] = []string{"project.read"} }},
		{"duplicate command", "used twice", func(m map[string]any) {
			c := contributes(m)
			c["commands"] = append(c["commands"].([]any), c["commands"].([]any)[0])
		}},
		{"command without title", "no title", func(m map[string]any) {
			contributes(m)["commands"] = []any{map[string]any{"id": "x", "run": "ls"}}
		}},
		{"empty command", "nothing to run", func(m map[string]any) {
			contributes(m)["commands"] = []any{map[string]any{"id": "x", "title": "X", "run": " "}}
		}},
		{"empty argv", "no argv", func(m map[string]any) {
			contributes(m)["stack"] = []any{map[string]any{"key": "x", "title": "X", "argv": []string{}}}
		}},
		{"entry outside", "inside the plugin folder", func(m map[string]any) {
			contributes(m)["views"] = []any{map[string]any{"id": "x", "title": "X", "entry": "../secret.html"}}
		}},
		{"absolute entry", "inside the plugin folder", func(m map[string]any) {
			contributes(m)["views"] = []any{map[string]any{"id": "x", "title": "X", "entry": "/etc/x.html"}}
		}},
		{"entry not html", ".html file", func(m map[string]any) {
			contributes(m)["views"] = []any{map[string]any{"id": "x", "title": "X", "entry": "ui/app.js"}}
		}},
		{"missing entry", "does not exist", func(m map[string]any) {
			contributes(m)["views"] = []any{map[string]any{"id": "x", "title": "X", "entry": "ui/missing.html"}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writePlugin(t, dir, edit(t, c.fn))
			_, err := ReadManifest(dir)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

// A plugin made for a newer Larvyn uses fields this one doesn't know: the
// error must say to update Larvyn, using the plugin's own version range.
func TestNewerPluginSaysUpdateLarvyn(t *testing.T) {
	old := updater.Version
	updater.Version = "v0.1.0-beta.9"
	defer func() { updater.Version = old }()
	dir := t.TempDir()
	writePlugin(t, dir, edit(t, func(m map[string]any) {
		m["larvyn"] = ">=0.2.0"
		m["env"] = []string{"MAIL_HOST"}
	}))
	if _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), "needs Larvyn >=0.2.0; this is v0.1.0-beta.9, update Larvyn") {
		t.Errorf("error = %v", err)
	}
	// Without a newer range, the unknown field is named, with a hint.
	writePlugin(t, dir, edit(t, func(m map[string]any) { m["env"] = []string{"MAIL_HOST"} }))
	if _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), `unknown field "env" (it may need a newer Larvyn)`) {
		t.Errorf("error = %v", err)
	}
}

func TestNotJSON(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "{nope")
	if _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error = %v", err)
	}
}

func TestManifestTooLarge(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, `{"apiVersion": 1, "description": "`+strings.Repeat("x", maxManifestSize)+`"}`)
	if _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("error = %v", err)
	}
}

func TestEntrySymlinkOutside(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	writePlugin(t, dir, validManifest)
	secret := filepath.Join(outside, "secret.html")
	os.WriteFile(secret, []byte("x"), 0o644)
	os.Remove(filepath.Join(dir, "ui", "index.html"))
	if err := os.Symlink(secret, filepath.Join(dir, "ui", "index.html")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), "outside the plugin folder") {
		t.Errorf("error = %v", err)
	}
}

func TestCompatible(t *testing.T) {
	cases := []struct {
		r, v string
		want bool
	}{
		{">=0.1.0", "v0.1.0-beta.8", true}, // Larvyn's prerelease tag is ignored
		{">=0.2.0", "v0.1.0-beta.8", false},
		{">=0.1.0 <0.2.0", "v0.1.5", true},
		{">=0.1.0 <0.2.0", "v0.2.0", false},
		{"0.1.0", "v0.1.0", true},
		{"=0.1.0", "v0.1.1", false},
		{">0.1.0", "v0.1.0", false},
		{"<=0.1.0", "v0.1.0", true},
		{">=9.0.0", "dev", true},
		{">=0.1.0-beta.10", "v0.1.0-beta.9", false}, // a prerelease bound compares betas
		{">=0.1.0-beta.10", "v0.1.0-beta.10", true},
		{">=0.1.0-beta.10", "v0.1.0", true},
		{">=0.1.0-beta.10", "v0.2.0-beta.1", true},
	}
	for _, c := range cases {
		if err := checkRange(c.r); err != nil {
			t.Fatalf("checkRange(%q): %v", c.r, err)
		}
		if got := Compatible(c.r, c.v); got != c.want {
			t.Errorf("Compatible(%q, %q) = %v, want %v", c.r, c.v, got, c.want)
		}
	}
}

func TestNetworkHosts(t *testing.T) {
	for _, h := range []string{"api.example.com", "*.example.com", "localhost", "localhost:8025", "127.0.0.1:1025", "example.com:443", "localhost:65535", "localhost:1"} {
		if !validHost(h) {
			t.Errorf("%q should be accepted", h)
		}
	}
	for _, h := range []string{"https://example.com", "example", "192.168.1.5", "localhost:", "*", "ex ample.com", "localhost:99999", "localhost:65536", "localhost:0"} {
		if validHost(h) {
			t.Errorf("%q should be refused", h)
		}
	}
}
