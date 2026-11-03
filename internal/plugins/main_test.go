package plugins

import (
	"fmt"
	"os"
	"testing"

	"larvyn/internal/store"
)

// Tests never read or write the user's real Larvyn state.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "larvyn-plugins-test")
	if err != nil || home == "" {
		// An empty LARVYN_HOME would point the tests, which delete the
		// plugins folder, at the user's real one.
		panic("no temp folder for LARVYN_HOME: " + fmt.Sprint(err))
	}
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
