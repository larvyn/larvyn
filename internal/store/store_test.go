package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnboardedForExistingInstalls(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LARVYN_HOME", dir)
	if New().Settings().Onboarded {
		t.Fatal("a fresh install should go through the first-run setup")
	}
	state := `{"projects":[{"id":"a","name":"crm","path":"/srv/crm"}],"settings":{}}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	if !New().Settings().Onboarded {
		t.Fatal("an install that already has projects should skip the first-run setup")
	}
}
