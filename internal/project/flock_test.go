package project

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestRestoreAppURL(t *testing.T) {
	t.Setenv("LARVYN_HOME", t.TempDir())
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	if err := os.WriteFile(env, []byte("APP_URL=http://localhost:8000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := store.Project{ID: "r1", Path: dir, Port: 8003}
	if err := store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) }); err != nil {
		t.Fatal(err)
	}

	// Serving with Flock remembers the original; switching to HTTPS does not
	// overwrite it with the http site URL.
	if !syncAppURL(p, "", "http://acme.test") || !syncAppURL(p, "http://acme.test", "https://acme.test") {
		t.Fatal("APP_URL was not synced")
	}
	if got, _ := store.FindProject("r1"); got.AppURLBeforeFlock != "http://localhost:8000" {
		t.Fatalf("remembered %q", got.AppURLBeforeFlock)
	}

	svc := &ProjectService{}
	if url, err := svc.RestoreAppURL("r1"); err != nil || url != "http://localhost:8000" {
		t.Fatalf("RestoreAppURL = %q, %v", url, err)
	}
	if raw, _ := os.ReadFile(env); string(raw) != "APP_URL=http://localhost:8000\n" {
		t.Errorf(".env is now %q", raw)
	}
	if got, _ := store.FindProject("r1"); got.AppURLBeforeFlock != "" {
		t.Errorf("the remembered URL should be cleared, is %q", got.AppURLBeforeFlock)
	}

	// Without a remembered URL, it falls back to artisan serve's.
	if err := os.WriteFile(env, []byte("APP_URL=https://acme.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if url, _ := svc.RestoreAppURL("r1"); url != "http://127.0.0.1:8003" {
		t.Errorf("fallback = %q", url)
	}
}

func TestSetAppURLWritesSafely(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LARVYN_HOME", home)
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	if err := os.WriteFile(env, []byte("APP_URL=http://localhost\nAPP_KEY=x\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := setAppURL(store.Project{ID: "w1", Path: dir}, "http://acme.test", false); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(env); string(raw) != "APP_URL=http://acme.test\nAPP_KEY=x\n" {
		t.Errorf(".env is now %q", raw)
	}
	if st, _ := os.Stat(env); runtime.GOOS != "windows" && st.Mode().Perm() != 0o640 {
		t.Errorf("permissions changed to %v", st.Mode().Perm())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".*.larvyn-*")); len(left) > 0 {
		t.Errorf("temporary files left: %v", left)
	}
	backups, _ := filepath.Glob(filepath.Join(home, "backups", "w1-.env-*"))
	if len(backups) != 1 {
		t.Fatalf("backups: %v", backups)
	}
	if raw, _ := os.ReadFile(backups[0]); string(raw) != "APP_URL=http://localhost\nAPP_KEY=x\n" {
		t.Errorf("backup holds %q", raw)
	}
}

func TestSameSite(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"http://acme.test", "https://acme.test", true},
		{"https://ACME.test/", "http://acme.test", true},
		{"http://acme.test:8080", "http://acme.test", false},
		{"http://localhost", "http://acme.test", false},
		{"", "http://acme.test", false},
	} {
		if got := sameSite(c.a, c.b); got != c.want {
			t.Errorf("sameSite(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
