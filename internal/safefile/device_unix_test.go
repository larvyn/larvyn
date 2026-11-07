//go:build !windows

package safefile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A symlink to an endless device or a FIFO is refused at once instead of
// filling memory or blocking (#54).
func TestDevicesAndFIFOs(t *testing.T) {
	dir := t.TempDir()
	zero := filepath.Join(dir, "composer.json")
	if err := os.Symlink("/dev/zero", zero); err != nil {
		t.Skip(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip(err)
	}
	done := make(chan error, 3)
	go func() { _, err := ReadRegular(zero, 1<<20); done <- err }()
	go func() { _, err := ReadRegular(fifo, 1<<20); done <- err }()
	go func() { _, err := ReadRegularNoFollow(fifo, 1<<20); done <- err }()
	for i := 0; i < 3; i++ {
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("read a device or FIFO")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("read blocked")
		}
	}
}
