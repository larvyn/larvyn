// Package clibin puts the larvyn CLI (internal/cli) in a folder of the state
// folder that every terminal Larvyn opens has first on its PATH
// (terminal.BinDir), so agents find a CLI that matches the app.
//
// On Linux and macOS that is a link to the app's own binary, which runs the
// CLI when given a command. On Windows the app is a GUI program that can't
// print to a terminal, so it carries the console build of cmd/larvyn and
// writes it there.
package clibin

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"larvyn/internal/store"
)

var runtimeArch = runtime.GOARCH

// Dir is where the CLI goes.
func Dir() string { return filepath.Join(store.HomeDir(), "bin") }

// ErrNotBuilt means this Windows build carries no CLI: `wails3 build` adds
// it, go run and go test don't.
var ErrNotBuilt = errors.New("this build has no larvyn CLI (wails3 build adds it)")

// Install puts the CLI in Dir, when it is missing or out of date, and
// returns the folder.
func Install() (string, error) {
	dir := Dir()
	if err := install(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// write replaces a file when its bytes differ.
func write(path string, b []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, 0o700); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// Windows can't replace an exe that is running (an agent is in the
		// middle of a larvyn command), but it can move it aside.
		_ = os.Remove(path + ".old")
		if os.Rename(path, path+".old") == nil {
			err = os.Rename(tmp, path)
		}
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	_ = os.Remove(path + ".old")
	return nil
}
