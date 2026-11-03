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
	// Folder is the name of the plugin's folder in the plugins folder. It
	// is what Uninstall takes: a broken plugin's manifest id may name
	// another plugin's folder.
	Folder string `json:"folder"`
	Error  string `json:"error,omitempty"`
	// Enabled is whether the plugin runs: the user turned it on, it has no
	// Error and every permission it asks for was granted.
	Enabled bool     `json:"enabled"`
	Granted []string `json:"granted"`
	// NeedsApproval is set when the user turned the plugin on but it now
	// asks for permissions they never granted, usually after an update.
	NeedsApproval bool `json:"needsApproval"`
	// Linked is set when the plugin folder is a symlink, as while developing
	// a plugin; uninstalling removes only the link.
	Linked bool `json:"linked"`
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
	states := store.Plugins()
	var out []Plugin
	for _, e := range entries {
		dir := filepath.Join(Dir(), e.Name())
		// Folders and symlinks to folders (handy while developing a plugin).
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() || e.Name()[0] == '.' {
			continue
		}
		p := load(dir, e.Name())
		p.Linked = e.Type()&os.ModeSymlink != 0
		applyState(&p, states[p.Manifest.ID])
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out
}

func load(dir, name string) Plugin {
	p := Plugin{Dir: dir, Folder: name}
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

// applyState fills in what the user decided about p.
func applyState(p *Plugin, st store.PluginState) {
	p.Granted = st.Granted
	if !st.Enabled || p.Error != "" {
		return
	}
	if missing := missingPermissions(p.Manifest, st.Granted); len(missing) > 0 {
		p.NeedsApproval = true
		return
	}
	p.Enabled = true
}

// missingPermissions returns the permissions m asks for that are not in
// granted.
func missingPermissions(m Manifest, granted []string) []string {
	have := map[string]bool{}
	for _, g := range granted {
		have[g] = true
	}
	var missing []string
	for _, p := range m.Permissions {
		if !have[p] {
			missing = append(missing, p)
		}
	}
	return missing
}

// Active returns the plugins that may run.
func Active() []Plugin {
	var out []Plugin
	for _, p := range Load() {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out
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

// Folder returns the plugins folder, creating it so it can be opened.
func (s *PluginService) Folder() string {
	_ = os.MkdirAll(Dir(), 0o755)
	return Dir()
}
