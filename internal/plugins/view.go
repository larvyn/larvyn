package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"larvyn/internal/dotenv"
	"larvyn/internal/store"
)

// Tier 1: plugin views. A view is an HTML page from the plugin's folder,
// shown in a sandboxed iframe (allow-scripts only, so it gets an opaque
// origin) and served under ViewPrefix with a strict Content-Security-Policy:
// it can load its own files and inline code, and nothing else. It has no
// network, no forms and no frames. The only way out is window.larvyn,
// which posts a message to Larvyn's page; that forwards it to Call, and Call
// checks the plugin's permissions in Go before doing anything.

// ViewPrefix is the URL path plugin files are served under:
// ViewPrefix + <plugin id> + "/" + <file>.
const ViewPrefix = "/plugin-view/"

// viewCSP keeps a view to its own files. The sandbox directive applies the
// iframe sandbox even if the page is opened some other way.
const viewCSP = "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; media-src 'self' blob:; connect-src 'none'; " +
	"form-action 'none'; frame-src 'none'; child-src 'none'; object-src 'none'; base-uri 'none'; sandbox allow-scripts"

// viewTypes are the content types of the files a view uses. They are fixed
// here because mime.TypeByExtension reads the OS database (the registry on
// Windows), which can map .js to text/plain; with nosniff the browser would
// then refuse to run it.
var viewTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8", ".json": "application/json", ".map": "application/json",
	".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".ico": "image/x-icon",
	".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf",
	".mp3": "audio/mpeg", ".mp4": "video/mp4", ".webm": "video/webm", ".wasm": "application/wasm",
	".txt": "text/plain; charset=utf-8",
}

// PluginView is a view for the sidebar.
type PluginView struct {
	PluginID   string `json:"pluginId"`
	PluginName string `json:"pluginName"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	// URL is where the iframe loads the view from.
	URL string `json:"url"`
}

// Views lists the views of the plugins that are on.
func (s *PluginService) Views() []PluginView {
	var out []PluginView
	for _, p := range Active() {
		for _, v := range p.Manifest.Contributes.Views {
			out = append(out, PluginView{PluginID: p.Manifest.ID, PluginName: p.Manifest.Name, ID: v.ID, Title: v.Title,
				URL: ViewPrefix + p.Manifest.ID + "/" + path.Clean(v.Entry)})
		}
	}
	return out
}

// ViewMiddleware serves plugin files and keeps plugin pages away from
// Wails' own endpoints. Desktop Wails accepts /wails/runtime calls (which
// reach every bound service) from any page in the webview, so a request
// from an opaque origin, which is what a sandboxed view sends, is refused.
func ViewMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, ViewPrefix):
			serveView(w, r)
		// Larvyn's page calls the runtime with POST only (fetch in
		// @wailsio/runtime). A GET can be made by any page with an <img> or
		// a link, without Origin or Referer, and Wails would still read the
		// call from its query string.
		case r.URL.Path == "/wails/runtime" && r.Method != http.MethodPost:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		case strings.HasPrefix(r.URL.Path, "/wails/") && fromPluginView(r):
			http.Error(w, "plugin views cannot call Larvyn directly", http.StatusForbidden)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// fromPluginView reports whether r came from a sandboxed plugin view: its
// opaque origin shows as Origin: null and makes every request cross-site.
// Larvyn's own page is always same-origin.
func fromPluginView(r *http.Request) bool {
	if r.Header.Get("Origin") == "null" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return true
	}
	if ref, err := url.Parse(r.Header.Get("Referer")); err == nil && strings.HasPrefix(ref.Path, ViewPrefix) {
		return true
	}
	return false
}

// serveView serves one file of a plugin that is on, from inside its folder.
func serveView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, rel, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, ViewPrefix), "/")
	p, err := find(id)
	if err != nil || !p.Enabled {
		http.NotFound(w, r)
		return
	}
	file, err := pluginFile(p.Dir, rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctype, ok := viewTypes[strings.ToLower(filepath.Ext(file))]
	if !ok {
		ctype = "application/octet-stream"
	}
	if strings.HasSuffix(file, ".html") {
		data = injectBridge(data)
		ctype = "text/html; charset=utf-8"
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Content-Security-Policy", viewCSP)
	h.Set("X-Content-Type-Options", "nosniff")
	// The sandboxed iframe has an opaque origin, and module scripts and
	// fonts are fetched in CORS mode even from the view's own folder. These
	// are a plugin's own static files, and views can't fetch (connect-src
	// 'none'), so any origin may read them.
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// pluginFile resolves rel inside dir, following symlinks, and refuses
// anything that ends up outside it.
func pluginFile(dir, rel string) (string, error) {
	clean := path.Clean("/" + rel)[1:]
	if clean == "" || strings.Contains(rel, `\`) {
		return "", errors.New("bad path")
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(clean)))
	if err != nil {
		return "", err
	}
	if r, err := filepath.Rel(root, real); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", errors.New("outside the plugin")
	}
	if fi, err := os.Stat(real); err != nil || !fi.Mode().IsRegular() {
		return "", errors.New("not a file")
	}
	return real, nil
}

// bridgeJS is window.larvyn, put at the top of every view page. call posts
// to Larvyn's page, which knows the view by its iframe, never by anything
// the page says about itself.
const bridgeJS = `<script>(function () {
  var next = 0, waiting = {};
  window.addEventListener('message', function (e) {
    if (e.source !== window.parent) return;
    var m = e.data;
    if (!m || m.larvyn !== 1 || !waiting[m.id]) return;
    var w = waiting[m.id]; delete waiting[m.id];
    if (m.error) w.reject(new Error(m.error)); else w.resolve(m.result);
  });
  window.larvyn = Object.freeze({
    call: function (method, params) {
      return new Promise(function (resolve, reject) {
        var id = ++next;
        waiting[id] = { resolve: resolve, reject: reject };
        window.parent.postMessage({ larvyn: 1, id: id, method: String(method), params: params || {} }, '*');
      });
    }
  });
})();</script>`

// injectBridge puts bridgeJS before everything else in the page.
func injectBridge(page []byte) []byte {
	lower := asciiLower(page)
	for _, tag := range [][]byte{[]byte("<head>"), []byte("<html>")} {
		if i := bytes.Index(lower, tag); i >= 0 {
			at := i + len(tag)
			return append(append(append([]byte{}, page[:at]...), bridgeJS...), page[at:]...)
		}
	}
	if i := bytes.Index(lower, []byte("<html")); i >= 0 {
		if j := bytes.IndexByte(page[i:], '>'); j >= 0 {
			at := i + j + 1
			return append(append(append([]byte{}, page[:at]...), bridgeJS...), page[at:]...)
		}
	}
	return append([]byte(bridgeJS), page...)
}

// asciiLower lowercases A-Z only, so its indexes match page byte for byte
// (bytes.ToLower can change lengths on non-ASCII or invalid UTF-8).
func asciiLower(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return out
}

// ViewResult is what a view's call returns. Notify asks the frontend to
// show a notification for the plugin (ui.notify).
type ViewResult struct {
	Result any    `json:"result"`
	Notify string `json:"notify,omitempty"`
}

// maxStorage caps what a plugin keeps through storage.set.
const maxStorage = 1 << 20

// fetchClient is what network.fetch uses; tests replace it.
var fetchClient = &http.Client{Timeout: 10 * time.Second}

const maxFetchBody = 2 << 20

// Call runs a window.larvyn call from one of a plugin's views, for the
// project open in Larvyn. Every method checks its permission first.
func (s *PluginService) Call(pluginID, projectID, method string, params map[string]any) (ViewResult, error) {
	p, err := find(pluginID)
	if err != nil {
		return ViewResult{}, err
	}
	str := func(k string) string { v, _ := params[k].(string); return v }
	need := func(perm string) error { return allow(p, perm) }

	switch method {
	case "project.info":
		if err := need("project.read"); err != nil {
			return ViewResult{}, err
		}
		proj, ok := store.FindProject(projectID)
		if !ok {
			return ViewResult{}, errors.New("no project is open")
		}
		return ViewResult{Result: map[string]string{"id": proj.ID, "name": proj.Name, "path": proj.Path}}, nil

	case "project.env.read":
		if err := need("project.env.read"); err != nil {
			return ViewResult{}, err
		}
		proj, ok := store.FindProject(projectID)
		if !ok {
			return ViewResult{}, errors.New("no project is open")
		}
		return ViewResult{Result: dotenv.Read(filepath.Join(proj.Path, ".env"))}, nil

	case "ui.notify":
		if err := need("ui.notify"); err != nil {
			return ViewResult{}, err
		}
		msg := strings.TrimSpace(str("message"))
		if msg == "" {
			return ViewResult{}, errors.New("ui.notify needs a message")
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return ViewResult{Result: true, Notify: msg}, nil

	case "storage.get", "storage.set":
		if err := need("storage"); err != nil {
			return ViewResult{}, err
		}
		return storageCall(pluginID, method, str("key"), params["value"])

	case "network.fetch":
		if err := need("network"); err != nil {
			return ViewResult{}, err
		}
		return fetch(p, str("url"))
	}
	return ViewResult{}, fmt.Errorf("unknown method %q", method)
}

// storageFile keeps a plugin's data outside its folder, so updates keep it.
func storageFile(pluginID string) string {
	return filepath.Join(store.HomeDir(), "plugin-data", pluginID+".json")
}

// storageMu makes each storage call's read-modify-write one step, so two
// calls at once can't drop each other's keys.
var storageMu sync.Mutex

func storageCall(pluginID, method, key string, value any) (ViewResult, error) {
	if key == "" {
		return ViewResult{}, errors.New(method + " needs a key")
	}
	storageMu.Lock()
	defer storageMu.Unlock()
	data := map[string]any{}
	if b, err := os.ReadFile(storageFile(pluginID)); err == nil {
		// A damaged file is reported, not silently replaced with only the
		// key being set.
		if err := json.Unmarshal(b, &data); err != nil {
			return ViewResult{}, fmt.Errorf("the plugin's storage is damaged: %w", err)
		}
	}
	if method == "storage.get" {
		return ViewResult{Result: data[key]}, nil
	}
	if value == nil {
		delete(data, key)
	} else {
		data[key] = value
	}
	b, err := json.Marshal(data)
	if err != nil {
		return ViewResult{}, err
	}
	if len(b) > maxStorage {
		return ViewResult{}, fmt.Errorf("storage is limited to %d KB per plugin", maxStorage>>10)
	}
	if err := os.MkdirAll(filepath.Dir(storageFile(pluginID)), 0o700); err != nil {
		return ViewResult{}, err
	}
	// Written to a temporary file and renamed, so a reader never sees half.
	tmp, err := os.CreateTemp(filepath.Dir(storageFile(pluginID)), "."+pluginID+"-*")
	if err != nil {
		return ViewResult{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return ViewResult{}, err
	}
	// Flushed before the rename, so a crash can't leave an empty file in
	// place of the old one.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return ViewResult{}, err
	}
	if err := tmp.Close(); err != nil {
		return ViewResult{}, err
	}
	return ViewResult{Result: true}, os.Rename(tmp.Name(), storageFile(pluginID))
}

// hostAllowed reports whether u's host[:port] is one the manifest lists.
func hostAllowed(m Manifest, u *url.URL) bool {
	host := strings.ToLower(u.Host)
	name := strings.ToLower(u.Hostname())
	for _, n := range m.Network {
		n = strings.ToLower(n)
		if strings.HasPrefix(n, "*.") {
			base := n[1:] // ".example.com"
			if !strings.Contains(n, ":") && u.Port() == "" && strings.HasSuffix(name, base) {
				return true
			}
			continue
		}
		if strings.Contains(n, ":") {
			if host == n {
				return true
			}
		} else if u.Port() == "" && name == n {
			return true
		}
	}
	return false
}

// fetch is network.fetch: a GET to a host the plugin lists, made by Larvyn.
// Redirects are followed only to listed hosts.
func fetch(p Plugin, raw string) (ViewResult, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ViewResult{}, fmt.Errorf("network.fetch needs an http or https URL, got %q", raw)
	}
	if !hostAllowed(p.Manifest, u) {
		return ViewResult{}, fmt.Errorf("%s is not in the plugin's network list", u.Host)
	}
	client := *fetchClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !hostAllowed(p.Manifest, req.URL) {
			return fmt.Errorf("redirect to %s is not allowed", req.URL.Host)
		}
		return nil
	}
	resp, err := client.Get(u.String())
	if err != nil {
		return ViewResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBody+1))
	if err != nil {
		return ViewResult{}, err
	}
	if len(body) > maxFetchBody {
		return ViewResult{}, fmt.Errorf("the response is larger than %d MB", maxFetchBody>>20)
	}
	return ViewResult{Result: map[string]any{"status": resp.StatusCode, "contentType": resp.Header.Get("Content-Type"), "body": string(body)}}, nil
}
