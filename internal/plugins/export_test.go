package plugins

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/iotest"
)

func TestExportTo(t *testing.T) {
	reset(t)
	dir := filepath.Join(Dir(), "acme.horizon")
	writePlugin(t, dir, validManifest)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o755)
	os.WriteFile(filepath.Join(dir, "node_modules", "x", "i.js"), []byte("x"), 0o644)
	secret := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(secret, []byte("secret"), 0o644)
	os.Symlink(secret, filepath.Join(dir, "ui", "link"))

	p, err := byFolder("acme.horizon")
	if err != nil {
		t.Fatal(err)
	}
	if zipName(p) != "acme.horizon-1.0.0.zip" {
		t.Errorf("name: %s", zipName(p))
	}
	dest := filepath.Join(t.TempDir(), "out.zip")
	if err := ExportTo(p, dest); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "acme.horizon/larvyn.json,acme.horizon/ui/index.html" {
		t.Errorf("zip holds %v; want only the plugin's own files, inside acme.horizon/", names)
	}

	// Unzipped, it is a folder Install from folder accepts.
	out := t.TempDir()
	for _, f := range zr.File {
		rc, _ := f.Open()
		target := filepath.Join(out, filepath.FromSlash(f.Name))
		os.MkdirAll(filepath.Dir(target), 0o755)
		b := make([]byte, f.UncompressedSize64)
		rc.Read(b)
		rc.Close()
		os.WriteFile(target, b, 0o644)
	}
	if _, err := ReadManifest(filepath.Join(out, "acme.horizon")); err != nil {
		t.Errorf("the exported plugin doesn't read back: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".larvyn-export-*")); len(left) > 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

// Exporting into the plugin's own folder, twice, must not put the zip (or
// its temporary file) inside itself; a .git file is left out too.
func TestExportIntoPluginFolder(t *testing.T) {
	reset(t)
	dir := filepath.Join(Dir(), "acme.horizon")
	writePlugin(t, dir, validManifest)
	os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /Users/me/secret/repo/.git/worktrees/x"), 0o644)
	p, _ := byFolder("acme.horizon")
	dest := filepath.Join(dir, "export.zip")
	for i := 0; i < 2; i++ {
		if err := ExportTo(p, dest); err != nil {
			t.Fatal(err)
		}
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".zip") || strings.Contains(f.Name, ".larvyn-export-") || strings.HasSuffix(f.Name, "/.git") {
			t.Errorf("the zip holds %s", f.Name)
		}
	}
	if len(zr.File) != 2 {
		t.Errorf("want the 2 plugin files, got %d", len(zr.File))
	}
}

func TestExportRefuses(t *testing.T) {
	reset(t)
	for _, f := range []string{"", "../x", ".cache", `a\b`, "acme.nope"} {
		if _, err := byFolder(f); err == nil {
			t.Errorf("byFolder(%q) should fail", f)
		}
	}
	old := maxInstallFiles
	maxInstallFiles = 1
	defer func() { maxInstallFiles = old }()
	writePlugin(t, filepath.Join(Dir(), "acme.horizon"), validManifest)
	p, _ := byFolder("acme.horizon")
	dest := filepath.Join(t.TempDir(), "out.zip")
	if err := ExportTo(p, dest); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("limits: %v", err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("a failed export must not leave a zip")
	}
}

// copyExact must store a file only if it still has the size it was
// measured with: neither cut short nor grown since.
func TestCopyExact(t *testing.T) {
	var out strings.Builder
	if err := copyExact(&out, strings.NewReader("12345"), 5); err != nil || out.String() != "12345" {
		t.Errorf("same size: %q %v", out.String(), err)
	}
	if err := copyExact(io.Discard, strings.NewReader("123"), 5); err == nil {
		t.Error("a file that shrank must fail")
	}
	if err := copyExact(io.Discard, strings.NewReader("1234567"), 5); err == nil || !strings.Contains(err.Error(), "grew") {
		t.Errorf("a file that grew must fail: %v", err)
	}
	boom := errors.New("disk error")
	if err := copyExact(io.Discard, io.MultiReader(strings.NewReader("12345"), iotest.ErrReader(boom)), 5); !errors.Is(err, boom) {
		t.Errorf("a read error after the copy must be passed on: %v", err)
	}
}
