package stack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"larvyn/internal/store"
)

// Tests never read or write the user's real Larvyn state, and inspect a
// project straight from its files (main wires the project service instead).
func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "larvyn-stack-test")
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	Inspect = fileFacts
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func fileFacts(p store.Project) Facts {
	exists := func(rel string) bool { _, err := os.Stat(filepath.Join(p.Path, rel)); return err == nil }
	f := Facts{IsLaravel: exists("artisan"), HasVendor: exists("vendor"), HasNodeModules: exists("node_modules")}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if b, err := os.ReadFile(filepath.Join(p.Path, "package.json")); err == nil && json.Unmarshal(b, &pkg) == nil {
		f.NpmScripts = pkg.Scripts
	}
	var lock struct {
		Packages []struct {
			Name string `json:"name"`
		} `json:"packages"`
	}
	if b, err := os.ReadFile(filepath.Join(p.Path, "composer.lock")); err == nil && json.Unmarshal(b, &lock) == nil {
		for _, x := range lock.Packages {
			f.Packages = append(f.Packages, x.Name)
		}
	}
	return f
}

// Against a real Laravel app when one is given, so the hook's contract
// (serve, vite, queue and the package-driven items) is covered.
func TestStackItemsOnAProject(t *testing.T) {
	dir := os.Getenv("LARVYN_TEST_PROJECT")
	if dir == "" {
		t.Skip("set LARVYN_TEST_PROJECT to a Laravel app")
	}
	items := stackItems(store.Project{ID: "items", Path: dir, Port: 8040})
	keys := map[string]bool{}
	for _, it := range items {
		keys[it.Key] = true
		t.Logf("%-8s available=%v dev=%v %q", it.Key, it.Available, it.Dev, it.Argv)
	}
	for _, k := range []string{"serve", "vite", "queue"} {
		if !keys[k] {
			t.Errorf("missing %s in %v", k, keys)
		}
	}
}
