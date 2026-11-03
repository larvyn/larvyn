package flock

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// fakeDaemon serves body at /v1/status on FLOCK_HOME's socket.
func fakeDaemon(t *testing.T, body string) string {
	t.Helper()
	home, err := os.MkdirTemp("", "flk") // short: unix socket paths are limited to ~100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("FLOCK_HOME", home)
	Forget()
	t.Cleanup(Forget)

	ln, err := net.Listen("unix", filepath.Join(home, "flockd.sock"))
	if err != nil {
		t.Skip("unix sockets not available:", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return home
}

func TestSiteFor(t *testing.T) {
	app := t.TempDir()
	wt := t.TempDir()
	fakeDaemon(t, `{"version":"1.0.0","sites":[
		{"name":"acme","domain":"acme.test","path":`+quote(app)+`,"php":"8.4","secured":true,"url":"https://acme.test"},
		{"name":"feature-auth.acme","domain":"feature-auth.acme.test","path":`+quote(wt)+`,"parent":"acme","branch":"feature/auth"}
	]}`)

	if v := Version(); v != "1.0.0" {
		t.Errorf("Version = %q", v)
	}
	s, ok := SiteFor(app)
	if !ok || s.URL != "https://acme.test" || s.PHP != "8.4" {
		t.Errorf("SiteFor(app) = %+v, %v", s, ok)
	}
	// A site without a URL in the status gets one from its domain.
	s, ok = SiteFor(wt)
	if !ok || s.URL != "http://feature-auth.acme.test" || s.Parent != "acme" {
		t.Errorf("SiteFor(worktree) = %+v, %v", s, ok)
	}
	if _, ok := SiteFor(t.TempDir()); ok {
		t.Error("SiteFor found a folder Flock does not serve")
	}
}

func TestNotRunning(t *testing.T) {
	t.Setenv("FLOCK_HOME", t.TempDir())
	Forget()
	t.Cleanup(Forget)
	if Version() != "" || Sites() != nil {
		t.Error("expected nothing without a daemon")
	}
	if _, ok := SiteFor("/anything"); ok {
		t.Error("SiteFor found a site without a daemon")
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
