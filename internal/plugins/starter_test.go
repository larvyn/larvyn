package plugins

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var starterDir = filepath.Join("..", "..", "examples", "plugin-starter")

// The starter is what people copy to build a view with Vite. Its manifest
// must install once it is built, and the page Vite builds (a module script
// in a relative assets/ folder) must be served the way a module needs.
func TestPluginStarterInstalls(t *testing.T) {
	reset(t)
	manifest, err := os.ReadFile(filepath.Join(starterDir, "larvyn.json"))
	if err != nil {
		t.Fatal(err)
	}
	// What `npm run build` writes, so the test needs no Node.
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "larvyn.json"), manifest, 0o644)
	os.MkdirAll(filepath.Join(src, "dist", "assets"), 0o755)
	os.WriteFile(filepath.Join(src, "dist", "index.html"), []byte(`<!doctype html><html><head><script type="module" crossorigin src="./assets/index-x.js"></script></head><body></body></html>`), 0o644)
	os.WriteFile(filepath.Join(src, "dist", "assets", "index-x.js"), []byte("console.log(1)"), 0o644)
	os.MkdirAll(filepath.Join(src, "node_modules", "vite"), 0o755)
	os.WriteFile(filepath.Join(src, "node_modules", "vite", "index.js"), []byte("x"), 0o644)

	s := &PluginService{}
	p, err := s.Inspect(src)
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Install(src, p.Manifest.Permissions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "node_modules")); !os.IsNotExist(err) {
		t.Error("node_modules must not be copied on install")
	}
	views := s.Views("")
	if len(views) != 1 {
		t.Fatalf("views: %+v", views)
	}
	if rec := get(views[0].URL); rec.Code != 200 || !strings.Contains(rec.Body.String(), "window.larvyn") {
		t.Fatalf("page: %d %.80s", rec.Code, rec.Body.String())
	}
	js := get(strings.TrimSuffix(views[0].URL, "index.html") + "assets/index-x.js")
	if js.Code != 200 || !strings.HasPrefix(js.Header().Get("Content-Type"), "text/javascript") || js.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("module script: %d %q %q", js.Code, js.Header().Get("Content-Type"), js.Header().Get("Access-Control-Allow-Origin"))
	}
}

// Every method a view can call must be in the starter's typed client, so
// it never falls behind the API.
func TestPluginStarterKnowsEveryMethod(t *testing.T) {
	src, err := os.ReadFile("view.go")
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout may have CRLF line endings.
	body := strings.ReplaceAll(string(src), "\r\n", "\n")
	start := strings.Index(body, "func (s *PluginService) Call(")
	if start < 0 {
		t.Fatal("PluginService.Call is not in view.go; did it move?")
	}
	end := strings.Index(body[start:], "\n}\n")
	if end < 0 {
		t.Fatal("could not find the end of PluginService.Call")
	}
	cases := regexp.MustCompile(`case ("[a-z.]+"(?:, "[a-z.]+")*):`).FindAllStringSubmatch(body[start:start+end], -1)

	client, err := os.ReadFile(filepath.Join(starterDir, "src", "larvyn.ts"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range cases {
		for _, m := range strings.Split(c[1], ", ") {
			n++
			if !strings.Contains(string(client), "'"+strings.Trim(m, `"`)+"': {") {
				t.Errorf("src/larvyn.ts has no type for %s", m)
			}
		}
	}
	if n < 8 {
		t.Fatalf("found only %d methods in Call; did it move?", n)
	}
}
