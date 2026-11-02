// Integration tests for the wiring in main.go: the services working together
// against a real Laravel app. Each package's own tests live next to it.
package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"larvyn/internal/laravel"
	"larvyn/internal/project"
	"larvyn/internal/stack"
	"larvyn/internal/store"
)

func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "larvyn-test")
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// LARVYN_TEST_PROJECT=/path/to/laravel go test -run Bridge -v
func TestBridgeAgainstRealProject(t *testing.T) {
	dir := os.Getenv("LARVYN_TEST_PROJECT")
	if dir == "" {
		t.Skip("LARVYN_TEST_PROJECT not set")
	}
	p, err := (&project.ProjectService{}).Add(dir)
	if err != nil {
		t.Fatal(err)
	}
	info := project.Inspect(p)
	t.Logf("laravel=%s php=%s db=%s packages=%d branch=%s", info.LaravelVersion, info.PHPConstraint, info.DBConnection, len(info.Packages), info.Branch)
	if !info.IsLaravel {
		t.Fatal("not detected as Laravel")
	}
	for _, action := range []string{"ping", "models", "migrations", "schedule", "db.overview", "queue.failed"} {
		raw, err := (&laravel.LaravelService{}).Bridge(p.ID, action, map[string]any{})
		if err != nil {
			t.Errorf("%s: %v", action, err)
			continue
		}
		var r struct {
			OK    bool            `json:"ok"`
			Error json.RawMessage `json:"error"`
		}
		_ = json.Unmarshal(raw, &r)
		s := string(raw)
		if len(s) > 220 {
			s = s[:220] + "…"
		}
		t.Logf("%s ok=%v %s", action, r.OK, s)
	}
	raw, err := (&laravel.LaravelService{}).Bridge(p.ID, "tinker", map[string]any{"code": "$a = 20;\n$a + 22"})
	if err != nil || !strings.Contains(string(raw), `"dump":"42`) {
		t.Errorf("tinker: %v %s", err, raw)
	}
	l := &laravel.LaravelService{}
	if cmds, err := l.Commands(p.ID, false); err != nil || len(cmds) < 100 {
		t.Errorf("artisan list: %v", err)
	}
	if routes, err := l.Routes(p.ID); err != nil {
		t.Errorf("routes: %v", err)
	} else {
		t.Logf("routes json %d bytes", len(routes))
	}
	items, err := (&stack.StackService{}).Items(p.ID)
	if err != nil {
		t.Errorf("stack items: %v", err)
	}
	t.Logf("stack: %d items", len(items))
}
