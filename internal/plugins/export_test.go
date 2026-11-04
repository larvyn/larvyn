package plugins

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
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
