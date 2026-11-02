package files

import (
	"encoding/json"
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
	p, err := addProject(root)
	must(err)
	defer removeProject(p.ID)

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
	if fc.Content != "<?php\n" || fc.Binary || fc.Version == "" {
		t.Fatalf("read = %+v", fc)
	}
	if bin, _ := svc.Read(p.ID, "logo.png"); !bin.Binary || bin.Content != "" {
		t.Fatalf("logo.png should be binary: %+v", bin)
	}

	mt, err := svc.Write(p.ID, "app/Models/User.php", "<?php // edited\n", fc.Version)
	must(err)
	if b, _ := os.ReadFile(filepath.Join(root, "app", "Models", "User.php")); string(b) != "<?php // edited\n" {
		t.Fatalf("content not written: %q", b)
	}
	// Saving again with the version Write returned works; a stale one (the
	// file changed since it was read) is refused.
	if _, err := svc.Write(p.ID, "app/Models/User.php", "<?php // again\n", mt); err != nil {
		t.Fatalf("save with the returned version: %v", err)
	}
	if _, err := svc.Write(p.ID, "app/Models/User.php", "lost", fc.Version); err == nil {
		t.Fatal("stale write should fail")
	}
	// The executable bit survives a save.
	art, _ := svc.Read(p.ID, "artisan")
	_, err = svc.Write(p.ID, "artisan", "#!/usr/bin/env php\n// x\n", art.Version)
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

// The version makes a round trip through the window as JSON, where numbers
// are float64: it must come back unchanged, or every save looks stale.
func TestFileVersionSurvivesJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &FileService{}
	p, err := addProject(root)
	if err != nil {
		t.Fatal(err)
	}
	defer removeProject(p.ID)
	fc, err := svc.Read(p.ID, "a.php")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(fc)
	var js map[string]any // what JSON.parse gives the frontend
	if err := json.Unmarshal(raw, &js); err != nil {
		t.Fatal(err)
	}
	v, ok := js["version"].(string)
	if !ok || v != fc.Version {
		t.Fatalf("version after JSON = %#v, want %q", js["version"], fc.Version)
	}
	if _, err := svc.Write(p.ID, "a.php", "<?php // saved\n", v); err != nil {
		t.Fatalf("save with the version from JSON: %v", err)
	}
}
