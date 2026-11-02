package files

import (
	"os"
	"path/filepath"
	"testing"

	"larvyn/internal/store"
)

// Tests never read or write the user's real Larvyn state.
func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "larvyn-files-test")
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// addProject registers a folder as a project, as the project service would.
func addProject(root string) (store.Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return store.Project{}, err
	}
	p := store.Project{ID: store.NewID(), Name: filepath.Base(abs), Path: abs}
	return p, store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) })
}

func removeProject(id string) {
	_ = store.Update(func(d *store.State) {
		out := d.Projects[:0]
		for _, p := range d.Projects {
			if p.ID != id {
				out = append(out, p)
			}
		}
		d.Projects = out
	})
}
