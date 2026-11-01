package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
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
}

type state struct {
	Projects []Project `json:"projects"`
	Settings Settings  `json:"settings"`
	Active   string    `json:"active"`
}

type Store struct {
	mu   sync.Mutex
	dir  string
	data state
}

var store = newStore()

func defaultSettings() Settings {
	return Settings{
		Editor:           "code",
		DefaultAgent:     "claude",
		TerminalFontSize: 13,
		Accent:           "#FF2D20",
		ConfirmDangerous: true,
	}
}

// homeDir is where Larvyn keeps its state. LARVYN_HOME points it elsewhere,
// which keeps test runs away from a real setup.
func homeDir() string {
	if h := os.Getenv("LARVYN_HOME"); h != "" {
		return h
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "larvyn")
}

func newStore() *Store {
	s := &Store{dir: homeDir(), data: state{Settings: defaultSettings()}}
	if b, err := os.ReadFile(filepath.Join(s.dir, "state.json")); err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	if s.data.Settings.TerminalFontSize == 0 {
		s.data.Settings.TerminalFontSize = 13
	}
	if s.data.Settings.Accent == "" {
		s.data.Settings.Accent = "#FF2D20"
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
	tmp := filepath.Join(s.dir, "state.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, "state.json"))
}

func (s *Store) update(fn func(d *state)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data)
	return s.saveLocked()
}

func (s *Store) Projects() []Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Project(nil), s.data.Projects...)
}

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

func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Settings
}

func (s *Store) PHP() string {
	if b := s.Settings().PHPBinary; b != "" {
		return b
	}
	return "php"
}
