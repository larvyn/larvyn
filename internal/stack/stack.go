package stack

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"larvyn/internal/flock"
	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// StackItem is one dev process a Laravel project usually runs.
type StackItem struct {
	Key         string                `json:"key"`
	Label       string                `json:"label"`
	Description string                `json:"description"`
	Argv        []string              `json:"argv"`
	Available   bool                  `json:"available"`
	Reason      string                `json:"reason"`
	URL         string                `json:"url"`
	Session     *terminal.SessionInfo `json:"session"`
	// Dev is set for the processes `php artisan dev` would start (from dev:list).
	Dev bool `json:"dev"`
	// ServedBy names the local server (Flock) that already does this item's
	// job, so it needs no process: the HTTP server when Flock serves the app.
	ServedBy string `json:"servedBy"`
	// Plugin names the plugin that added the item; "" for Larvyn's own.
	Plugin string `json:"plugin"`
	// NeedConfirm is returned by Start until the user confirmed a plugin
	// item's command line.
	NeedConfirm error `json:"-"`
	// Guard starts a plugin item through the plugin host's gate, which
	// checks again, in step with the plugin being turned off, that it may run.
	Guard func(start func() (terminal.SessionInfo, error)) (terminal.SessionInfo, error) `json:"-"`
}

// devStackItems turns the app's dev:list into stack items. The well-known
// ones keep Larvyn's keys and labels (and serve keeps the project's own port,
// so several projects can run side by side); the rest use their dev:list name.
func devStackItems(procs []DevProcess, serve, vite StackItem) []StackItem {
	items := []StackItem{}
	for _, d := range procs {
		args, plain := splitCommand(d.Command)
		argv := proc.ShellCommand(d.Command)
		if plain {
			argv = args
			if argv[0] == "php" {
				argv[0] = store.PHP()
			}
		}
		it := StackItem{Key: d.Name, Label: titleCase(d.Name), Description: d.Command, Argv: argv, Available: true, Dev: true}
		artisanCmd := ""
		if plain && len(args) > 2 && args[0] == "php" && args[1] == "artisan" {
			artisanCmd = args[2]
		}
		switch {
		case artisanCmd == "serve":
			it = serve
			it.Argv = append(append([]string{}, serve.Argv...), withoutHostPort(args[3:])...)
			it.Dev = true
		case d.Name == "vite" || (plain && isNodeCommand(args) && len(args) == 3 && args[2] == "dev"):
			it.Key, it.Label, it.Description, it.URL = "vite", vite.Label, d.Command+", hot module reload", ""
		case artisanCmd == "horizon":
			it.Key, it.Label = "queue", "Horizon"
		case artisanCmd == "queue:listen" || artisanCmd == "queue:work":
			it.Key, it.Label = "queue", "Queue worker"
		case artisanCmd == "pail":
			it.Key, it.Label = "pail", "Pail"
		case artisanCmd == "schedule:work":
			it.Key, it.Label = "schedule", "Scheduler"
		case artisanCmd == "reverb:start":
			it.Key, it.Label = "reverb", "Reverb"
		}
		items = append(items, it)
	}
	return items
}

func withoutHostPort(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "--host") && !strings.HasPrefix(a, "--port") {
			out = append(out, a)
		}
	}
	return out
}

func isNodeCommand(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(argv[0]), filepath.Ext(argv[0])))
	switch name {
	case "npm", "pnpm", "yarn", "bun", "npx", "bunx":
		return true
	}
	return false
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type StackService struct{}

// Facts is what the stack needs to know about a project's files.
type Facts struct {
	IsLaravel      bool
	HasVendor      bool
	HasNodeModules bool
	NpmScripts     map[string]string
	// Packages are the Composer package names, e.g. laravel/horizon.
	Packages []string
}

// Inspect reads a project's Facts. main sets it from the project service's
// inspection, so this package does not depend on it.
var Inspect = func(p store.Project) Facts { return Facts{} }

// PluginItems returns the stack items of the plugins that are on. main sets
// it from the plugin host; their keys start with "plugin:".
var PluginItems = func(p store.Project) []StackItem { return nil }

func stackItems(p store.Project) []StackItem {
	items, _ := stackItemsFrom(p)
	return items
}

// stackItemsFrom is stackItems, also telling whether the app's own dev:list
// chose the processes (fromDevList), or Larvyn's defaults did.
func stackItemsFrom(p store.Project) (items []StackItem, fromDevList bool) {
	info := Inspect(p)
	has := func(name string) bool {
		for _, pkg := range info.Packages {
			if pkg == name {
				return true
			}
		}
		return false
	}
	port := p.Port
	if port == 0 {
		port = 8000
	}
	pm := proc.PackageManager(p.Path)
	_, hasDev := info.NpmScripts["dev"]
	serve := StackItem{Key: "serve", Label: "HTTP server", Description: fmt.Sprintf("php artisan serve on :%d", port),
		Argv:      []string{store.PHP(), "artisan", "serve", "--host=127.0.0.1", fmt.Sprintf("--port=%d", port)},
		Available: info.IsLaravel, URL: fmt.Sprintf("http://127.0.0.1:%d", port)}
	vite := StackItem{Key: "vite", Label: "Vite", Description: pm + " run dev, hot module reload",
		Argv: []string{pm, "run", "dev"}, Available: hasDev}

	if info.IsLaravel && info.HasVendor {
		if procs, ok := devProcesses(p); ok {
			items, fromDevList = devStackItems(procs, serve, vite), true
		}
	}
	if items == nil {
		items = []StackItem{serve, vite}
	}
	present := map[string]bool{}
	for _, it := range items {
		present[it.Key] = true
	}
	add := func(it StackItem) {
		if !present[it.Key] {
			items = append(items, it)
			present[it.Key] = true
		}
	}
	if has("laravel/horizon") {
		add(StackItem{Key: "queue", Label: "Horizon", Description: "Redis queues with a dashboard",
			Argv: []string{store.PHP(), "artisan", "horizon"}, Available: true, URL: fmt.Sprintf("http://127.0.0.1:%d/horizon", port)})
	} else {
		add(StackItem{Key: "queue", Label: "Queue worker", Description: "queue:listen, reloads on code change",
			Argv: []string{store.PHP(), "artisan", "queue:listen", "--tries=1"}, Available: info.IsLaravel})
	}
	add(StackItem{Key: "schedule", Label: "Scheduler", Description: "schedule:work, runs due tasks every minute",
		Argv: []string{store.PHP(), "artisan", "schedule:work"}, Available: info.IsLaravel})
	if has("laravel/pail") {
		add(StackItem{Key: "pail", Label: "Pail", Description: "live log tail",
			Argv: []string{store.PHP(), "artisan", "pail", "--timeout=0"}, Available: true})
	}
	if has("laravel/reverb") {
		add(StackItem{Key: "reverb", Label: "Reverb", Description: "WebSocket server",
			Argv: []string{store.PHP(), "artisan", "reverb:start", "--debug"}, Available: true})
	}
	if has("laravel/octane") {
		add(StackItem{Key: "octane", Label: "Octane", Description: "high-performance server with --watch",
			Argv: []string{store.PHP(), "artisan", "octane:start", "--watch", fmt.Sprintf("--port=%d", port+100)}, Available: true,
			URL: fmt.Sprintf("http://127.0.0.1:%d", port+100)})
	}
	for _, it := range PluginItems(p) {
		add(it)
	}
	// The project's own setup only matters for Larvyn's items: a plugin's
	// php or node program may not use the app's dependencies, and its
	// missing programs are reported by the plugin host.
	if !info.HasVendor {
		for i := range items {
			if items[i].Plugin == "" && items[i].Argv[0] == store.PHP() {
				items[i].Available = false
				items[i].Reason = "run composer install first"
			}
		}
	}
	if !info.HasNodeModules {
		for i := range items {
			if items[i].Plugin == "" && isNodeCommand(items[i].Argv) {
				items[i].Available = false
				items[i].Reason = "run " + pm + " install first"
			}
		}
	}
	if site, ok := flock.SiteFor(p.Path); ok && site.Problem == "" {
		for i := range items {
			if items[i].Key == "serve" {
				// Flock serves the app at its .test domain all the time:
				// artisan serve would only add a second copy on another URL.
				items[i].ServedBy, items[i].URL, items[i].Dev = "Flock", site.URL, false
				items[i].Description = "Served by Flock at " + site.Domain
			}
		}
	}
	for i := range items {
		items[i].Session = terminal.Find(p.ID, "service", items[i].Key)
	}
	return items, fromDevList
}

// Items lists the project's stack with what is running.
func (s *StackService) Items(projectID string) ([]StackItem, error) {
	p, ok := store.FindProject(projectID)
	if !ok {
		return nil, errors.New("project not found")
	}
	return stackItems(p), nil
}

// Start runs a stack item (no-op when it already runs).
func (s *StackService) Start(projectID, key string) (terminal.SessionInfo, error) {
	p, ok := store.FindProject(projectID)
	if !ok {
		return terminal.SessionInfo{}, errors.New("project not found")
	}
	for _, it := range stackItems(p) {
		if it.Key != key {
			continue
		}
		if it.Session != nil {
			if it.Session.Running {
				return *it.Session, nil
			}
			_ = terminal.Sessions.Close(it.Session.ID)
		}
		if !it.Available {
			return terminal.SessionInfo{}, errors.New(proc.FirstNonEmpty(it.Reason, it.Label+" is not available here"))
		}
		if it.NeedConfirm != nil {
			return terminal.SessionInfo{}, it.NeedConfirm
		}
		start := func() (terminal.SessionInfo, error) {
			return terminal.Sessions.Start(terminal.SessionSpec{ProjectID: p.ID, Kind: "service", Key: key, Title: it.Label,
				Cwd: p.Path, Argv: it.Argv, Meta: map[string]string{"url": it.URL}})
		}
		if it.Guard != nil {
			return it.Guard(start)
		}
		return start()
	}
	return terminal.SessionInfo{}, errors.New("unknown stack item " + key)
}

// Stop stops a stack item.
func (s *StackService) Stop(projectID, key string) error {
	if sess := terminal.Find(projectID, "service", key); sess != nil {
		return terminal.Sessions.Kill(sess.ID)
	}
	return nil
}

// Restart stops and starts a stack item.
func (s *StackService) Restart(projectID, key string) (terminal.SessionInfo, error) {
	// A plugin item whose command line changed must be confirmed before the
	// running one is stopped, or a declined prompt would leave it stopped.
	if p, ok := store.FindProject(projectID); ok {
		for _, it := range stackItems(p) {
			if it.Key == key && it.NeedConfirm != nil {
				return terminal.SessionInfo{}, it.NeedConfirm
			}
		}
	}
	if sess := terminal.Find(projectID, "service", key); sess != nil {
		_ = terminal.Sessions.Kill(sess.ID)
		// A program that ignores SIGTERM is killed after about 3 s.
		for i := 0; i < 80; i++ {
			if cur := terminal.Find(projectID, "service", key); cur == nil || !cur.Running {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return s.Start(projectID, key)
}

// StartAll starts the given services, or by default what `php artisan dev`
// would run (server, vite and queue on apps without dev:list), leaving out
// what a local server like Flock already does.
func (s *StackService) StartAll(projectID string, keys []string) error {
	if len(keys) == 0 {
		p, ok := store.FindProject(projectID)
		if !ok {
			return errors.New("project not found")
		}
		items, fromDevList := stackItemsFrom(p)
		for _, it := range items {
			if it.Dev {
				keys = append(keys, it.Key)
			}
		}
		// dev:list chose the processes: when Flock covers all of them (just
		// serve), nothing is left to start, which is not the same as having
		// no dev:list. The defaults would start a queue worker the app never
		// asked for.
		if len(keys) == 0 && !fromDevList {
			for _, it := range items {
				if (it.Key == "serve" || it.Key == "vite" || it.Key == "queue") && it.ServedBy == "" {
					keys = append(keys, it.Key)
				}
			}
		}
	}
	var errs []error
	for _, k := range keys {
		if _, err := s.Start(projectID, k); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", k, err))
		}
	}
	return errors.Join(errs...)
}

// StopAll stops every service of a project.
func (s *StackService) StopAll(projectID string) {
	for _, sess := range terminal.Sessions.List() {
		if sess.ProjectID == projectID && sess.Kind == "service" && sess.Running {
			_ = terminal.Sessions.Kill(sess.ID)
		}
	}
}
