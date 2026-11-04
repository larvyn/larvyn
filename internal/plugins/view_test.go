package plugins

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"larvyn/internal/store"
)

// viewManifest has a view and asks for everything views can use.
func viewManifest(t *testing.T, network ...string) string {
	return edit(t, func(m map[string]any) {
		perms := []string{"project.read", "terminal.run", "project.env.read", "ui.notify", "storage"}
		if len(network) > 0 {
			perms = append(perms, "network")
			m["network"] = network
		}
		m["permissions"] = perms
	})
}

func installView(t *testing.T, manifest string) (*PluginService, store.Project) {
	t.Helper()
	reset(t)
	src := t.TempDir()
	writePlugin(t, src, manifest)
	os.WriteFile(filepath.Join(src, "ui", "app.js"), []byte("console.log(1)"), 0o644)
	s := &PluginService{}
	p, err := s.Inspect(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Install(src, p.Manifest.Permissions); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_NAME=Shop\nDB_PASSWORD=secret\n"), 0o644)
	proj := store.Project{ID: store.NewID(), Name: "shop", Path: dir}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, proj) })
	t.Cleanup(func() {
		store.Update(func(d *store.State) { d.Projects = nil })
		os.RemoveAll(filepath.Join(store.HomeDir(), "plugin-data"))
	})
	return s, proj
}

func get(path string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("app")) })
	ViewMiddleware(next).ServeHTTP(rec, req)
	return rec
}

func TestServeView(t *testing.T) {
	s, _ := installView(t, viewManifest(t))
	views := s.Views()
	if len(views) != 1 || views[0].URL != "/plugin-view/acme.horizon/ui/index.html" {
		t.Fatalf("views: %+v", views)
	}
	rec := get(views[0].URL)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"connect-src 'none'", "sandbox allow-scripts", "frame-src 'none'", "form-action 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP lacks %q: %s", want, csp)
		}
	}
	if body := rec.Body.String(); !strings.HasPrefix(body, "<script>") || !strings.Contains(body, "window.larvyn") {
		t.Errorf("the bridge must be injected first: %.80s", body)
	}
	if rec := get("/plugin-view/acme.horizon/ui/app.js"); rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Errorf("js: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get("/index.html"); rec.Body.String() != "app" {
		t.Error("other paths go to the app")
	}
}

func TestServeViewRefuses(t *testing.T) {
	s, _ := installView(t, viewManifest(t))
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("secret"), 0o644)
	os.Symlink(outside, filepath.Join(Dir(), "acme.horizon", "ui", "link.txt"))
	for _, path := range []string{
		"/plugin-view/acme.horizon/../../state.json",
		"/plugin-view/acme.horizon/ui/link.txt",
		"/plugin-view/acme.horizon/",
		"/plugin-view/acme.horizon/ui",
		"/plugin-view/acme.nope/ui/index.html",
		`/plugin-view/acme.horizon/ui\index.html`,
	} {
		if rec := get(path); rec.Code == 200 {
			t.Errorf("%s was served: %.60s", path, rec.Body.String())
		}
	}
	s.Disable("acme.horizon")
	if rec := get("/plugin-view/acme.horizon/ui/index.html"); rec.Code == 200 {
		t.Error("a plugin that is off must not be served")
	}
}

func TestViewsCannotReachWailsRuntime(t *testing.T) {
	// A header-less GET, as an <img src> in a plugin page would send.
	if rec := get("/wails/runtime?object=0&method=0&args={}"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("a GET to /wails/runtime must be refused: %d", rec.Code)
	}
	if rec := get("/wails/eventpayload/x", "Sec-Fetch-Site", "cross-site"); rec.Code != http.StatusForbidden {
		t.Errorf("a cross-site request to /wails/ must be refused: %d", rec.Code)
	}
	post := httptest.NewRequest("POST", "/wails/runtime", strings.NewReader("{}"))
	post.Header.Set("Origin", "wails://localhost")
	post.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	ViewMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("app")) })).ServeHTTP(rec, post)
	if rec.Body.String() != "app" {
		t.Errorf("Larvyn's own POST must reach the runtime: %d", rec.Code)
	}
	for _, h := range [][]string{{"Origin", "null"}, {"Referer", "wails://localhost/plugin-view/acme.x/ui/index.html"}, {"Sec-Fetch-Site", "cross-site"}} {
		req := httptest.NewRequest("POST", "/wails/runtime", strings.NewReader("{}"))
		req.Header.Set(h[0], h[1])
		rec := httptest.NewRecorder()
		ViewMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("app")) })).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("a POST with %v reached /wails/runtime: %d", h, rec.Code)
		}
	}
	if rec := get("/wails/custom.js", "Sec-Fetch-Site", "same-origin"); rec.Body.String() != "app" {
		t.Error("Larvyn's own page must still reach /wails/ files")
	}
}

func TestInjectBridge(t *testing.T) {
	for in, at := range map[string]string{
		"<!doctype html><html><head><title>x</title></head>": "<head><script>",
		"<HTML lang=en><body>x":                              "<HTML lang=en><script>",
		"<p>bare":                                            "<script>",
	} {
		if got := string(injectBridge([]byte(in))); !strings.Contains(got, at) || !strings.Contains(got, "window.larvyn") {
			t.Errorf("injectBridge(%q) = %.80s", in, got)
		}
	}
}

func TestCallChecksPermissions(t *testing.T) {
	s, proj := installView(t, viewManifest(t))
	r, err := s.Call("acme.horizon", proj.ID, "project.info", nil)
	if err != nil || r.Result.(map[string]string)["name"] != "shop" {
		t.Fatalf("project.info: %+v %v", r, err)
	}
	r, err = s.Call("acme.horizon", proj.ID, "project.env.read", nil)
	if err != nil || r.Result.(map[string]string)["DB_PASSWORD"] != "secret" {
		t.Fatalf("env: %+v %v", r, err)
	}
	if r, err := s.Call("acme.horizon", proj.ID, "ui.notify", map[string]any{"message": "hi"}); err != nil || r.Notify != "hi" {
		t.Errorf("notify: %+v %v", r, err)
	}
	if _, err := s.Call("acme.horizon", proj.ID, "network.fetch", map[string]any{"url": "http://localhost:8025/"}); err == nil {
		t.Error("network.fetch without the network permission must be refused")
	}
	if _, err := s.Call("acme.horizon", proj.ID, "fs.read", nil); err == nil || !strings.Contains(err.Error(), "unknown method") {
		t.Errorf("unknown method: %v", err)
	}

	// A plugin that asks for less gets less.
	_, proj2 := installView(t, edit(t, func(m map[string]any) { m["permissions"] = []string{"terminal.run"} }))
	for _, method := range []string{"project.info", "project.env.read", "ui.notify", "storage.get"} {
		if _, err := s.Call("acme.horizon", proj2.ID, method, map[string]any{"key": "k", "message": "m"}); err == nil || !strings.Contains(err.Error(), "may not") {
			t.Errorf("%s without its permission: %v", method, err)
		}
	}
}

func TestStorageConcurrent(t *testing.T) {
	s, proj := installView(t, viewManifest(t))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Call("acme.horizon", proj.ID, "storage.set", map[string]any{"key": fmt.Sprint("k", i), "value": i})
		}(i)
	}
	wg.Wait()
	for i := 0; i < 20; i++ {
		if r, _ := s.Call("acme.horizon", proj.ID, "storage.get", map[string]any{"key": fmt.Sprint("k", i)}); r.Result == nil {
			t.Errorf("k%d was lost to a concurrent set", i)
		}
	}
	os.WriteFile(storageFile("acme.horizon"), []byte("{broken"), 0o600)
	if _, err := s.Call("acme.horizon", proj.ID, "storage.set", map[string]any{"key": "x", "value": 1}); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("a damaged file must be reported, not overwritten: %v", err)
	}
}

func TestStorage(t *testing.T) {
	s, proj := installView(t, viewManifest(t))
	if _, err := s.Call("acme.horizon", proj.ID, "storage.set", map[string]any{"key": "tab", "value": "inbox"}); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Call("acme.horizon", proj.ID, "storage.get", map[string]any{"key": "tab"})
	if r.Result != "inbox" {
		t.Errorf("get: %+v", r)
	}
	big := strings.Repeat("x", maxStorage)
	if _, err := s.Call("acme.horizon", proj.ID, "storage.set", map[string]any{"key": "big", "value": big}); err == nil {
		t.Error("storage over the limit must fail")
	}
	s.Call("acme.horizon", proj.ID, "storage.set", map[string]any{"key": "tab", "value": nil})
	if r, _ := s.Call("acme.horizon", proj.ID, "storage.get", map[string]any{"key": "tab"}); r.Result != nil {
		t.Errorf("a nil value deletes: %+v", r)
	}
}

func TestNetworkFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/away" {
			http.Redirect(w, r, "http://example.invalid/x", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"messages":[]}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://") // 127.0.0.1:port
	s, proj := installView(t, viewManifest(t, host))

	r, err := s.Call("acme.horizon", proj.ID, "network.fetch", map[string]any{"url": srv.URL + "/api"})
	if err != nil {
		t.Fatal(err)
	}
	if res := r.Result.(map[string]any); res["status"] != 200 || res["body"] != `{"messages":[]}` {
		t.Errorf("fetch: %+v", res)
	}
	for _, bad := range []string{"http://127.0.0.1:1/", "file:///etc/passwd", "http://user:pw@" + host + "/", "ftp://" + host} {
		if _, err := s.Call("acme.horizon", proj.ID, "network.fetch", map[string]any{"url": bad}); err == nil {
			t.Errorf("%s must be refused", bad)
		}
	}
	if _, err := s.Call("acme.horizon", proj.ID, "network.fetch", map[string]any{"url": srv.URL + "/away"}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("a redirect to an unlisted host must fail: %v", err)
	}
}

func TestHostAllowed(t *testing.T) {
	m := Manifest{Network: []string{"api.example.com", "*.cdn.example.com", "localhost:8025"}}
	cases := map[string]bool{
		"https://api.example.com/x":      true,
		"https://api.example.com:8443/x": false,
		"https://a.cdn.example.com/x":    true,
		"https://cdn.example.com/x":      false,
		"https://evilexample.com/x":      false,
		"http://localhost:8025/api":      true,
		"http://localhost:8026/api":      false,
		"http://localhost/api":           false,
	}
	for raw, want := range cases {
		u, _ := url.Parse(raw)
		if got := hostAllowed(m, u); got != want {
			t.Errorf("hostAllowed(%s) = %v, want %v", raw, got, want)
		}
	}
}
