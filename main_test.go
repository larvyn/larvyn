package main

import (
	"os"
	"testing"

	"larvyn/internal/store"
	"larvyn/internal/wiring"
)

func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "larvyn-test")
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	wiring.Connect()
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
