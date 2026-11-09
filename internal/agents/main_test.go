package agents

import (
	"os"
	"testing"

	"larvyn/internal/store"
)

// Tests never read or write the user's real Larvyn state.
func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "larvyn-agents-test")
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// useYolo sets agents to skip approvals for the test, as the tests of the
// launch flags need (a fresh install asks first).
func useYolo(t *testing.T) {
	t.Helper()
	prev := store.CurrentSettings().AgentPermissions
	set := func(v string) {
		if err := store.Update(func(d *store.State) { d.Settings.AgentPermissions = v }); err != nil {
			t.Fatal(err)
		}
	}
	set("yolo")
	t.Cleanup(func() { set(prev) })
}
