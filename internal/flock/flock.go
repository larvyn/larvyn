// Package flock talks to Flock (flockphp.com), an optional local PHP
// environment that serves every project and every git worktree of it at a
// .test domain.
//
// Reads go to the daemon's HTTP API on a unix socket in Flock's data folder
// (GET /v1/status lists the sites with their URL, path and PHP version,
// worktrees included). Changes to a site go through its documented CLI, flk,
// so Flock applies them exactly as when the user types them. When Flock is
// not installed or not running, every lookup simply finds nothing.
package flock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
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
	// PHPSource is where the PHP version comes from: "isolated" (set for
	// the site in Flock), "project" (pinned in .flockrc) or "default".
	PHPSource string `json:"php_source"`
	// Parked sites come from a parked folder; only linked ones can be unlinked.
	Parked bool `json:"parked"`
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

// Bin finds Flock's command, flk: in its shim folder once it is set up, else
// where its package put it. It is flk, not flock, because on Linux
// /usr/bin/flock is util-linux's file lock. "" when Flock is not installed.
func Bin() string {
	name := "flk"
	if runtime.GOOS == "windows" {
		name = "flk.exe"
	}
	candidates := []string{filepath.Join(BinDir(), name)}
	if runtime.GOOS == "darwin" {
		// Before its setup, the CLI is only inside the app, named flock.
		candidates = append(candidates, "/Applications/Flock.app/Contents/MacOS/flock")
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// States of Flock on this machine, from State.
const (
	NotInstalled = ""          // no flk anywhere
	NotSetUp     = "installed" // the package is there, `flk install` has not run
	Stopped      = "stopped"   // set up, but the daemon does not answer
	Running      = "running"
)

// State tells how far Flock is on this machine, so Larvyn can offer the next step.
func State() string {
	if _, ok := load(); ok {
		return Running
	}
	if st, err := os.Stat(BinDir()); err == nil && st.IsDir() {
		return Stopped
	}
	if Bin() != "" {
		return NotSetUp
	}
	return NotInstalled
}

// Link serves dir as <folder>.test (`flk link` in it) and returns the site
// once the daemon reports it.
func Link(dir string) (Site, error) {
	if err := run(dir, "link"); err != nil {
		return Site{}, err
	}
	for range 20 {
		Forget()
		if s, ok := SiteFor(dir); ok {
			return s, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return Site{}, errors.New("Flock linked the folder but does not serve it yet: see `flk sites`")
}

// Start starts (or restarts) the daemon. It needs no password: Flock's setup
// lets the user manage their own daemon.
func Start() error {
	err := run("", "restart")
	Forget()
	return err
}

// run runs flk with args in dir and turns a failure into its output.
func run(dir string, args ...string) error {
	bin := Bin()
	if bin == "" {
		return errors.New("Flock is not installed")
	}
	// A flk that waits on a stuck daemon must not hang the action (or the
	// auto-serve goroutine) for good.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
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
	var st status
	if err := call(sock, http.MethodGet, "/v1/status", &st, 2*time.Second); err != nil {
		return status{}, false
	}
	for i := range st.Sites {
		if st.Sites[i].URL == "" && st.Sites[i].Domain != "" {
			st.Sites[i].URL = scheme(st.Sites[i].Secured) + "://" + st.Sites[i].Domain
		}
	}
	return st, true
}

var errNotRunning = errors.New("Flock is not running")

// call makes one request to the daemon's API on sock and decodes the JSON
// answer into out (when not nil). Errors carry the daemon's message.
func call(sock, method, path string, out any, timeout time.Duration) error {
	if sock == "" {
		return errNotRunning
	}
	if _, err := os.Stat(sock); err != nil {
		return errNotRunning
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
		},
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequest(method, "http://flockd"+path, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return errNotRunning
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("Flock answered %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
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

// Unlink stops serving a linked site.
func Unlink(site string) error { defer Forget(); return run("", "unlink", site) }

// Secure serves a site over HTTPS with Flock's trusted certificate (on) or
// over plain HTTP.
func Secure(site string, on bool) error {
	defer Forget()
	if on {
		return run("", "secure", site)
	}
	return run("", "unsecure", site)
}

// UsePHP makes the site use a PHP minor version on this machine (flk
// isolate), or the project's / Flock's default again when minor is "".
func UsePHP(site, minor string) error {
	defer Forget()
	if minor == "" {
		return run("", "unisolate", "php", "--site", site)
	}
	return run("", "isolate", "php", minor, "--site", site)
}

// PHPVersion is a PHP minor version Flock can serve.
type PHPVersion struct {
	Minor     string  `json:"minor"`
	Installed string  `json:"installed"` // full version, "" when not installed
	Default   bool    `json:"default"`
	Job       *PHPJob `json:"job"` // an install that runs or failed
}

// PHPJob is the progress of a PHP install.
type PHPJob struct {
	State    string  `json:"state"` // running | failed
	Phase    string  `json:"phase"` // downloading | installing
	Progress float64 `json:"progress"`
	Error    string  `json:"error"`
}

// PHPVersions lists the PHP versions that are installed or can be.
func PHPVersions() ([]PHPVersion, error) {
	var out struct {
		Versions []PHPVersion `json:"versions"`
	}
	err := call(socket(), http.MethodGet, "/v1/php/versions", &out, 3*time.Second)
	return out.Versions, err
}

// InstallPHP downloads a PHP minor version and waits until it is installed.
func InstallPHP(minor string) error {
	if err := call(socket(), http.MethodPost, "/v1/php/versions/"+minor+"/install", nil, 30*time.Second); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		versions, err := PHPVersions()
		if err != nil {
			return err
		}
		for _, v := range versions {
			if v.Minor != minor {
				continue
			}
			if v.Job != nil && v.Job.State == "failed" {
				return fmt.Errorf("installing PHP %s failed: %s", minor, v.Job.Error)
			}
			if v.Installed != "" && v.Job == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("installing PHP %s did not finish in time", minor)
}
