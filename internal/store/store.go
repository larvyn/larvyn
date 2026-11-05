// Package store keeps Larvyn's state: the projects, the user's settings and
// the folder they live in. It is the base layer and imports nothing of
// Larvyn's own.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"
)

// Project is a folder Larvyn manages, usually a Laravel application.
type Project struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	Color        string `json:"color"`
	Port         int    `json:"port"`
	Pinned       bool   `json:"pinned"`
	AddedAt      int64  `json:"addedAt"`
	LastOpenedAt int64  `json:"lastOpenedAt"`
	// AppURLBeforeFlock is the APP_URL the project had before Larvyn pointed
	// it at Flock's site, so it can go back when Flock stops serving it.
	AppURLBeforeFlock string `json:"appUrlBeforeFlock,omitempty"`
}

// Settings are the user's global preferences.
type Settings struct {
	Editor           string `json:"editor"`
	EditorCommand    string `json:"editorCommand"`
	Shell            string `json:"shell"`
	PHPBinary        string `json:"phpBinary"`
	DefaultAgent     string `json:"defaultAgent"`
	WorktreeRoot     string `json:"worktreeRoot"`
	TerminalFontSize int    `json:"terminalFontSize"`
	Accent           string `json:"accent"`
	ConfirmDangerous bool   `json:"confirmDangerous"`
	// NoUpdateCheck turns off the check for new releases at start.
	NoUpdateCheck bool `json:"noUpdateCheck"`
	// AgentPermissions is "yolo" (agents start with their skip-approvals
	// flag, as Orca does by default) or "manual" (they ask as usual).
	AgentPermissions string `json:"agentPermissions"`
	// NoRestore stops Larvyn from bringing back terminals, agents and
	// services from the previous run.
	NoRestore bool `json:"noRestore"`
	// Onboarded is set once the first-run setup was finished or skipped.
	Onboarded bool `json:"onboarded"`
	// NoFlockAutoServe stops Larvyn from serving the Laravel projects it
	// adds with Flock (flk link) when Flock runs.
	NoFlockAutoServe bool `json:"noFlockAutoServe"`
	// CustomAgents are the agents the user added in Settings.
	CustomAgents []CustomAgent `json:"customAgents,omitempty"`
}

// CustomAgent is a command that works like one of the built-in agents (its
// Base): it starts, resumes and takes its first prompt the same way, with
// its own name and environment.
type CustomAgent struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Base    string            `json:"base"`
	Command string            `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
}

// PluginState is what the user decided about an installed plugin.
type PluginState struct {
	Enabled bool `json:"enabled"`
	// Granted are the permissions the user approved. A plugin whose update
	// asks for more stays off until the user approves again.
	Granted     []string `json:"granted"`
	InstalledAt int64    `json:"installedAt"`
	// Confirmed maps a plugin command or stack item ("command:<id>",
	// "stack:<key>") to the exact command line the user confirmed before its
	// first run. A different command line asks again.
	Confirmed map[string]string `json:"confirmed,omitempty"`
	// OffIn are the projects the user turned the plugin off in. It is on in
	// every other project.
	OffIn []string `json:"offIn,omitempty"`
	// Source is "marketplace" for a plugin installed from the marketplace,
	// "" for one installed from a folder or a .zip. Only marketplace
	// installs are offered updates.
	Source string `json:"source,omitempty"`
	// Revoked is the registry's reason for revoking the installed version.
	// A revoked plugin is off and can't be turned on.
	Revoked string `json:"revoked,omitempty"`
}

// State is what state.json holds.
type State struct {
	Projects []Project `json:"projects"`
	Settings Settings  `json:"settings"`
	Active   string    `json:"active"`
	// Plugins is keyed by plugin id.
	Plugins map[string]PluginState `json:"plugins,omitempty"`
}

// Store is the state loaded from (and saved to) HomeDir()/state.json.
type Store struct {
	mu   sync.Mutex
	dir  string
	data State
}

var (
	defMu sync.Mutex
	def   = New()
)

// Default is the store the app uses.
func Default() *Store {
	defMu.Lock()
	defer defMu.Unlock()
	return def
}

// Reload replaces the default store with a fresh read of HomeDir(), e.g.
// after a test pointed LARVYN_HOME somewhere else.
func Reload() {
	s := New()
	defMu.Lock()
	def = s
	defMu.Unlock()
}

// Projects, FindProject, CurrentSettings, PHP, Update and ActiveID act on the
// default store.
func Projects() []Project                   { return Default().Projects() }
func FindProject(id string) (Project, bool) { return Default().Project(id) }
func CurrentSettings() Settings             { return Default().Settings() }
func PHP() string                           { return Default().PHP() }
func Update(fn func(d *State)) error        { return Default().Update(fn) }
func ActiveID() string                      { return Default().ActiveID() }
func Plugins() map[string]PluginState       { return Default().Plugins() }

// DefaultSettings are the preferences of a fresh install.
func DefaultSettings() Settings {
	return Settings{
		Editor:           "code",
		DefaultAgent:     "claude",
		AgentPermissions: "yolo",
		TerminalFontSize: 13,
		Accent:           "#FF2D20",
		ConfirmDangerous: true,
	}
}

// NewID returns a short random id for a project or a terminal session.
func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ProjectPath returns the folder of the project with this id.
func ProjectPath(id string) (string, error) {
	p, ok := FindProject(id)
	if !ok {
		return "", errors.New("project not found")
	}
	return p.Path, nil
}

// HomeDir is where Larvyn keeps its state. LARVYN_HOME points it elsewhere,
// which keeps test runs away from a real setup.
func HomeDir() string {
	if h := os.Getenv("LARVYN_HOME"); h != "" {
		return h
	}
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, "larvyn")
	}
	// No $HOME: the account's home from the system's user database still
	// gives the same folder on every launch, so state persists.
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return filepath.Join(u.HomeDir, ".config", "larvyn")
	}
	return fallbackHome()
}

// fallbackHome is the last resort, when the account has no home at all: a
// private folder for this run. It is never a shared, predictable path in the
// temp dir that another account could have created first (#53); when even
// the private folder can't be made, it is a random name nobody can have
// prepared, and writes there simply fail.
var fallbackHome = sync.OnceValue(func() string {
	if dir, err := os.MkdirTemp("", "larvyn-"); err == nil {
		return dir
	}
	return filepath.Join(os.TempDir(), "larvyn-"+NewID()+NewID())
})

// New reads the state from HomeDir().
func New() *Store {
	s := &Store{dir: HomeDir(), data: State{Settings: DefaultSettings()}}
	if b, err := os.ReadFile(filepath.Join(s.dir, "state.json")); err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	if s.data.Settings.TerminalFontSize == 0 {
		s.data.Settings.TerminalFontSize = 13
	}
	if s.data.Settings.Accent == "" {
		s.data.Settings.Accent = "#FF2D20"
	}
	// Installs from before the first-run setup already have projects: they
	// don't need to be walked through it.
	if len(s.data.Projects) > 0 {
		s.data.Settings.Onboarded = true
	}
	return s
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	// A fresh temp file each time (CreateTemp makes it 0600): reusing a fixed
	// name would keep the mode of one an older version left behind.
	f, err := os.CreateTemp(s.dir, "state.json.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // a no-op once renamed
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.dir, "state.json"))
}

// Update changes the state under the lock and saves it.
func (s *Store) Update(fn func(d *State)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// If the save fails, the change is undone, so what Larvyn reports keeps
	// matching what is on disk.
	before := s.data.clone()
	fn(&s.data)
	if err := s.saveLocked(); err != nil {
		s.data = before
		return err
	}
	return nil
}

// clone is a deep copy: fn may change slices and maps in place.
func (d State) clone() State {
	c := d
	c.Projects = append([]Project(nil), d.Projects...)
	if d.Plugins != nil {
		c.Plugins = make(map[string]PluginState, len(d.Plugins))
		for id, p := range d.Plugins {
			p.Granted = append([]string(nil), p.Granted...)
			p.Confirmed = copyMap(p.Confirmed)
			c.Plugins[id] = p
		}
	}
	return c
}

func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	c := make(map[string]string, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// Plugins returns a copy of the plugin decisions, keyed by plugin id.
func (s *Store) Plugins() map[string]PluginState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]PluginState, len(s.data.Plugins))
	for id, p := range s.data.Plugins {
		p.Granted = append([]string(nil), p.Granted...)
		p.Confirmed = copyMap(p.Confirmed)
		out[id] = p
	}
	return out
}

// Projects returns a copy of the saved projects.
func (s *Store) Projects() []Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Project(nil), s.data.Projects...)
}

// Project finds a project by id.
func (s *Store) Project(id string) (Project, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.data.Projects {
		if p.ID == id {
			return p, true
		}
	}
	return Project{}, false
}

// ActiveID is the project that was open last.
func (s *Store) ActiveID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Active
}

// Settings returns the current preferences.
func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Settings
}

// PHP is the PHP binary to run: the one set in Settings, or php from PATH.
func (s *Store) PHP() string {
	if b := s.Settings().PHPBinary; b != "" {
		return b
	}
	return "php"
}

// AppendLog adds a line to larvyn.log in HomeDir(), so errors leave a trace.
func AppendLog(level, message string) {
	_ = os.MkdirAll(HomeDir(), 0o755)
	f, err := os.OpenFile(filepath.Join(HomeDir(), "larvyn.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	// Logs from older versions were created world-readable.
	_ = f.Chmod(0o600)
	fmt.Fprintf(f, "%s %s %s\n", time.Now().Format(time.RFC3339), level, message)
}
