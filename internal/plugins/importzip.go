package plugins

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// PickZip asks for a plugin .zip and returns its path ("" if cancelled).
func (s *PluginService) PickZip() (string, error) {
	return application.Get().Dialog.OpenFile().
		CanChooseFiles(true).CanChooseDirectories(false).
		AddFilter("Plugin archive", "*.zip").
		SetTitle("Choose a plugin .zip").
		PromptForSingleSelection()
}

// InspectZip reads the plugin in a .zip without installing it, for the
// permission dialog.
func (s *PluginService) InspectZip(zipPath string) (Plugin, error) {
	dir, cleanup, err := unpackPlugin(zipPath)
	if err != nil {
		return Plugin{}, err
	}
	defer cleanup()
	p, err := s.Inspect(dir)
	p.Dir = zipPath
	return p, err
}

// InstallZip installs the plugin in a .zip, as Install does from a folder:
// granted must hold every permission it asks for.
func (s *PluginService) InstallZip(zipPath string, granted []string) (Plugin, error) {
	dir, cleanup, err := unpackPlugin(zipPath)
	if err != nil {
		return Plugin{}, err
	}
	defer cleanup()
	return s.Install(dir, granted)
}

// unpackPlugin unpacks a plugin zip into a private temporary folder and
// returns the folder that holds its larvyn.json. A zip is untrusted input,
// so it is unpacked defensively:
//   - every entry must be a relative path inside the archive (no "..", no
//     absolute or drive paths, no backslashes): no zip-slip;
//   - only folders and regular files are unpacked; symlinks and anything
//     else are refused;
//   - the file count and the bytes actually written are capped with the
//     install limits, whatever sizes the archive claims (no zip bombs);
//   - it holds one plugin: larvyn.json at the top, or in its only folder.
func unpackPlugin(zipPath string) (string, func(), error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", nil, errors.New("this is not a zip archive")
	}
	defer zr.Close()
	tmp, err := os.MkdirTemp("", "larvyn-plugin-zip-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(tmp) }
	fail := func(err error) (string, func(), error) {
		cleanup()
		return "", nil, err
	}

	files, written := 0, int64(0)
	tops := map[string]bool{}
	rootManifest := false
	for _, f := range zr.File {
		name := f.Name
		clean := path.Clean(name)
		if name == "" || strings.Contains(name, `\`) || path.IsAbs(name) || filepath.IsAbs(name) ||
			clean == ".." || strings.HasPrefix(clean, "../") || (len(name) > 1 && name[1] == ':') {
			return fail(fmt.Errorf("the archive has an unsafe path: %q", name))
		}
		if clean == "." {
			continue
		}
		top, _, _ := strings.Cut(clean, "/")
		if top == "__MACOSX" || path.Base(clean) == ".DS_Store" {
			continue // macOS Finder metadata
		}
		tops[top] = true
		if clean == ManifestFile {
			rootManifest = true
		}
		target := filepath.Join(tmp, filepath.FromSlash(clean))
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fail(err)
			}
			continue
		case !mode.IsRegular():
			return fail(fmt.Errorf("the archive has %q, which is not a regular file (links are not allowed)", name))
		}
		files++
		if files > maxInstallFiles {
			return fail(tooLarge())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fail(err)
		}
		n, err := unpackFile(f, target, maxInstallBytes-written)
		written += n
		if err != nil {
			return fail(err)
		}
	}

	dir := tmp
	if !rootManifest {
		if len(tops) != 1 {
			return fail(errors.New("the archive must hold one plugin: larvyn.json at the top, or one folder with larvyn.json inside"))
		}
		for top := range tops {
			dir = filepath.Join(tmp, top)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err != nil {
		return fail(errors.New("the archive has no " + ManifestFile + ", so it is not a Larvyn plugin"))
	}
	return dir, cleanup, nil
}

// unpackFile writes one entry, refusing to write more than budget bytes.
func unpackFile(f *zip.File, target string, budget int64) (int64, error) {
	in, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, fmt.Errorf("the archive has %q twice", f.Name)
	}
	n, err := io.Copy(out, io.LimitReader(in, budget+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > budget {
		err = tooLarge()
	}
	if err != nil {
		return n, err
	}
	// Keep the executable bit (a plugin's scripts), nothing more.
	if f.Mode()&0o100 != 0 {
		err = os.Chmod(target, 0o700)
	}
	return n, err
}
