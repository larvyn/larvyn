package main

import (
	"os"
	"runtime"
	"testing"

	"larvyn/internal/store"
)

func TestCloseProjectStopsItsTerminalsAndKeepsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep")
	}
	ps := &ProjectService{}
	p, err := ps.Add(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other, err := ps.Add(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Remove(p.ID)
	defer ps.Remove(other.ID)
	ps.Touch(p.ID)

	mine, err := terminals.Start(SessionSpec{ProjectID: p.ID, Kind: "service", Key: "serve", Argv: []string{"sleep", "30"}, Cwd: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := terminals.Start(SessionSpec{ProjectID: other.ID, Kind: "service", Key: "serve", Argv: []string{"sleep", "30"}, Cwd: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer terminals.Close(theirs.ID)

	if err := ps.Close(p.ID); err != nil {
		t.Fatal(err)
	}
	for _, s := range terminals.List() {
		if s.ID == mine.ID {
			t.Fatalf("the closed project's terminal is still listed: %+v", s)
		}
	}
	found := false
	for _, s := range terminals.List() {
		found = found || (s.ID == theirs.ID && s.Running)
	}
	if !found {
		t.Fatal("closing one project stopped another project's terminal")
	}
	if got := store.ActiveID(); got != "" {
		t.Fatalf("active project = %q after closing it, want none", got)
	}
	if _, ok := store.FindProject(p.ID); !ok {
		t.Fatal("Close removed the project from Larvyn")
	}

	// Closing a project that isn't the open one leaves the open one alone.
	ps.Touch(other.ID)
	if err := ps.Close(p.ID); err != nil {
		t.Fatal(err)
	}
	if got := store.ActiveID(); got != other.ID {
		t.Fatalf("active project = %q, want %q", got, other.ID)
	}
}
