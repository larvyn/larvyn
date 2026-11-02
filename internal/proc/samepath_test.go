package proc

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSamePathResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	_ = os.Mkdir(real, 0o755)
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	if !SamePath(link, real+string(filepath.Separator)) {
		t.Error("a symlink and its target are the same folder")
	}
	if SamePath(real, filepath.Join(dir, "other")) || SamePath("", real) {
		t.Error("different or empty paths are not the same")
	}
	if runtime.GOOS == "linux" && SamePath(real, filepath.Join(dir, "REAL")) {
		t.Error("case matters on Linux")
	}
}
