package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

// The plugins in examples/plugins are what people copy, so they must always
// pass the checks Larvyn runs.
func TestExamplePlugins(t *testing.T) {
	root := filepath.Join("..", "..", "examples", "plugins")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n++
		p := load(filepath.Join(root, e.Name()), e.Name())
		if p.Error != "" {
			t.Errorf("%s: %s", e.Name(), p.Error)
		}
	}
	if n < 3 {
		t.Errorf("found %d example plugins, want at least 3", n)
	}
}
