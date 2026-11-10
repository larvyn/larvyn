package agentapi

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/proc"
	"larvyn/internal/terminal"
)

// A server started in a terminal shows up in ports, with its URL.
func TestPortsThroughTheAPI(t *testing.T) {
	if _, ok := proc.ListeningPorts(nil); !ok {
		t.Skip("ports can't be read here (no lsof?)")
	}
	main, _ := gitRepo(t)
	addProject(t, main)
	checkPorts(t, api.Caller{Cwd: main})
}

// fakeServer listens on a free port and says which, for the ports test. It
// gives up after a minute in case its terminal outlives the test.
func fakeServer() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer ln.Close()
	fmt.Printf("listening on %d\n", ln.Addr().(*net.TCPAddr).Port)
	time.Sleep(time.Minute)
}

// checkPorts starts a server in a terminal and finds its port with ports.
// The server is this test binary, run from its own folder so cmd.exe needs
// no quotes; python3 can be the Microsoft Store alias that runs nothing.
func checkPorts(t *testing.T, caller api.Caller) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// .\ too on Windows: with NoDefaultCurrentDirectoryInExePath set,
	// cmd.exe doesn't run a bare name from the current folder.
	run := "./" + filepath.Base(exe)
	if runtime.GOOS == "windows" {
		run = `.\` + filepath.Base(exe)
	}
	srv, err := apiTerminalOpen(caller, run+" larvyn-fake-server", filepath.Dir(exe), "server")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = terminal.Sessions.Close(srv.ID) })
	var rows PortRows
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		rows, err = apiPorts(caller, false)
		if err == nil && len(rows) == 1 && rows[0].Terminal == srv.ID && strings.HasPrefix(rows[0].URL, "http://localhost:") {
			break
		}
		if time.Now().After(deadline) {
			// What it saw, to tell a server that never started from one
			// that ports can't see.
			out, _ := apiTerminalRead(srv.ID, 0, true, false)
			s, _ := findTerminal(srv.ID)
			pid, _ := terminal.PID(srv.ID)
			all, _ := apiPorts(caller, true)
			t.Fatalf("the server's port: ports %+v (%v), all projects %+v; running=%v pid=%d; its output:\n%s", rows, err, all, s.Running, pid, out)
		}
	}
	// The server listens before it prints its port: wait for the line.
	want := fmt.Sprintf("listening on %d", rows[0].Port)
	waitFor(t, "the server to say "+want, func() bool {
		out, _ := apiTerminalRead(srv.ID, 0, true, false)
		return strings.Contains(out, want)
	})
}
