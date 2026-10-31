package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
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
	items := []StackItem{
		{Key: "serve", Label: "HTTP server", Description: fmt.Sprintf("php artisan serve on :%d", port),
			Argv:      []string{store.PHP(), "artisan", "serve", "--host=127.0.0.1", fmt.Sprintf("--port=%d", port)},
			Available: info.IsLaravel, URL: fmt.Sprintf("http://127.0.0.1:%d", port)},
		{Key: "vite", Label: "Vite", Description: pm + " run dev, hot module reload",
			Argv: []string{pm, "run", "dev"}, Available: hasDev},
	}
	if has("laravel/horizon") {
		items = append(items, StackItem{Key: "queue", Label: "Horizon", Description: "Redis queues with a dashboard",
			Argv: []string{store.PHP(), "artisan", "horizon"}, Available: true, URL: fmt.Sprintf("http://127.0.0.1:%d/horizon", port)})
	} else {
		items = append(items, StackItem{Key: "queue", Label: "Queue worker", Description: "queue:listen, reloads on code change",
			Argv: []string{store.PHP(), "artisan", "queue:listen", "--tries=1"}, Available: info.IsLaravel})
	}
	items = append(items, StackItem{Key: "schedule", Label: "Scheduler", Description: "schedule:work, runs due tasks every minute",
		Argv: []string{store.PHP(), "artisan", "schedule:work"}, Available: info.IsLaravel})
	if has("laravel/pail") {
		items = append(items, StackItem{Key: "pail", Label: "Pail", Description: "live log tail",
			Argv: []string{store.PHP(), "artisan", "pail", "--timeout=0"}, Available: true})
	}
	if has("laravel/reverb") {
		items = append(items, StackItem{Key: "reverb", Label: "Reverb", Description: "WebSocket server",
			Argv: []string{store.PHP(), "artisan", "reverb:start", "--debug"}, Available: true})
	}
	if has("laravel/octane") {
		items = append(items, StackItem{Key: "octane", Label: "Octane", Description: "high-performance server with --watch",
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
	if hasDev && !info.HasNodeModules {
		items[1].Available = false
		items[1].Reason = "run " + pm + " install first"
	}
	for i := range items {
		items[i].Session = terminals.find(p.ID, "service", items[i].Key)
	}
	return items
}

// Items lists the project's stack with what is running.
func (s *StackService) Items(projectID string) ([]StackItem, error) {
	p, ok := store.Project(projectID)
	if !ok {
		return nil, errors.New("project not found")
	}
	return stackItems(p), nil
}

// Start runs a stack item (no-op when it already runs).
func (s *StackService) Start(projectID, key string) (SessionInfo, error) {
	p, ok := store.Project(projectID)
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
			return SessionInfo{}, errors.New(firstNonEmpty(it.Reason, it.Label+" is not available here"))
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

// StartAll starts the usual dev set: server, vite, queue.
func (s *StackService) StartAll(projectID string, keys []string) error {
	if len(keys) == 0 {
		keys = []string{"serve", "vite", "queue"}
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
