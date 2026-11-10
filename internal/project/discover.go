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

// packageExts are macOS bundles: folders Finder shows as one file. Never a
// project, and some (the Photos and Music libraries) are privacy-guarded.
var packageExts = map[string]bool{
	".app": true, ".bundle": true, ".framework": true, ".photoslibrary": true,
	".photolibrary": true, ".musiclibrary": true, ".tvlibrary": true,
	".aplibrary": true, ".imovielibrary": true, ".fcpbundle": true,
	".logicx": true, ".band": true, ".xcarchive": true, ".sparsebundle": true,
}

// sameFolder reports whether two paths name the same folder. The file
// system decides, so case counts exactly where it does (Linux, and
// case-sensitive volumes on macOS) and symlinked paths match. A folder that
// no longer exists is compared by its path, ignoring case on Windows.
func sameFolder(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	sa, errA := os.Stat(a)
	sb, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return runtime.GOOS == "windows" && strings.EqualFold(a, b)
	}
	return os.SameFile(sa, sb)
}

// discoverRoots are the folders Discover starts from: the folders Herd and
// Valet serve sites from (walked first: that is where the apps are), the
// home folder, and where common local stacks keep their sites. A root
// inside a guarded folder is left out before it is even looked at.
func discoverRoots(home string, guarded map[string]bool) []string {
	roots := stackSitePaths(home)
	if home != "" {
		roots = append(roots, home)
	}
	switch runtime.GOOS {
	case "windows":
		roots = append(roots, `C:\laragon\www`, `C:\xampp\htdocs`, `C:\wamp64\www`, `C:\wamp\www`, `C:\inetpub\wwwroot`)
	default:
		roots = append(roots, "/var/www", "/srv/www", "/srv/http", "/opt/lampp/htdocs")
	}
	var out []string
	for _, r := range roots {
		if isGuarded(r, guarded) {
			continue
		}
		if st, err := os.Stat(r); err == nil && st.IsDir() {
			out = append(out, r)
		}
	}
	return out
}

// stackSitePaths are the folders Laravel Herd and Valet serve sites from
// ("herd park", "valet park"), read from their config files.
func stackSitePaths(home string) []string {
	if home == "" {
		return nil
	}
	configs := []string{
		filepath.Join(home, ".config", "valet", "config.json"),
		filepath.Join(home, ".valet", "config.json"),
	}
	switch runtime.GOOS {
	case "darwin":
		configs = append(configs, filepath.Join(home, "Library", "Application Support", "Herd", "config", "valet", "config.json"))
	case "windows":
		configs = append(configs, filepath.Join(home, ".config", "herd", "config", "valet", "config.json"))
	}
	var paths []string
	for _, c := range configs {
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		var cfg struct {
			Paths []string `json:"paths"`
		}
		if json.Unmarshal(b, &cfg) != nil {
			continue
		}
		for _, p := range cfg.Paths {
			if p = strings.TrimSpace(p); p != "" && filepath.IsAbs(p) {
				paths = append(paths, filepath.Clean(p))
			}
		}
	}
	return paths
}

// within reports whether path is dir or inside it, ignoring case where the
// file system usually does (macOS, Windows).
func within(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		path, dir = strings.ToLower(path), strings.ToLower(dir)
	}
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// isGuarded reports whether path is a guarded folder or inside one.
func isGuarded(path string, guarded map[string]bool) bool {
	for g := range guarded {
		if within(path, g) {
			return true
		}
	}
	return false
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

// Discovery is what Discover found.
type Discovery struct {
	Projects []Found `json:"projects"`
	// Narrowed is true when folders that may hold projects were left out
	// to avoid a macOS permission prompt (Desktop, Documents, Downloads);
	// Discover(true) looks in them too.
	Narrowed bool `json:"narrowed"`
}

// Discover looks for Laravel projects on this computer, under the folders
// Herd and Valet serve, the home folder and the usual web roots, newest
// first. It stops looking after a few seconds, so on a very large disk the
// list may be partial.
//
// On macOS it stays out of the privacy-guarded folders (Desktop, Documents,
// Photos, network volumes…) unless a project already lives there, so
// opening the picker never raises a permission prompt. widen asks for
// Desktop, Documents and Downloads too, and keeps asking for them from then
// on: the user chose to, and macOS asks only once.
func (s *ProjectService) Discover(widen bool) Discovery {
	if widen {
		_ = store.Update(func(d *store.State) { d.DiscoverEverywhere = true })
	}
	home, _ := os.UserHomeDir()
	var added []string
	for _, p := range store.Projects() {
		added = append(added, p.Path)
	}
	guarded, narrowed := guardedDirs(home, added, store.DiscoverEverywhere())
	return Discovery{
		Projects: discover(discoverRoots(home, guarded), guarded, discoverDepth, time.Now().Add(discoverTimeout)),
		Narrowed: narrowed,
	}
}

// discover walks roots, never entering a folder in guarded.
func discover(roots []string, guarded map[string]bool, maxDepth int, deadline time.Time) []Found {
	// The folders already added, to mark them: by path, and by identity
	// for a path spelled another way (case, symlinks).
	added := map[string]bool{}
	var addedInfo []os.FileInfo
	for _, p := range store.Projects() {
		added[filepath.Clean(p.Path)] = true
		if st, err := os.Stat(p.Path); err == nil {
			addedInfo = append(addedInfo, st)
		}
	}
	isAdded := func(dir string) bool {
		if added[dir] {
			return true
		}
		st, err := os.Stat(dir)
		if err != nil {
			return false
		}
		for _, a := range addedInfo {
			if os.SameFile(st, a) {
				return true
			}
		}
		return false
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
		key := filepath.Clean(dir)
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
			f := Found{Path: dir, Name: prettyName(dir), Added: isAdded(key), Modified: mod.UnixMilli()}
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
			if !e.IsDir() || strings.HasPrefix(name, ".") || skipDirs[name] || packageExts[strings.ToLower(filepath.Ext(name))] {
				continue
			}
			child := filepath.Join(dir, name)
			if guarded[child] {
				continue
			}
			walk(child, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Modified > found[j].Modified })
	return found
}
