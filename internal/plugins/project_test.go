package plugins

import (
	"runtime"
	"slices"
	"strings"
	"testing"

	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

func TestPluginOffInOneProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep")
	}
	s, a := installRun(t, "sleep 30")
	b := store.Project{ID: store.NewID(), Name: "other", Path: t.TempDir()}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, b) })

	run := func(p store.Project) terminal.SessionInfo {
		t.Helper()
		sess, err := s.RunCommand(p.ID, "acme.horizon", "hello", "sleep 30")
		if err != nil {
			t.Fatal(err)
		}
		return sess
	}
	inA, inB := run(a), run(b)

	if err := s.SetOnInProject("acme.horizon", a.ID, false); err != nil {
		t.Fatal(err)
	}
	if p, _ := find("acme.horizon"); !p.Enabled || !slices.Equal(p.OffIn, []string{a.ID}) {
		t.Fatalf("still on, off in a only: %+v", p)
	}

	// Gone from a, still in b.
	if len(s.Commands(a.ID)) != 0 || len(StackItems(a.ID)) != 0 {
		t.Error("an off plugin must not list commands or stack items in that project")
	}
	if len(s.Commands(b.ID)) != 1 || len(StackItems(b.ID)) != 1 {
		t.Error("other projects keep the plugin")
	}

	// Refused in a, from every entry point.
	off := func(what string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "off in this project") {
			t.Errorf("%s in a: %v", what, err)
		}
	}
	_, err := s.RunCommand(a.ID, "acme.horizon", "hello", "sleep 30")
	off("RunCommand", err)
	_, err = s.Call("acme.horizon", a.ID, "project.info", nil)
	off("Call", err)
	_, err = GuardStackStart("acme.horizon", a.ID, "watch", func() (terminal.SessionInfo, error) { return terminal.SessionInfo{}, nil })
	off("GuardStackStart", err)
	if _, err := s.Call("acme.horizon", b.ID, "project.info", nil); err != nil {
		t.Errorf("Call in b: %v", err)
	}

	// Turning it off in a stopped what it ran there, and only there.
	alive := map[string]bool{}
	for _, x := range terminal.Sessions.List() {
		alive[x.ID] = true
	}
	if alive[inA.ID] || !alive[inB.ID] {
		t.Errorf("sessions after turning off in a: a alive=%v, b alive=%v", alive[inA.ID], alive[inB.ID])
	}

	// On again: twice is fine, and the list is empty again.
	for i := 0; i < 2; i++ {
		if err := s.SetOnInProject("acme.horizon", a.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if st := store.Plugins()["acme.horizon"]; st.OffIn != nil {
		t.Errorf("OffIn after turning back on: %v", st.OffIn)
	}
	if len(s.Commands(a.ID)) != 1 {
		t.Error("back on in a")
	}

	if err := s.SetOnInProject("acme.horizon", "no-such-project", false); err == nil {
		t.Error("an unknown project must be refused")
	}
	if err := s.SetOnInProject("nobody.nothing", a.ID, false); err == nil {
		t.Error("an unknown plugin must be refused")
	}
}
