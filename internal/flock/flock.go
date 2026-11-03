// Package flock talks to Flock (flockphp.com), a local PHP environment that
// serves every project and every git worktree of it at a .test domain.
//
// Flock's daemon answers a small HTTP API on a unix socket in its data folder;
// GET /v1/status lists the sites it serves with their URL, path and PHP
// version, worktrees included. Larvyn only reads it: when Flock is not
// installed or not running, every lookup simply finds nothing.
package flock

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Site is a site Flock serves, as its status reports it.
type Site struct {
	Name    string `json:"name"`
	Domain  string `json:"domain"`
	Path    string `json:"path"`
	PHP     string `json:"php"`
	Secured bool   `json:"secured"`
	// Parent is the site a git worktree belongs to; Branch is its branch.
	Parent  string `json:"parent,omitempty"`
	Branch  string `json:"branch,omitempty"`
	URL     string `json:"url,omitempty"`
	Problem string `json:"problem,omitempty"`
}

type status struct {
	Version string `json:"version"`
	Sites   []Site `json:"sites"`
}

// Home is Flock's data folder: ~/Library/Application Support/Flock on macOS,
// %AppData%\Flock on Windows, $XDG_DATA_HOME/flock (~/.local/share/flock) on
// Linux. FLOCK_HOME overrides it, as it does for Flock itself.
func Home() string {
	if dir := os.Getenv("FLOCK_HOME"); dir != "" {
		return dir
	}
	if runtime.GOOS == "linux" {
		if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
			return filepath.Join(dir, "flock")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, ".local", "share", "flock")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "Flock")
}

// BinDir is the folder with Flock's shims (php, composer, node, flk…), which
// run the versions the current project uses.
func BinDir() string {
	if h := Home(); h != "" {
		return filepath.Join(h, "bin")
	}
	return ""
}

func socket() string {
	if h := Home(); h != "" {
		return filepath.Join(h, "flockd.sock")
	}
	return ""
}

// Installed reports whether Flock has been set up on this machine.
func Installed() bool {
	h := Home()
	if h == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(h, "bin"))
	return err == nil
}

// Status lookups are cached briefly: project inspection and the agents view
// ask often, and the daemon's answer only changes when sites do.
const cacheFor = 3 * time.Second

var (
	mu       sync.Mutex
	cached   status
	cachedOK bool
	cachedAt time.Time
)

// Version is the running daemon's version, "" when it is not running.
func Version() string {
	st, ok := load()
	if !ok {
		return ""
	}
	return st.Version
}

// Sites lists what the running daemon serves; nil when it is not running.
func Sites() []Site {
	st, _ := load()
	return st.Sites
}

// SiteFor finds the site Flock serves from dir: the project itself or one of
// its git worktrees.
func SiteFor(dir string) (Site, bool) {
	if dir == "" {
		return Site{}, false
	}
	for _, s := range Sites() {
		if samePath(s.Path, dir) {
			return s, true
		}
	}
	return Site{}, false
}

// Forget drops the cached status, so the next lookup asks the daemon again.
func Forget() {
	mu.Lock()
	cachedAt = time.Time{}
	mu.Unlock()
}

func load() (status, bool) {
	mu.Lock()
	defer mu.Unlock()
	if !cachedAt.IsZero() && time.Since(cachedAt) < cacheFor {
		return cached, cachedOK
	}
	cached, cachedOK = fetch(socket())
	cachedAt = time.Now()
	return cached, cachedOK
}

// fetch asks the daemon on sock for its status.
func fetch(sock string) (status, bool) {
	if sock == "" {
		return status{}, false
	}
	if _, err := os.Stat(sock); err != nil {
		return status{}, false
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
		},
	}
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://flockd/v1/status")
	if err != nil {
		return status{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return status{}, false
	}
	var st status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return status{}, false
	}
	for i := range st.Sites {
		if st.Sites[i].URL == "" && st.Sites[i].Domain != "" {
			st.Sites[i].URL = scheme(st.Sites[i].Secured) + "://" + st.Sites[i].Domain
		}
	}
	return st, true
}

func scheme(secure bool) string {
	if secure {
		return "https"
	}
	return "http"
}

// samePath is proc.SamePath; proc imports this package for BinDir, so it
// cannot be used here.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
