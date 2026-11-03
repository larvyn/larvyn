package plugins

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"larvyn/internal/store"
	"larvyn/internal/updater"
)

// Limits on what an install copies, so a wrong folder (a home folder, a
// project with vendor/) fails fast instead of filling the disk.
var (
	maxInstallFiles       = 2000
	maxInstallBytes int64 = 50 << 20
)

// skipDirs are never copied: they belong to the plugin's development.
var skipDirs = map[string]bool{".git": true, "node_modules": true}

// Inspect reads the plugin in dir without installing it, for the dialog
// that asks the user for its permissions.
func (s *PluginService) Inspect(dir string) (Plugin, error) {
	m, err := readInstallable(dir)
	if err != nil {
		return Plugin{}, err
	}
	p := Plugin{Manifest: m, Dir: dir}
	applyState(&p, store.Plugins()[m.ID])
	return p, nil
}

func readInstallable(dir string) (Manifest, error) {
	m, err := ReadManifest(dir)
	if errors.Is(err, os.ErrNotExist) && m.ID == "" {
		return m, errors.New("this folder has no " + ManifestFile + ", so it is not a Larvyn plugin")
	}
	if err != nil {
		return m, err
	}
	if !Compatible(m.Larvyn, updater.Version) {
		return m, fmt.Errorf("%s needs Larvyn %s; this is %s", m.Name, m.Larvyn, updater.Version)
	}
	return m, nil
}

// Install copies the plugin in dir into the plugins folder and turns it on.
// granted must hold every permission the plugin asks for: the user saw them
// in the install dialog. Installing over an existing copy updates it.
func (s *PluginService) Install(dir string, granted []string) (Plugin, error) {
	m, err := readInstallable(dir)
	if err != nil {
		return Plugin{}, err
	}
	if missing := missingPermissions(m, granted); len(missing) > 0 {
		return Plugin{}, fmt.Errorf("not installed: permissions %v were not granted", missing)
	}
	dest := filepath.Join(Dir(), m.ID)
	if !samePath(dir, dest) {
		if err := installCopy(dir, dest); err != nil {
			return Plugin{}, err
		}
	}
	err = store.Update(func(d *store.State) {
		if d.Plugins == nil {
			d.Plugins = map[string]store.PluginState{}
		}
		d.Plugins[m.ID] = store.PluginState{Enabled: true, Granted: append([]string(nil), m.Permissions...), InstalledAt: time.Now().Unix()}
	})
	if err != nil {
		return Plugin{}, err
	}
	return find(m.ID)
}

// Enable turns an installed plugin on with the permissions the user just
// approved.
func (s *PluginService) Enable(id string, granted []string) (Plugin, error) {
	p, err := find(id)
	if err != nil {
		return p, err
	}
	if p.Error != "" {
		return p, errors.New(p.Error)
	}
	if missing := missingPermissions(p.Manifest, granted); len(missing) > 0 {
		return p, fmt.Errorf("not turned on: permissions %v were not granted", missing)
	}
	err = store.Update(func(d *store.State) {
		if d.Plugins == nil {
			d.Plugins = map[string]store.PluginState{}
		}
		st := d.Plugins[id]
		st.Enabled, st.Granted = true, append([]string(nil), p.Manifest.Permissions...)
		if st.InstalledAt == 0 {
			st.InstalledAt = time.Now().Unix()
		}
		d.Plugins[id] = st
	})
	if err != nil {
		return p, err
	}
	return find(id)
}

// Disable turns a plugin off; its files and granted permissions stay.
func (s *PluginService) Disable(id string) error {
	return store.Update(func(d *store.State) {
		if st, ok := d.Plugins[id]; ok {
			st.Enabled = false
			d.Plugins[id] = st
		}
	})
}

// Uninstall deletes a plugin's folder (only the link, for a linked one) and
// forgets what the user decided about it.
func (s *PluginService) Uninstall(id string) error {
	// The id becomes a path: only a well-formed one, so it can't climb out
	// of the plugins folder.
	if !pluginIDPattern.MatchString(id) {
		return fmt.Errorf("%q is not a plugin id", id)
	}
	dir := filepath.Join(Dir(), id)
	fi, err := os.Lstat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	case fi.Mode()&os.ModeSymlink != 0:
		err = os.Remove(dir)
	default:
		err = os.RemoveAll(dir)
	}
	if err != nil {
		return err
	}
	return store.Update(func(d *store.State) { delete(d.Plugins, id) })
}

// find returns the installed plugin with this id.
func find(id string) (Plugin, error) {
	for _, p := range Load() {
		if p.Manifest.ID == id {
			return p, nil
		}
	}
	return Plugin{}, fmt.Errorf("plugin %s is not installed", id)
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// installCopy copies src into dest through a temporary folder, so a failed
// copy leaves the installed version alone. Only regular files and folders
// are copied: symlinks are skipped, so a plugin can't bring in files from
// outside its folder. The copy is checked again before it replaces dest.
func installCopy(src, dest string) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(Dir(), ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := copyTree(src, tmp); err != nil {
		return err
	}
	if _, err := ReadManifest(tmp); err != nil {
		return fmt.Errorf("the copied plugin is not valid (symlinks are not copied): %w", err)
	}
	old := ""
	if _, err := os.Lstat(dest); err == nil {
		old = tmp + ".old"
		if err := os.Rename(dest, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		if old != "" {
			_ = os.Rename(old, dest)
		}
		return err
	}
	if old != "" {
		return os.RemoveAll(old)
	}
	return nil
}

func copyTree(src, dest string) error {
	files, size := 0, int64(0)
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(dest, rel)
		switch {
		case d.IsDir():
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return os.Mkdir(target, 0o755)
		case !d.Type().IsRegular():
			return nil // symlinks, sockets, devices
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files++
		size += info.Size()
		if files > maxInstallFiles || size > maxInstallBytes {
			return fmt.Errorf("the plugin is too large: more than %d files or %d MB", maxInstallFiles, maxInstallBytes>>20)
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dest string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
