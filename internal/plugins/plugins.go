// Package plugins loads the plugins installed in Larvyn's plugins folder and
// checks their manifests. It is the plugin host: later steps route every
// call a plugin makes through it, so the permissions are enforced here, in
// Go, and never by the plugin's own code.
package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"larvyn/internal/store"
	"larvyn/internal/updater"
)

// PluginService lists the installed plugins for the frontend.
type PluginService struct{}

// Plugin is one folder in the plugins folder. A plugin with an Error is
// shown so the user can fix or remove it, but it is never run.
type Plugin struct {
	Manifest Manifest `json:"manifest"`
	Dir      string   `json:"dir"`
	Error    string   `json:"error,omitempty"`
}

// Dir is the folder plugins are installed in, one folder per plugin named
// after its id.
func Dir() string {
	return filepath.Join(store.HomeDir(), "plugins")
}

// Load reads every plugin in Dir(), sorted by id. A missing folder means no
// plugins.
func Load() []Plugin {
	entries, err := os.ReadDir(Dir())
	if err != nil {
		return nil
	}
	var out []Plugin
	for _, e := range entries {
		dir := filepath.Join(Dir(), e.Name())
		// Folders and symlinks to folders (handy while developing a plugin).
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() || e.Name()[0] == '.' {
			continue
		}
		out = append(out, load(dir, e.Name()))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out
}

func load(dir, name string) Plugin {
	p := Plugin{Dir: dir}
	m, err := ReadManifest(dir)
	p.Manifest = m
	switch {
	case errors.Is(err, os.ErrNotExist) && m.ID == "":
		err = errors.New("no " + ManifestFile + " in this folder")
	case err == nil && m.ID != name:
		err = errors.New("the folder must be named after the plugin id " + m.ID)
	case err == nil && !Compatible(m.Larvyn, updater.Version):
		err = errors.New("needs Larvyn " + m.Larvyn + "; this is " + updater.Version)
	}
	if p.Manifest.ID == "" {
		p.Manifest.ID = name
	}
	if err != nil {
		p.Error = err.Error()
	}
	return p
}

// List returns the installed plugins.
func (s *PluginService) List() []Plugin {
	return Load()
}

// Permissions returns every permission a plugin can ask for, for the
// install dialog.
func (s *PluginService) Permissions() []Permission {
	return Permissions
}

// Folder returns the plugins folder.
func (s *PluginService) Folder() string {
	return Dir()
}
