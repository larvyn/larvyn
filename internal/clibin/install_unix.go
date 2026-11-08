//go:build !windows

package clibin

import (
	"os"
	"path/filepath"
)

// install links larvyn to the app's own binary, which runs the CLI when
// given a command.
func install(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return link(exe, filepath.Join(dir, "larvyn"))
}

// link makes path a symlink to target, replacing what was there.
func link(target, path string) error {
	if cur, err := os.Readlink(path); err == nil && cur == target {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".new"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
