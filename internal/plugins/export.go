package plugins

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Export asks where to save the plugin in this folder as a .zip, and saves
// it there. It returns the file's path, or "" when the user cancelled.
func (s *PluginService) Export(folder string) (string, error) {
	p, err := byFolder(folder)
	if err != nil {
		return "", err
	}
	dest, err := application.Get().Dialog.SaveFile().
		SetMessage("Export "+p.Manifest.Name).
		SetFilename(zipName(p)).
		AddFilter("Zip archive", "*.zip").
		CanCreateDirectories(true).
		PromptForSingleSelection()
	if err != nil || dest == "" {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(dest), ".zip") {
		dest += ".zip"
	}
	return dest, ExportTo(p, dest)
}

// zipName is <id>-<version>.zip, or <folder>.zip for a broken plugin.
func zipName(p Plugin) string {
	if p.Manifest.Version != "" && p.Error == "" {
		return p.Manifest.ID + "-" + p.Manifest.Version + ".zip"
	}
	return p.Folder + ".zip"
}

// byFolder finds the installed plugin in this folder of the plugins folder.
func byFolder(folder string) (Plugin, error) {
	if folder == "" || strings.HasPrefix(folder, ".") || strings.ContainsAny(folder, `/\`) {
		return Plugin{}, fmt.Errorf("%q is not a plugin folder", folder)
	}
	for _, p := range Load() {
		if p.Folder == folder {
			return p, nil
		}
	}
	return Plugin{}, fmt.Errorf("no plugin in %s", folder)
}

// ExportTo writes the plugin's files to a zip at dest, inside one folder
// named after the plugin (so unzipping gives a folder Install from folder
// accepts). It copies what Install copies: regular files only, without
// symlinks, .git or node_modules, within the same limits. The zip is
// written next to dest and renamed into place when complete.
func ExportTo(p Plugin, dest string) error {
	root, err := filepath.EvalSymlinks(p.Dir)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(dest))
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(parent, ".larvyn-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	// Saved inside the plugin's own folder, the zip being written and an
	// earlier export at the same path would otherwise end up in the zip.
	skip := map[string]bool{tmp.Name(): true, filepath.Join(parent, filepath.Base(dest)): true}
	if err := writeZip(tmp, root, p.Folder, skip); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// copyExact copies exactly size bytes and fails if the file changed size
// since it was measured: shorter (CopyN reports it) or longer (a byte is
// left after size), so the zip never stores a cut-off or a partly grown
// file. A read error after the copy is passed on.
func copyExact(out io.Writer, in io.Reader, size int64) error {
	if _, err := io.CopyN(out, in, size); err != nil {
		return fmt.Errorf("the file changed while it was exported: %w", err)
	}
	var extra [1]byte
	switch n, err := in.Read(extra[:]); {
	case n > 0:
		return errors.New("the file grew while it was exported")
	case err == nil || errors.Is(err, io.EOF):
		return nil
	default:
		return err
	}
}

func writeZip(w io.Writer, root, top string, skip map[string]bool) error {
	zw := zip.NewWriter(w)
	files, size := 0, int64(0)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		// A .git file (a linked worktree) holds a local path; it is skipped
		// like the .git folder.
		if !d.Type().IsRegular() || d.Name() == ".git" || skip[path] {
			return nil // symlinks, sockets, devices
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files++
		size += info.Size()
		if files > maxInstallFiles || size > maxInstallBytes {
			return tooLarge()
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = top + "/" + filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		out, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		return copyExact(out, in, info.Size())
	})
	if err != nil {
		zw.Close()
		return err
	}
	if files == 0 {
		zw.Close()
		return errors.New("the plugin folder is empty")
	}
	return zw.Close()
}
