package stack

import (
	"errors"
	"strings"
	"testing"

	"larvyn/internal/store"
)

func TestSplitCommand(t *testing.T) {
	for in, want := range map[string][]string{
		"php artisan serve":                      {"php", "artisan", "serve"},
		`php artisan queue:listen --queue="a b"`: {"php", "artisan", "queue:listen", "--queue=a b"},
		"npm run dev":                            {"npm", "run", "dev"},
	} {
		got, ok := splitCommand(in)
		if !ok || strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("splitCommand(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"npm run build && npm run dev", "php artisan foo | tee x", `echo "open`} {
		if _, ok := splitCommand(in); ok {
			t.Errorf("splitCommand(%q) should need a shell", in)
		}
	}
}

func TestDevStackItems(t *testing.T) {
	serve := StackItem{Key: "serve", Label: "HTTP server", Argv: []string{"php", "artisan", "serve", "--host=127.0.0.1", "--port=8040"}}
	vite := StackItem{Key: "vite", Label: "Vite"}
	items := devStackItems([]DevProcess{
		{Name: "server", Command: "php artisan serve --port=9000"},
		{Name: "queue", Command: "php artisan queue:listen --tries=1 --timeout=0"},
		{Name: "vite", Command: "npm run dev"},
		{Name: "ssr", Command: "php artisan inertia:start-ssr"},
		{Name: "docs", Command: "npm run docs:dev && echo done"},
	}, serve, vite)
	keys := []string{}
	for _, it := range items {
		keys = append(keys, it.Key)
		if !it.Dev {
			t.Errorf("%s is not marked as a dev process", it.Key)
		}
	}
	if strings.Join(keys, ",") != "serve,queue,vite,ssr,docs" {
		t.Fatalf("keys = %v", keys)
	}
	if got := strings.Join(items[0].Argv, " "); got != "php artisan serve --host=127.0.0.1 --port=8040" {
		t.Errorf("serve keeps the project's port, got %q", got)
	}
	if items[3].Argv[1] != "artisan" || items[3].Label != "Ssr" {
		t.Errorf("custom artisan process: %+v", items[3])
	}
	if items[4].Argv[0] == "npm" {
		t.Errorf("a chained command must run through the shell: %v", items[4].Argv)
	}
}

func TestPluginStackItems(t *testing.T) {
	dir := t.TempDir()
	p := store.Project{ID: "plugtest", Path: dir}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) })
	defer store.Update(func(d *store.State) { d.Projects = nil })
	old := PluginItems
	defer func() { PluginItems = old }()
	confirmErr := errors.New("plugin-confirm:{}")
	PluginItems = func(store.Project) []StackItem {
		return []StackItem{
			{Key: "plugin:acme.x:mail", Label: "Mail", Argv: []string{"mailpit"}, Available: true, Plugin: "X", NeedConfirm: confirmErr},
			{Key: "plugin:acme.x:gone", Label: "Gone", Argv: []string{"nope"}, Available: false, Reason: "needs nope", Plugin: "X"},
		}
	}
	items, err := (&StackService{}).Items(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var found int
	for _, it := range items {
		if strings.HasPrefix(it.Key, "plugin:") {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("plugin items missing: %+v", items)
	}
	if _, err := (&StackService{}).Start(p.ID, "plugin:acme.x:mail"); err != confirmErr {
		t.Errorf("an unconfirmed plugin item must return its confirmation error, got %v", err)
	}
	if _, err := (&StackService{}).Start(p.ID, "plugin:acme.x:gone"); err == nil || !strings.Contains(err.Error(), "needs nope") {
		t.Errorf("a missing program must say so: %v", err)
	}
}
