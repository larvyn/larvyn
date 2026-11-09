package proc

import (
	"net"
	"os"
	"runtime"
	"testing"
)

func TestListeningPortsFindsOurOwn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("not on Windows yet")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	want := ln.Addr().(*net.TCPAddr).Port
	ports, ok := ListeningPorts([]int{os.Getpid()})
	if !ok {
		t.Skip("ports can't be read here")
	}
	for _, p := range ports[os.Getpid()] {
		if p == want {
			return
		}
	}
	t.Fatalf("port %d not in %v", want, ports)
}
