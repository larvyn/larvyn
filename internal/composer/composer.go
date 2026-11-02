// Package composer finds Composer for the other packages: on PATH, or the
// composer.phar Larvyn keeps in its own folder and runs with the configured PHP,
// so Composer works even when it is not installed.
package composer

import (
	"os"
	"path/filepath"

	"larvyn/internal/proc"
	"larvyn/internal/store"
)

// Phar is where Larvyn keeps its own composer.phar.
func Phar() string {
	return filepath.Join(store.HomeDir(), "bin", "composer.phar")
}

// Argv returns the command that runs Composer, or nil when there is none yet.
func Argv() []string {
	if p := proc.Which("composer"); p != "" {
		return []string{p}
	}
	if _, err := os.Stat(Phar()); err == nil {
		return []string{store.PHP(), Phar()}
	}
	return nil
}

// Available reports whether Composer can run, from PATH or Larvyn's phar.
func Available() bool { return Argv() != nil }

// Resolve rewrites argv so a bare "composer" uses Larvyn's own phar when
// Composer is not on PATH.
func Resolve(argv []string) []string {
	if len(argv) > 0 && argv[0] == "composer" {
		if c := Argv(); c != nil {
			return append(c, argv[1:]...)
		}
	}
	return argv
}
