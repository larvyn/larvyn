package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"larvyn/internal/proc"
	"larvyn/internal/store"
)

// StackItem is one dev process a Laravel project usually runs.
type StackItem struct {
	Key         string       `json:"key"`
	Label       string       `json:"label"`
	Description string       `json:"description"`
	Argv        []string     `json:"argv"`
	Available   bool         `json:"available"`
	Reason      string       `json:"reason"`
	URL         string       `json:"url"`
	Session     *SessionInfo `json:"session"`
	// Dev is set for the processes `php artisan dev` would start (from dev:list).
	Dev bool `json:"dev"`
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

func packageManager(root string) string {
	for file, pm := range map[string]string{"pnpm-lock.yaml": "pnpm", "yarn.lock": "yarn", "bun.lockb": "bun", "bun.lock": "bun"} {
		if _, err := os.Stat(filepath.Join(root, file)); err == nil {
			return pm
		}
	}
	return "npm"
}

func stackItems(p Project) []StackItem {
	info := inspect(p)
	has := func(name string) bool {
		for _, pkg := range info.Packages {
			if pkg.Name == name {
				return true
			}
		}
		return false
	}
	port := p.Port
	if port == 0 {
		port = 8000
	}
	pm := packageManager(p.Path)
	_, hasDev := info.NpmScripts["dev"]
	serve := StackItem{Key: "serve", Label: "HTTP server", Description: fmt.Sprintf("php artisan serve on :%d", port),
		Argv:      []string{store.PHP(), "artisan", "serve", "--host=127.0.0.1", fmt.Sprintf("--port=%d", port)},
		Available: info.IsLaravel, URL: fmt.Sprintf("http://127.0.0.1:%d", port)}
	vite := StackItem{Key: "vite", Label: "Vite", Description: pm + " run dev, hot module reload",
		Argv: []string{pm, "run", "dev"}, Available: hasDev}

	var items []StackItem
	if info.IsLaravel && info.HasVendor {
		if procs, ok := devProcesses(p); ok {
			items = devStackItems(procs, serve, vite)
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
	if !info.HasVendor {
		for i := range items {
			if items[i].Argv[0] == store.PHP() {
				items[i].Available = false
				items[i].Reason = "run composer install first"
			}
		}
	}
	if !info.HasNodeModules {
		for i := range items {
			if isNodeCommand(items[i].Argv) {
				items[i].Available = false
				items[i].Reason = "run " + pm + " install first"
			}
		}
	}
	for i := range items {
		items[i].Session = terminals.find(p.ID, "service", items[i].Key)
	}
	return items
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
func (s *StackService) Start(projectID, key string) (SessionInfo, error) {
	p, ok := store.FindProject(projectID)
	if !ok {
		return SessionInfo{}, errors.New("project not found")
	}
	for _, it := range stackItems(p) {
		if it.Key != key {
			continue
		}
		if it.Session != nil {
			if it.Session.Running {
				return *it.Session, nil
			}
			_ = terminals.Close(it.Session.ID)
		}
		if !it.Available {
			return SessionInfo{}, errors.New(proc.FirstNonEmpty(it.Reason, it.Label+" is not available here"))
		}
		return terminals.Start(SessionSpec{ProjectID: p.ID, Kind: "service", Key: key, Title: it.Label,
			Cwd: p.Path, Argv: it.Argv, Meta: map[string]string{"url": it.URL}})
	}
	return SessionInfo{}, errors.New("unknown stack item " + key)
}

// Stop stops a stack item.
func (s *StackService) Stop(projectID, key string) error {
	if sess := terminals.find(projectID, "service", key); sess != nil {
		return terminals.Kill(sess.ID)
	}
	return nil
}

// Restart stops and starts a stack item.
func (s *StackService) Restart(projectID, key string) (SessionInfo, error) {
	if sess := terminals.find(projectID, "service", key); sess != nil {
		_ = terminals.Kill(sess.ID)
		for i := 0; i < 40; i++ {
			if cur := terminals.find(projectID, "service", key); cur == nil || !cur.Running {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return s.Start(projectID, key)
}

// StartAll starts the given services, or by default what `php artisan dev`
// would run (server, vite and queue on apps without dev:list).
func (s *StackService) StartAll(projectID string, keys []string) error {
	if len(keys) == 0 {
		p, ok := store.FindProject(projectID)
		if !ok {
			return errors.New("project not found")
		}
		for _, it := range stackItems(p) {
			if it.Dev {
				keys = append(keys, it.Key)
			}
		}
		if len(keys) == 0 {
			keys = []string{"serve", "vite", "queue"}
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
	for _, sess := range terminals.List() {
		if sess.ProjectID == projectID && sess.Kind == "service" && sess.Running {
			_ = terminals.Kill(sess.ID)
		}
	}
}
