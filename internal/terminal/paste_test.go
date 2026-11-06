package terminal

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestSaveImage(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("TMP", os.Getenv("TMPDIR")) // Windows
	png := []byte("\x89PNG\r\n\x1a\nrest-of-image")
	s := &TerminalService{}
	path, err := s.SaveImage(base64.StdEncoding.EncodeToString(png))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".png" || !strings.HasPrefix(path, pastedImageDir()) {
		t.Errorf("unexpected path %q", path)
	}
	if b, _ := os.ReadFile(path); string(b) != string(png) {
		t.Errorf("saved %q, want the image bytes", b)
	}

	if _, err := s.SaveImage(base64.StdEncoding.EncodeToString([]byte("#!/bin/sh\nrm -rf ~"))); err == nil {
		t.Error("saved data that is not an image")
	}
	if _, err := s.SaveImage("%%%"); err == nil {
		t.Error("accepted data that is not base64")
	}
}

func TestSaveImageCleansOldImages(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("TMP", os.Getenv("TMPDIR"))
	dir := pastedImageDir()
	os.MkdirAll(dir, 0o700)
	old := filepath.Join(dir, "paste-old.png")
	os.WriteFile(old, []byte("x"), 0o600)
	past := time.Now().Add(-2 * pastedImageTTL)
	os.Chtimes(old, past, past)

	s := &TerminalService{}
	if _, err := s.SaveImage(base64.StdEncoding.EncodeToString([]byte("GIF89a..."))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("an image older than a day was kept")
	}
}

func TestImageExt(t *testing.T) {
	cases := map[string]string{
		"\x89PNG\r\n\x1a\n...":         ".png",
		"\xff\xd8\xff\xe0...":          ".jpg",
		"GIF87a...":                    ".gif",
		"RIFF\x00\x00\x00\x00WEBPVP8 ": ".webp",
		"<svg></svg>":                  "",
		"":                             "",
	}
	for in, want := range cases {
		if got := imageExt([]byte(in)); got != want {
			t.Errorf("imageExt(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQuotingFor(t *testing.T) {
	cases := []struct{ goos, program, want string }{
		{"linux", "/bin/zsh", "posix"},
		{"darwin", "/usr/local/bin/claude", "posix"},
		{"windows", `C:\Program Files\PowerShell\7\pwsh.exe`, "powershell"},
		{"windows", "powershell.exe", "powershell"},
		{"windows", "cmd.exe", "cmd"},
		{"windows", `C:\Windows\System32\CMD.EXE`, "cmd"},
		{"windows", `C:\Program Files\Git\bin\bash.exe`, "posix"},
		{"windows", `C:\Users\i\.local\bin\claude.exe`, "strict"},
		{"windows", "", "strict"},
	}
	for _, c := range cases {
		if got := quotingFor(c.goos, c.program); got != c.want {
			t.Errorf("quotingFor(%q, %q) = %q, want %q", c.goos, c.program, got, c.want)
		}
	}
}

func TestQuotingFollowsTheSessionsProgram(t *testing.T) {
	Sessions.mu.Lock()
	Sessions.sessions["quoting-cmd"] = &session{info: SessionInfo{ID: "quoting-cmd"}, argv: []string{`C:\Windows\System32\cmd.exe`}}
	Sessions.mu.Unlock()
	defer func() {
		Sessions.mu.Lock()
		delete(Sessions.sessions, "quoting-cmd")
		Sessions.mu.Unlock()
	}()
	want := "posix"
	if runtime.GOOS == "windows" {
		want = "cmd"
	}
	if got := Sessions.Quoting("quoting-cmd"); got != want {
		t.Errorf("Quoting = %q, want %q", got, want)
	}
}

func TestDropFor(t *testing.T) {
	add := func(id string, running bool) {
		Sessions.mu.Lock()
		Sessions.sessions[id] = &session{info: SessionInfo{ID: id, Running: running}}
		Sessions.mu.Unlock()
		t.Cleanup(func() {
			Sessions.mu.Lock()
			delete(Sessions.sessions, id)
			Sessions.mu.Unlock()
		})
	}
	add("drop-live", true)
	add("drop-exited", false)
	on := func(id string) *application.DropTargetDetails {
		return &application.DropTargetDetails{Attributes: map[string]string{DropSessionAttr: id}}
	}
	files := []string{"/tmp/a.png"}

	if d, ok := dropFor(files, on("drop-live")); !ok || d.Session != "drop-live" || len(d.Paths) != 1 {
		t.Errorf("a drop on a running session was not passed on: %+v %v", d, ok)
	}
	for name, target := range map[string]*application.DropTargetDetails{
		"exited session":  on("drop-exited"),
		"unknown session": on("nope"),
		"not a terminal":  {Attributes: map[string]string{}},
		"no target":       nil,
	} {
		if _, ok := dropFor(files, target); ok {
			t.Errorf("%s: drop passed on", name)
		}
	}
	if _, ok := dropFor(nil, on("drop-live")); ok {
		t.Error("a drop without files was passed on")
	}
}
