package plugins

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"larvyn/internal/terminal"
)

// withPrograms makes which find only the given programs.
func withPrograms(t *testing.T, found ...string) {
	t.Helper()
	old := which
	which = func(name string) string {
		for _, f := range found {
			if f == name {
				return "/usr/local/bin/" + name
			}
		}
		return ""
	}
	t.Cleanup(func() { which = old })
}

var requiresManifest = func(t *testing.T) string {
	return edit(t, func(m map[string]any) {
		m["requires"] = []any{
			map[string]any{"program": "mailpit", "install": map[string]string{osKey(): "echo installing-mailpit"}, "url": "https://mailpit.axllent.org"},
			map[string]any{"program": "php"},
			map[string]any{"program": "redis-server", "url": "https://redis.io"},
		}
	})
}

func TestMissingPrograms(t *testing.T) {
	reset(t)
	withPrograms(t, "php")
	writePlugin(t, filepath.Join(Dir(), "acme.horizon"), requiresManifest(t))
	p, err := find("acme.horizon")
	if err != nil || p.Error != "" {
		t.Fatalf("%+v %v", p, err)
	}
	if len(p.Missing) != 2 || p.Missing[0].Program != "mailpit" || p.Missing[0].Install != "echo installing-mailpit" || p.Missing[1].Install != "" {
		t.Errorf("missing: %+v", p.Missing)
	}
	src := t.TempDir()
	writePlugin(t, src, requiresManifest(t))
	if q, _ := (&PluginService{}).Inspect(src); len(q.Missing) != 2 {
		t.Errorf("the install dialog should see the missing programs: %+v", q.Missing)
	}
}

func TestInstallRequirement(t *testing.T) {
	reset(t)
	withPrograms(t, "php")
	writePlugin(t, filepath.Join(Dir(), "acme.horizon"), requiresManifest(t))
	s := &PluginService{}

	if _, err := s.InstallRequirement("acme.horizon", "php", ""); err == nil || !strings.Contains(err.Error(), "not missing") {
		t.Errorf("php is installed: %v", err)
	}
	if _, err := s.InstallRequirement("acme.horizon", "redis-server", ""); err == nil || !strings.Contains(err.Error(), "https://redis.io") {
		t.Errorf("no command for this system should point to the url: %v", err)
	}
	if _, err := s.InstallRequirement("acme.nope", "mailpit", "echo installing-mailpit"); err == nil {
		t.Error("unknown plugin")
	}
	if _, err := s.InstallRequirement("acme.horizon", "mailpit", "echo something-else"); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("a command other than the confirmed one must not run: %v", err)
	}

	sess, err := s.InstallRequirement("acme.horizon", "mailpit", "echo installing-mailpit")
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Sessions.Close(sess.ID)
	if sess.Command != "echo installing-mailpit" || sess.Title != "Install mailpit" {
		t.Errorf("session: %+v", sess)
	}
	for i := 0; i < 100; i++ {
		if out, _ := terminal.Sessions.Output(sess.ID); strings.Contains(out, "installing-mailpit") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("the install command did not run in the terminal")
}
