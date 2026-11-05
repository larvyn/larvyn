package system

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

	"larvyn/internal/composer"
	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
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
	// Outdated explains why this version should be updated; empty when fine.
	Outdated string `json:"outdated"`
}

// minLaravelInstaller is the first laravel/installer that writes a valid
// APP_URL for Laravel 13: older ones turn the skeleton's
// http://localhost:8000 into http://localhost:8000:8000, and every artisan
// command then fails with "Invalid URI: Host is malformed".
const minLaravelInstaller = "5.25.2"

// installerOutdated returns why a laravel/installer version must be updated,
// or "" when it is fine or unknown.
func installerOutdated(version string) string {
	if version == "" || !versionLess(version, minLaravelInstaller) {
		return ""
	}
	return "Laravel installer " + version + " writes a broken APP_URL (http://localhost:8000:8000) in new Laravel 13 apps. Update to " + minLaravelInstaller + " or newer."
}

// laravelInstallerVersion runs `laravel --version`; "" when it is missing or
// prints no version.
func laravelInstallerVersion() string {
	bin := proc.Which("laravel")
	if bin == "" {
		return ""
	}
	res, err := proc.Run("", 8*time.Second, nil, bin, "--version")
	if err != nil {
		return ""
	}
	return versionRe.FindString(res.Stdout + res.Stderr)
}

// LaravelInstaller reports the global laravel/installer for the New app
// dialog: where it is, its version and whether it must be updated.
func (s *SystemService) LaravelInstaller() Tool {
	t := Tool{ID: "laravel", Name: "Laravel installer", Path: proc.Which("laravel"), Hint: "composer global require laravel/installer"}
	t.Version = laravelInstallerVersion()
	t.Outdated = installerOutdated(t.Version)
	return t
}

// versionLess compares dotted numeric versions: 5.15.0 < 5.25.2 < 5.100.
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			fmt.Sscan(pa[i], &x)
		}
		if i < len(pb) {
			fmt.Sscan(pb[i], &y)
		}
		if x != y {
			return x < y
		}
	}
	return false
}

var versionRe = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

// Tools detects PHP, Composer, Node and friends.
func (s *SystemService) Tools() []Tool {
	specs := []struct {
		id, name, bin string
		args          []string
		hint          string
	}{
		{"php", "PHP", store.PHP(), []string{"-r", "echo PHP_VERSION;"}, "Install PHP 8.2+, Flock or Laravel Herd"},
		{"composer", "Composer", "composer", []string{"--version"}, "getcomposer.org"},
		{"laravel", "Laravel installer", "laravel", []string{"--version"}, "composer global require laravel/installer"},
		{"node", "Node.js", "node", []string{"--version"}, "nodejs.org"},
		{"npm", "npm", "npm", []string{"--version"}, ""},
		{"pnpm", "pnpm", "pnpm", []string{"--version"}, ""},
		{"bun", "Bun", "bun", []string{"--version"}, ""},
		{"git", "Git", "git", []string{"--version"}, "git-scm.com"},
		{"herd", "Laravel Herd", "herd", []string{"--version"}, "herd.laravel.com"},
		// flk, not flock: on Linux /usr/bin/flock is util-linux's file lock.
		{"flock", "Flock", "flk", []string{"version"}, "flockphp.com"},
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
				if c := composer.Argv(); c != nil {
					t.Path, cmd = c[len(c)-1], c
				}
			}
			if t.Path != "" {
				if res, err := proc.Run("", 8*time.Second, nil, cmd[0], append(cmd[1:], args...)...); err == nil {
					t.Version = versionRe.FindString(res.Stdout + res.Stderr)
				}
			}
			if id == "laravel" {
				t.Outdated = installerOutdated(t.Version)
			}
			out[i] = t
		}(i, sp.id, sp.name, sp.bin, sp.args, sp.hint)
	}
	wg.Wait()
	return out
}

// Settings returns the user's settings.
func (s *SystemService) Settings() store.Settings { return store.CurrentSettings() }

// SaveSettings stores the user's settings.
func (s *SystemService) SaveSettings(v store.Settings) error {
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
	"nvim":     {"nvim", "+{line}", "--", "{file}"},
}

// OpenInEditor opens a file (or folder) at a line in the configured editor.
func (s *SystemService) OpenInEditor(path string, line int) error {
	// Paths come from logs, stack traces and artisan output. An absolute path
	// can't be read as an option or a command (nvim runs "+cmd" arguments).
	if !filepath.IsAbs(path) {
		return fmt.Errorf("can't open %q: not an absolute path", path)
	}
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
		if tmpl[0] == "nvim" {
			tmpl = []string{"nvim", "--", "{file}"}
		}
	}
	args := make([]string, 0, len(tmpl))
	for _, a := range tmpl[1:] {
		a = strings.ReplaceAll(a, "{file}", path)
		a = strings.ReplaceAll(a, "{line}", fmt.Sprint(line))
		args = append(args, a)
	}
	bin := findEditor(tmpl[0])
	if bin == "" {
		return errors.New(editorNotFound(tmpl[0]))
	}
	return proc.StartDetached(filepath.Dir(path), bin, args...)
}

// macEditorBundles maps an editor's command to the same CLI inside its app
// bundle. On macOS the command is only on PATH when the user installs it by
// hand, so a GUI launch usually can't find it.
var macEditorBundles = map[string]string{
	"code":     "Visual Studio Code.app/Contents/Resources/app/bin/code",
	"cursor":   "Cursor.app/Contents/Resources/app/bin/cursor",
	"windsurf": "Windsurf.app/Contents/Resources/app/bin/windsurf",
	"zed":      "Zed.app/Contents/MacOS/cli",
	"phpstorm": "PhpStorm.app/Contents/MacOS/phpstorm",
	"subl":     "Sublime Text.app/Contents/SharedSupport/bin/subl",
}

// editorInstallHints tells the user how to put an editor's command on PATH.
var editorInstallHints = map[string]string{
	"code":     `in VS Code run "Shell Command: Install 'code' command in PATH" (⌘⇧P)`,
	"cursor":   `in Cursor run "Shell Command: Install 'cursor' command in PATH" (⌘⇧P)`,
	"windsurf": `in Windsurf run "Shell Command: Install 'windsurf' command in PATH" (⌘⇧P)`,
	"zed":      `in Zed run "cli: install" from the command palette`,
	"phpstorm": `in PhpStorm use Tools → Create Command-line Launcher, or turn on shell scripts in JetBrains Toolbox`,
	"subl":     `link "/Applications/Sublime Text.app/Contents/SharedSupport/bin/subl" into a folder on your PATH`,
}

// macAppDirs are the folders searched for app bundles.
var macAppDirs = func() []string {
	dirs := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return dirs
}

// findEditor finds an editor's command on PATH and, on macOS, inside its app
// bundle in /Applications or ~/Applications. It returns "" when neither has it.
func findEditor(name string) string {
	if bin := proc.Which(name); bin != "" {
		return bin
	}
	if runtime.GOOS == "darwin" {
		return bundledEditor(name, macAppDirs())
	}
	return ""
}

// bundledEditor returns the first executable copy of name's bundled CLI in dirs.
func bundledEditor(name string, dirs []string) string {
	rel, ok := macEditorBundles[filepath.Base(name)]
	if !ok {
		return ""
	}
	for _, d := range dirs {
		p := filepath.Join(d, filepath.FromSlash(rel))
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

func editorNotFound(name string) string {
	msg := name + " was not found on PATH"
	if runtime.GOOS == "darwin" {
		if _, ok := macEditorBundles[name]; ok {
			msg += " or in /Applications"
		}
	}
	if hint, ok := editorInstallHints[name]; ok {
		msg += "; " + hint + ", or pick another editor in Settings"
	} else {
		msg += "; pick another editor in Settings"
	}
	return msg
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
	name := proc.Slug(req.Name)
	if req.Parent == "" || name == "" {
		return nil, errors.New("pick a folder and a name")
	}
	target := filepath.Join(req.Parent, name)
	if _, err := os.Stat(target); err == nil {
		return nil, errors.New(target + " already exists")
	}
	var argv []string
	// An installer older than minLaravelInstaller breaks APP_URL, so the
	// Composer route (which mirrors `laravel new`) is used instead.
	if proc.Which("laravel") != "" && installerOutdated(laravelInstallerVersion()) == "" {
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
	} else if composer := composer.Argv(); composer != nil {
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
	sess, err := terminal.Sessions.Start(terminal.SessionSpec{Kind: "run", Key: "create", Title: "laravel new " + name, Cwd: req.Parent, Argv: argv,
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
func (s *SystemService) CheckForUpdate() (updater.Info, error) { return updater.Check() }

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

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
