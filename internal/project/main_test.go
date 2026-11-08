package project

import (
	"os"
	"testing"

	"larvyn/internal/store"
)

// Tests never read or write the user's real Larvyn state.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "larvyn-project-test")
	if err != nil {
		panic(err) // never run against the real state folder
	}
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
