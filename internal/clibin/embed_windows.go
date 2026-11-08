package clibin

import (
	"embed"
	"path/filepath"
)

// The console CLI that build:cli (build/Taskfile.yml) put here; only
// .gitkeep in a build that skipped it (go test, go run).
//
//go:embed bin/*
var files embed.FS

// install writes the embedded CLI as larvyn.exe.
func install(dir string) error {
	b, err := files.ReadFile("bin/larvyn-" + runtimeArch + ".exe")
	if err != nil {
		return ErrNotBuilt
	}
	return write(filepath.Join(dir, "larvyn.exe"), b)
}
