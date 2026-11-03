package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"larvyn/internal/store"
)

func TestSyncAppURL(t *testing.T) {
	t.Setenv("LARVYN_HOME", t.TempDir())
	cases := []struct {
		name, env, previous, want string
		changed                   bool
	}{
		{"localhost default", "APP_URL=http://localhost:8000", "", "http://acme.test", true},
		{"empty", "APP_URL=", "", "http://acme.test", true},
		{"previous site URL", "APP_URL=http://acme.test/", "http://acme.test", "https://acme.test", true},
		{"chosen by the user", "APP_URL=https://acme.dev", "", "https://acme.dev", false},
		{"already right", "APP_URL=http://acme.test", "", "http://acme.test", false},
		{"no APP_URL", "APP_NAME=Acme", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			env := filepath.Join(dir, ".env")
			if err := os.WriteFile(env, []byte("APP_NAME=Acme\n"+c.env+"\nDB_CONNECTION=sqlite\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			next := "http://acme.test"
			if c.previous != "" {
				next = "https://acme.test"
			}
			changed := syncAppURL(store.Project{ID: "p1", Path: dir}, c.previous, next)
			if changed != c.changed {
				t.Errorf("changed = %v, want %v", changed, c.changed)
			}
			raw, _ := os.ReadFile(env)
			if c.want != "" && !strings.Contains(string(raw), "APP_URL="+c.want+"\n") {
				t.Errorf(".env is now:\n%s", raw)
			}
			if !strings.Contains(string(raw), "DB_CONNECTION=sqlite") {
				t.Errorf("other lines were lost:\n%s", raw)
			}
		})
	}
}

func TestSiteName(t *testing.T) {
	for in, want := range map[string]string{"booknera": "booknera", "My_App 2": "my-app-2", "-x-": "x"} {
		if got := siteName(in); got != want {
			t.Errorf("siteName(%q) = %q, want %q", in, got, want)
		}
	}
}
