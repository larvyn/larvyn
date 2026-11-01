package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"larvyn/internal/git"
	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/updater"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type SystemService struct{}

// Tool is an external program Larvyn uses.
type Tool struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Hint    string `json:"hint"`
}

var versionRe = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

// Tools detects PHP, Composer, Node and friends.
func (s *SystemService) Tools() []Tool {
	specs := []struct {
		id, name, bin string
		args          []string
		hint          string
	}{
		{"php", "PHP", store.PHP(), []string{"-r", "echo PHP_VERSION;"}, "Install PHP 8.2+ or Laravel Herd"},
		{"composer", "Composer", "composer", []string{"--version"}, "getcomposer.org"},
		{"laravel", "Laravel installer", "laravel", []string{"--version"}, "composer global require laravel/installer"},
		{"node", "Node.js", "node", []string{"--version"}, "nodejs.org"},
		{"npm", "npm", "npm", []string{"--version"}, ""},
		{"pnpm", "pnpm", "pnpm", []string{"--version"}, ""},
		{"bun", "Bun", "bun", []string{"--version"}, ""},
		{"git", "Git", "git", []string{"--version"}, "git-scm.com"},
		{"herd", "Laravel Herd", "herd", []string{"--version"}, "herd.laravel.com"},
		{"docker", "Docker", "docker", []string{"--version"}, ""},
		{"mysql", "MySQL client", "mysql", []string{"--version"}, ""},
		{"redis", "Redis CLI", "redis-cli", []string{"--version"}, ""},
	}
	out := make([]Tool, len(specs))
	var wg sync.WaitGroup
	for i, sp := range specs {
		wg.Add(1)
		go func(i int, id, name, bin string, args []string, hint string) {
			defer wg.Done()
			t := Tool{ID: id, Name: name, Hint: hint, Path: proc.Which(bin)}
			cmd := []string{t.Path}
			if id == "composer" && t.Path == "" {
				if c := composerArgv(); c != nil {
					t.Path, cmd = c[len(c)-1], c
				}
			}
			if t.Path != "" {
				if res, err := proc.Run("", 8*time.Second, nil, cmd[0], append(cmd[1:], args...)...); err == nil {
					t.Version = versionRe.FindString(res.Stdout + res.Stderr)
				}
			}
			out[i] = t
		}(i, sp.id, sp.name, sp.bin, sp.args, sp.hint)
	}
	wg.Wait()
	return out
}

// Settings returns the user's settings.
func (s *SystemService) Settings() Settings { return store.CurrentSettings() }

// SaveSettings stores the user's settings.
func (s *SystemService) SaveSettings(v Settings) error {
	return store.Update(func(d *store.State) { d.Settings = v })
}

// Platform reports the OS for keyboard hints.
func (s *SystemService) Platform() string { return runtime.GOOS }

// LaunchOptions come from the environment so scripts can open Larvyn on a
// view, with a command running in a terminal, or with an agent started.
type LaunchOptions struct {
	View  string `json:"view"`  // LARVYN_VIEW
	Run   string `json:"run"`   // LARVYN_RUN: shell line for a new terminal
	Agent string `json:"agent"` // LARVYN_AGENT: agent id to start
	// LARVYN_PALETTE opens the command palette with this text typed ("1" for empty).
	Palette string `json:"palette"`
	// LARVYN_FILE opens this project-relative file in the Files editor.
	File string `json:"file"`
}

// Launch returns the launch options for the active project.
func (s *SystemService) Launch() LaunchOptions {
	return LaunchOptions{View: os.Getenv("LARVYN_VIEW"), Run: os.Getenv("LARVYN_RUN"), Agent: os.Getenv("LARVYN_AGENT"), Palette: os.Getenv("LARVYN_PALETTE"), File: os.Getenv("LARVYN_FILE")}
}

// Home returns Larvyn's state folder.
func (s *SystemService) Home() string { return store.HomeDir() }

var editors = map[string][]string{
	"code":     {"code", "-g", "{file}:{line}"},
	"cursor":   {"cursor", "-g", "{file}:{line}"},
	"windsurf": {"windsurf", "-g", "{file}:{line}"},
	"zed":      {"zed", "{file}:{line}"},
	"phpstorm": {"phpstorm", "--line", "{line}", "{file}"},
	"sublime":  {"subl", "{file}:{line}"},
	"nvim":     {"nvim", "+{line}", "{file}"},
}

// OpenInEditor opens a file (or folder) at a line in the configured editor.
func (s *SystemService) OpenInEditor(path string, line int) error {
	st := store.CurrentSettings()
	if line <= 0 {
		line = 1
	}
	var tmpl []string
	if st.Editor == "custom" && st.EditorCommand != "" {
		tmpl = strings.Fields(st.EditorCommand)
	} else if t, ok := editors[st.Editor]; ok {
		tmpl = t
	} else {
		tmpl = editors["code"]
	}
	if isDir(path) {
		tmpl = []string{tmpl[0], "{file}"}
	}
	args := make([]string, 0, len(tmpl))
	for _, a := range tmpl[1:] {
		a = strings.ReplaceAll(a, "{file}", path)
		a = strings.ReplaceAll(a, "{line}", fmt.Sprint(line))
		args = append(args, a)
	}
	bin := proc.Which(tmpl[0])
	if bin == "" {
		return errors.New(tmpl[0] + " was not found on PATH; pick another editor in Settings")
	}
	return proc.StartDetached(filepath.Dir(path), bin, args...)
}

// OpenFolder reveals a folder in the file manager.
func (s *SystemService) OpenFolder(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

// OpenURL opens a link in the default browser.
func (s *SystemService) OpenURL(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return errors.New("only http(s) links can be opened")
	}
	return application.Get().Browser.OpenURL(url)
}

// CreateRequest describes a new Laravel application.
type CreateRequest struct {
	Parent  string `json:"parent"`
	Name    string `json:"name"`
	Starter string `json:"starter"` // none, react, vue, livewire
	Testing string `json:"testing"` // pest, phpunit
	DB      string `json:"db"`
	// CreateSQLite asks for database/database.sqlite (and migrations) once
	// the app is scaffolded, when DB is sqlite.
	CreateSQLite bool `json:"createSqlite"`
}

// CreateProject scaffolds a new Laravel app in a terminal and registers it.
func (s *SystemService) CreateProject(req CreateRequest) (map[string]any, error) {
	name := git.Slug(req.Name)
	if req.Parent == "" || name == "" {
		return nil, errors.New("pick a folder and a name")
	}
	target := filepath.Join(req.Parent, name)
	if _, err := os.Stat(target); err == nil {
		return nil, errors.New(target + " already exists")
	}
	var argv []string
	if proc.Which("laravel") != "" {
		argv = []string{"laravel", "new", name, "--no-interaction", "--git"}
		switch req.Starter {
		case "react", "vue", "livewire":
			argv = append(argv, "--"+req.Starter)
		}
		if req.Testing == "phpunit" {
			argv = append(argv, "--phpunit")
		} else {
			argv = append(argv, "--pest")
		}
		if req.DB != "" {
			argv = append(argv, "--database="+req.DB)
		}
		if req.Starter != "" && req.Starter != "none" {
			argv = append(argv, "--npm")
		}
	} else if composer := composerArgv(); composer != nil {
		// Without the installer, create.php does what `laravel new` would:
		// starter kit, database, git, Pest and npm.
		argv = createFallbackArgv(req, name, composer)
	} else {
		return nil, errors.New("install Composer first (Settings → Toolchain)")
	}
	meta := map[string]string{"target": target}
	if req.CreateSQLite && (req.DB == "" || req.DB == "sqlite") {
		meta["sqlite"] = "1"
	}
	sess, err := terminals.Start(SessionSpec{Kind: "run", Key: "create", Title: "laravel new " + name, Cwd: req.Parent, Argv: argv,
		Meta: meta})
	if err != nil {
		return nil, err
	}
	return map[string]any{"session": sess, "path": target}, nil
}

// createFallbackArgv runs the embedded create.php with the dialog's choices.
func createFallbackArgv(req CreateRequest, name string, composer []string) []string {
	opts, _ := json.Marshal(map[string]any{
		"parent": req.Parent, "name": name, "starter": req.Starter, "testing": req.Testing, "db": req.DB, "composer": composer,
	})
	return []string{store.PHP(), createScript(), base64.StdEncoding.EncodeToString(opts)}
}

// Log appends a frontend error to larvyn.log in the state folder, so crashes
// in the window leave a trace.
func (s *SystemService) Log(level, message string) {
	store.AppendLog(level, message)
}

// CheckForUpdate looks up the newest release (betas included) and compares it
// with this build.
func (s *SystemService) CheckForUpdate() (UpdateInfo, error) { return updater.Check() }

// InstallUpdate downloads tag, verifies it against the release checksums,
// replaces this installation and restarts Larvyn.
func (s *SystemService) InstallUpdate(tag string) error {
	if err := updater.Install(tag); err != nil {
		return err
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		application.Get().Quit()
	}()
	return nil
}
