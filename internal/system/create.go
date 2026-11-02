package system

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"

	"larvyn/internal/store"
)

//go:embed create.php
var createSource []byte

// createScript returns the path of create.php, Larvyn's stand-in for
// `laravel new` when the installer is missing, written next to the app state.
func createScript() string {
	path := filepath.Join(store.HomeDir(), "create.php")
	_ = os.MkdirAll(store.HomeDir(), 0o755)
	if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, createSource) {
		_ = os.WriteFile(path, createSource, 0o644)
	}
	return path
}
