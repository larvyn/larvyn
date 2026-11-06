package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"larvyn/internal/dotenv"
	"larvyn/internal/store"
)

// Found is a Laravel project found on disk by Discover.
type Found struct {
	Path string `json:"path"`
	Name string `json:"name"`
	// Added is true when the folder is already one of the user's projects.
	Added bool `json:"added"`
	// Modified is when composer.json last changed, in Unix milliseconds.
	Modified int64 `json:"modified"`
}

// How far and how long Discover looks. A Laravel app sits a few folders
// below home (~/code/client/app); deeper than that is rarely a project and
// costs a lot on big disks.
const (
	discoverDepth   = 5
	discoverTimeout = 6 * time.Second
	discoverMaxDirs = 60000
)

// skipDirs are folders never worth walking into.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "Library": true, "Applications": true,
	"AppData": true, "snap": true, "go": true, "Pictures": true, "Music": true,
	"Videos": true, "Movies": true, "OneDrive": true, "iCloud Drive": true,
	"$RECYCLE.BIN": true, "System Volume Information": true,
}

// pathKey is what makes two paths the same folder: case counts, except on
// Windows and macOS, whose file systems ignore it by default.
func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(p)
	}
	return p
}

// discoverRoots are the folders Discover starts from: the home folder plus
// where common local stacks keep their sites.
func discoverRoots() []string {
	var roots []string
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, home)
	}
	switch runtime.GOOS {
	case "windows":
		roots = append(roots, `C:\laragon\www`, `C:\xampp\htdocs`, `C:\wamp64\www`, `C:\wamp\www`, `C:\inetpub\wwwroot`)
	default:
		roots = append(roots, "/var/www", "/srv/www", "/srv/http", "/opt/lampp/htdocs")
	}
	out := roots[:0]
	for _, r := range roots {
		if st, err := os.Stat(r); err == nil && st.IsDir() {
			out = append(out, r)
		}
	}
	return out
}

// isLaravelDir reports whether dir holds a Laravel app: an artisan file and
// a composer.json that requires laravel/framework. It returns composer.json's
// modification time.
func isLaravelDir(dir string) (bool, time.Time) {
	if _, err := os.Stat(filepath.Join(dir, "artisan")); err != nil {
		return false, time.Time{}
	}
	path := filepath.Join(dir, "composer.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return false, time.Time{}
	}
	var c struct {
		Require map[string]string `json:"require"`
	}
	if json.Unmarshal(b, &c) != nil {
		return false, time.Time{}
	}
	if _, ok := c.Require["laravel/framework"]; !ok {
		return false, time.Time{}
	}
	var mod time.Time
	if st, err := os.Stat(path); err == nil {
		mod = st.ModTime()
	}
	return true, mod
}

// Discover looks for Laravel projects on this computer, under the home
// folder and the usual web roots, newest first. It stops looking after a
// few seconds, so on a very large disk the list may be partial.
func (s *ProjectService) Discover() []Found {
	return discover(discoverRoots(), discoverDepth, time.Now().Add(discoverTimeout))
}

func discover(roots []string, maxDepth int, deadline time.Time) []Found {
	added := map[string]bool{}
	for _, p := range store.Projects() {
		added[pathKey(p.Path)] = true
	}
	seen := map[string]bool{}
	var found []Found
	visited := 0

	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if visited >= discoverMaxDirs || time.Now().After(deadline) {
			return
		}
		visited++
		key := pathKey(dir)
		if seen[key] {
			return
		}
		seen[key] = true
		if ok, mod := isLaravelDir(dir); ok {
			// A git worktree (where .git is a file) is a checkout of a
			// project already listed, often one of Larvyn's own agent
			// worktrees; it would only clutter the list.
			if st, err := os.Stat(filepath.Join(dir, ".git")); err == nil && !st.IsDir() {
				return
			}
			f := Found{Path: dir, Name: prettyName(dir), Added: added[key], Modified: mod.UnixMilli()}
			if env := dotenv.Read(filepath.Join(dir, ".env")); env["APP_NAME"] != "" && env["APP_NAME"] != "Laravel" {
				f.Name = env["APP_NAME"]
			}
			found = append(found, f)
			return // a project's own folders are not projects
		}
		if depth >= maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			// Symlinks are not followed: they loop, and point back into
			// folders already walked.
			if !e.IsDir() || strings.HasPrefix(name, ".") || skipDirs[name] {
				continue
			}
			walk(filepath.Join(dir, name), depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Modified > found[j].Modified })
	return found
}
