package safefile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegular(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.json")
	os.WriteFile(small, []byte(`{"a":1}`), 0o644)
	if b, err := ReadRegular(small, 100); err != nil || string(b) != `{"a":1}` {
		t.Fatalf("got %q, %v", b, err)
	}
	// Exactly at the limit is fine, one byte over is not.
	if _, err := ReadRegular(small, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRegular(small, 6); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if _, err := ReadRegular(dir, 100); err == nil {
		t.Fatal("read a folder")
	}
	if _, err := ReadRegular(filepath.Join(dir, "missing"), 100); !os.IsNotExist(err) {
		t.Fatalf("want not-exist, got %v", err)
	}
}

func TestSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	os.WriteFile(target, []byte("x"), 0o644)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if b, err := ReadRegular(link, 10); err != nil || string(b) != "x" {
		t.Fatalf("ReadRegular should follow a link to a regular file: %q %v", b, err)
	}
	if _, err := ReadRegularNoFollow(link, 10); err == nil {
		t.Fatal("ReadRegularNoFollow followed a symlink")
	}
	if b, err := ReadRegularNoFollow(target, 10); err != nil || string(b) != "x" {
		t.Fatalf("%q %v", b, err)
	}
}
