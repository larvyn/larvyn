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

// A failed save must not leave a change in memory that is not on disk.
func TestUpdateRollsBackWhenSaveFails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-folder")
	os.WriteFile(file, []byte("x"), 0o644)
	s := &Store{dir: file, data: State{
		Projects: []Project{{ID: "a"}},
		Plugins:  map[string]PluginState{"acme.x": {Enabled: true, Granted: []string{"ui.notify"}}},
	}}
	err := s.Update(func(d *State) {
		d.Projects[0].Name = "changed"
		d.Projects = append(d.Projects, Project{ID: "b"})
		st := d.Plugins["acme.x"]
		st.Granted[0] = "terminal.run"
		d.Plugins["acme.x"] = st
		d.Plugins["acme.y"] = PluginState{Enabled: true}
	})
	if err == nil {
		t.Fatal("the save should fail: the state folder is a file")
	}
	if len(s.data.Projects) != 1 || s.data.Projects[0].Name != "" {
		t.Errorf("projects not rolled back: %+v", s.data.Projects)
	}
	if len(s.data.Plugins) != 1 || s.data.Plugins["acme.x"].Granted[0] != "ui.notify" {
		t.Errorf("plugins not rolled back: %+v", s.data.Plugins)
	}
}
