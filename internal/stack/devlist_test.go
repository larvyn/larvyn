package stack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasDevListCommand(t *testing.T) {
	root := t.TempDir()
	if hasDevListCommand(root) {
		t.Fatal("no vendor folder should mean no dev:list")
	}

	console := filepath.Join(root, "vendor", "laravel", "framework", "src", "Illuminate", "Foundation", "Console")
	if err := os.MkdirAll(console, 0o755); err != nil {
		t.Fatal(err)
	}
	// Laravel 12: commands exist, dev:list does not.
	os.WriteFile(filepath.Join(console, "ServeCommand.php"), []byte("<?php #[AsCommand(name: 'serve')]"), 0o644)
	if hasDevListCommand(root) {
		t.Fatal("framework without DevListCommand reported dev:list")
	}

	// Laravel 13 ships DevListCommand.
	os.WriteFile(filepath.Join(console, "DevListCommand.php"), []byte("<?php #[AsCommand(name: 'dev:list')]"), 0o644)
	if !hasDevListCommand(root) {
		t.Fatal("DevListCommand not detected")
	}
}
