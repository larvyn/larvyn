package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileServiceListReadWrite(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "app", "Models"), 0o755))
	must(os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	must(os.WriteFile(filepath.Join(root, "app", "Models", "User.php"), []byte("<?php\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "artisan"), []byte("#!/usr/bin/env php\n"), 0o755))
	must(os.WriteFile(filepath.Join(root, "logo.png"), []byte{0x89, 'P', 'N', 'G', 0, 1}, 0o644))

	svc := &FileService{}
	p, err := (&ProjectService{}).Add(root)
	must(err)
	defer (&ProjectService{}).Remove(p.ID)

	list, err := svc.List(p.ID, "")
	must(err)
	if len(list) != 3 || list[0].Name != "app" || !list[0].Dir || list[1].Name != "artisan" {
		t.Fatalf("root listing = %+v, want app/ first, artisan, logo.png and no .git", list)
	}
	sub, err := svc.List(p.ID, "app/Models")
	must(err)
	if len(sub) != 1 || sub[0].Path != "app/Models/User.php" {
		t.Fatalf("app/Models = %+v", sub)
	}

	fc, err := svc.Read(p.ID, "app/Models/User.php")
	must(err)
	if fc.Content != "<?php\n" || fc.Binary || fc.ModTime == 0 {
		t.Fatalf("read = %+v", fc)
	}
	if bin, _ := svc.Read(p.ID, "logo.png"); !bin.Binary || bin.Content != "" {
		t.Fatalf("logo.png should be binary: %+v", bin)
	}

	mt, err := svc.Write(p.ID, "app/Models/User.php", "<?php // edited\n", fc.ModTime)
	must(err)
	if b, _ := os.ReadFile(filepath.Join(root, "app", "Models", "User.php")); string(b) != "<?php // edited\n" {
		t.Fatalf("content not written: %q", b)
	}
	// A stale modTime (the file changed since it was read) is refused.
	if _, err := svc.Write(p.ID, "app/Models/User.php", "lost", mt-1); err == nil {
		t.Fatal("stale write should fail")
	}
	// The executable bit survives a save.
	art, _ := svc.Read(p.ID, "artisan")
	_, err = svc.Write(p.ID, "artisan", "#!/usr/bin/env php\n// x\n", art.ModTime)
	must(err)
	if st, _ := os.Stat(filepath.Join(root, "artisan")); runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("artisan lost its executable bit: %v", st.Mode())
	}
}

func TestResolveWithinRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"../secret", "a/../../secret", filepath.Join(outside, "secret")} {
		if _, err := resolveWithin(root, rel); err == nil {
			t.Errorf("resolveWithin(%q) should fail", rel)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveWithin(root, "link/secret"); err == nil {
			t.Error("a symlink out of the project should be refused")
		}
		if err := os.MkdirAll(filepath.Join(root, "storage", "app", "public"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "storage", "app", "public"), filepath.Join(root, "public-storage")); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveWithin(root, "public-storage"); err != nil {
			t.Errorf("a symlink inside the project should work: %v", err)
		}
	}
	if abs, err := resolveWithin(root, ""); err != nil || abs != root {
		t.Errorf("root = %q, %v", abs, err)
	}
}
