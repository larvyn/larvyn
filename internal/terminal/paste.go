package terminal

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"larvyn/internal/proc"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// FilesDropped is files dropped on a terminal: the window types their paths
// in as a paste, which is how agents take a dropped image.
type FilesDropped struct {
	Session string   `json:"session"`
	Paths   []string `json:"paths"`
}

func init() {
	application.RegisterEvent[FilesDropped]("term:dropped")
}

// DropSessionAttr is the attribute on a terminal's element that names its
// session; the element is also a Wails file drop target.
const DropSessionAttr = "data-session"

// HandleDrop passes files dropped on a terminal to the window. Main calls it
// from the window's FilesDropped event; drops anywhere else are ignored.
func HandleDrop(paths []string, target *application.DropTargetDetails) {
	if d, ok := dropFor(paths, target); ok {
		emit("term:dropped", d)
	}
}

// dropFor is the drop to pass on: files dropped on a running session.
func dropFor(paths []string, target *application.DropTargetDetails) (FilesDropped, bool) {
	if target == nil || len(paths) == 0 {
		return FilesDropped{}, false
	}
	id := target.Attributes[DropSessionAttr]
	if id == "" {
		return FilesDropped{}, false
	}
	s, err := Sessions.get(id)
	if err != nil {
		return FilesDropped{}, false
	}
	s.mu.Lock()
	running := s.info.Running
	s.mu.Unlock()
	if !running {
		return FilesDropped{}, false // an exited session takes no input
	}
	return FilesDropped{Session: id, Paths: paths}, true
}

// Quoting names how the window quotes paths it pastes, after the shell new
// terminals start: "posix" (sh, bash, zsh, fish), "powershell" or "cmd".
func (t *TerminalService) Quoting() string {
	return quotingFor(runtime.GOOS, proc.DefaultShell()[0])
}

func quotingFor(goos, shell string) string {
	if goos != "windows" {
		return "posix"
	}
	// The path is a Windows one: its base is after the last \ or /.
	base := strings.ToLower(shell[strings.LastIndexAny(shell, `\/`)+1:])
	if base == "cmd.exe" || base == "cmd" {
		return "cmd"
	}
	return "powershell"
}

// maxPastedImage is the largest clipboard image SaveImage keeps.
const maxPastedImage = 25 << 20

// pastedImageTTL is how long saved clipboard images are kept: long enough
// for the agent the path was pasted to to read it.
const pastedImageTTL = 24 * time.Hour

// pastedImageDir is where SaveImage writes clipboard images.
func pastedImageDir() string { return filepath.Join(os.TempDir(), "larvyn-paste") }

// SaveImage writes a pasted clipboard image (PNG, JPEG, GIF or WebP, as
// base64) to a temporary file and returns its path, for the window to paste
// where the program reads images from paths rather than the clipboard.
// Images saved more than a day ago are deleted.
func (t *TerminalService) SaveImage(data string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", errors.New("the image is not valid base64")
	}
	if len(b) > maxPastedImage {
		return "", errors.New("the image is larger than 25 MB")
	}
	ext := imageExt(b)
	if ext == "" {
		return "", errors.New("not a PNG, JPEG, GIF or WebP image")
	}
	dir := pastedImageDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	cleanPastedImages(dir, time.Now().Add(-pastedImageTTL))
	path := filepath.Join(dir, "paste-"+strconv.FormatInt(time.Now().UnixNano(), 36)+ext)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// imageExt returns the file extension for an image's bytes, or "".
func imageExt(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return ".png"
	case bytes.HasPrefix(b, []byte{0xff, 0xd8, 0xff}):
		return ".jpg"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return ".gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return ".webp"
	}
	return ""
}

// cleanPastedImages deletes the images in dir saved before cutoff.
func cleanPastedImages(dir string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
