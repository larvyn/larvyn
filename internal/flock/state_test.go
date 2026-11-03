//go:build !windows

package flock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeFlk puts a flk script in dir that appends its arguments and working
// folder to dir/calls.
func fakeFlk(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"$PWD $*\" >> " + filepath.Join(dir, "calls") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "flk"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FLOCK_HOME", home)
	t.Setenv("PATH", t.TempDir())
	Forget()
	t.Cleanup(Forget)
	if s := State(); s != NotInstalled {
		t.Errorf("nothing installed: %q", s)
	}

	pkg := t.TempDir()
	fakeFlk(t, pkg)
	t.Setenv("PATH", pkg)
	if s := State(); s != NotSetUp {
		t.Errorf("package only: %q", s)
	}

	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if s := State(); s != Stopped {
		t.Errorf("set up, no daemon: %q", s)
	}
}

func TestLink(t *testing.T) {
	app := t.TempDir()
	home := fakeDaemon(t, `{"sites":[{"name":"acme","domain":"acme.test","path":`+quote(app)+`}]}`)
	fakeFlk(t, filepath.Join(home, "bin"))
	if s := State(); s != Running {
		t.Errorf("State = %q", s)
	}
	site, err := Link(app)
	if err != nil || site.URL != "http://acme.test" {
		t.Fatalf("Link = %+v, %v", site, err)
	}
	calls, _ := os.ReadFile(filepath.Join(home, "bin", "calls"))
	dir, ok := strings.CutSuffix(string(calls), " link\n")
	if !ok || !samePath(dir, app) {
		t.Errorf("flk ran as %q, want `flk link` in %s", calls, app)
	}
}
