package proc

import (
	"os"
	"runtime"
	"testing"
)

func TestUnwrapShim(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("batch shims are Windows only")
	}
	dir := t.TempDir()
	exe, _ := os.Executable()
	write := func(name, body string) string {
		p := dir + `\` + name
		os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	herd := write("php.bat", "@echo off\r\n\""+exe+"\" %*\r\n")
	if got := UnwrapShim(herd); got != exe {
		t.Errorf("Herd-style shim: got %q", got)
	}
	complex := write("npm.cmd", "@echo off\r\nSETLOCAL\r\n\""+exe+"\" %*\r\n")
	if got := UnwrapShim(complex); got != complex {
		t.Errorf("a script with more than one command must run as is, got %q", got)
	}
	if got := UnwrapShim(exe); got != exe {
		t.Errorf("an .exe stays as is, got %q", got)
	}
}

func TestStripANSIKeepsCursorMovesAsSpaces(t *testing.T) {
	in := "\x1b[1mAccessing\x1b[1Cworkspace:\x1b[0m\x1b[Cdone\x1b[3Cx\x1b]0;title\x07"
	if got := StripANSI(in); got != "Accessing workspace: done   x" {
		t.Errorf("got %q", got)
	}
}
