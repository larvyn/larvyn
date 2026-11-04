package plugins

import (
	"archive/zip"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name, body string
	mode       fs.FileMode
}

// makeZip writes a zip with these entries and returns its path.
func makeZip(t *testing.T, entries ...entry) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "plugin.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	zw.Close()
	f.Close()
	return p
}

const indexHTML = "<!doctype html>"

func pluginEntries(top string) []entry {
	return []entry{{name: top + "larvyn.json", body: validManifest}, {name: top + "ui/index.html", body: indexHTML}}
}

func TestInstallZip(t *testing.T) {
	for name, entries := range map[string][]entry{
		"in a folder":         pluginEntries("acme.horizon/"),
		"at the top":          pluginEntries(""),
		"with Finder clutter": append(pluginEntries("acme.horizon/"), entry{name: "__MACOSX/acme.horizon/._larvyn.json", body: "x"}, entry{name: "acme.horizon/.DS_Store", body: "x"}),
	} {
		t.Run(name, func(t *testing.T) {
			reset(t)
			z := makeZip(t, entries...)
			s := &PluginService{}
			p, err := s.InspectZip(z)
			if err != nil || p.Manifest.ID != "acme.horizon" || p.Dir != z {
				t.Fatalf("inspect: %+v %v", p, err)
			}
			if _, err := s.InstallZip(z, []string{"project.read"}); err == nil {
				t.Fatal("installed without every permission")
			}
			got, err := s.InstallZip(z, horizonPerms)
			if err != nil || !got.Enabled {
				t.Fatalf("install: %+v %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(Dir(), "acme.horizon", "ui", "index.html")); err != nil {
				t.Error("files were not installed")
			}
			if _, err := os.Stat(filepath.Join(Dir(), "acme.horizon", ".DS_Store")); err == nil {
				t.Error("Finder files must not be installed")
			}
		})
	}
}

func TestInstallZipRefuses(t *testing.T) {
	cases := map[string][]entry{
		"zip slip":         append(pluginEntries("acme.horizon/"), entry{name: "../../evil.sh", body: "x"}),
		"nested zip slip":  append(pluginEntries("acme.horizon/"), entry{name: "acme.horizon/../../evil", body: "x"}),
		"absolute path":    append(pluginEntries("acme.horizon/"), entry{name: "/tmp/evil", body: "x"}),
		"windows drive":    append(pluginEntries("acme.horizon/"), entry{name: "C:/evil", body: "x"}),
		"backslash":        append(pluginEntries("acme.horizon/"), entry{name: `acme.horizon\..\..\evil`, body: "x"}),
		"symlink":          append(pluginEntries("acme.horizon/"), entry{name: "acme.horizon/ui/link", body: "/etc/passwd", mode: fs.ModeSymlink | 0o777}),
		"two plugins":      append(pluginEntries("acme.horizon/"), pluginEntries("acme.other/")...),
		"no manifest":      {{name: "acme.horizon/ui/index.html", body: indexHTML}},
		"duplicate entry":  append(pluginEntries("acme.horizon/"), entry{name: "acme.horizon/ui/index.html", body: "again"}),
		"invalid manifest": {{name: "acme.horizon/larvyn.json", body: "{}"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			reset(t)
			z := makeZip(t, entries...)
			_, err := (&PluginService{}).InstallZip(z, horizonPerms)
			if err == nil {
				t.Error("must be refused")
			}
			t.Logf("%s: %v", name, err)
			if _, err := os.Stat(filepath.Join(Dir(), "acme.horizon")); err == nil {
				t.Error("nothing may be installed")
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(Dir()), "evil.sh")); err == nil {
				t.Error("a file was written outside")
			}
		})
	}
	reset(t)
	notZip := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(notZip, []byte("not a zip"), 0o644)
	if _, err := (&PluginService{}).InspectZip(notZip); err == nil || !strings.Contains(err.Error(), "not a zip") {
		t.Errorf("not a zip: %v", err)
	}
}

// What the archive claims doesn't matter: the bytes actually written are
// counted.
func TestInstallZipBomb(t *testing.T) {
	reset(t)
	old := maxInstallBytes
	maxInstallBytes = 1 << 10
	defer func() { maxInstallBytes = old }()
	z := makeZip(t, append(pluginEntries("acme.horizon/"), entry{name: "acme.horizon/big.txt", body: strings.Repeat("0", 4<<10)})...)
	if _, err := (&PluginService{}).InstallZip(z, horizonPerms); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %v", err)
	}
}

func TestUnpackCleansUp(t *testing.T) {
	z := makeZip(t, pluginEntries("acme.horizon/")...)
	dir, cleanup, err := unpackPlugin(z)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(dir); err == nil {
		t.Error("the temporary folder is left behind")
	}
	// A failed unpack cleans up after itself too.
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "larvyn-plugin-zip-*"))
	unpackPlugin(makeZip(t, entry{name: "../evil", body: "x"}))
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "larvyn-plugin-zip-*"))
	if len(after) > len(before) {
		t.Errorf("a failed unpack left %v", after)
	}
}

// What Export writes, Install from .zip reads.
func TestExportThenInstallZip(t *testing.T) {
	reset(t)
	src := t.TempDir()
	writePlugin(t, src, validManifest)
	s := &PluginService{}
	if _, err := s.Install(src, horizonPerms); err != nil {
		t.Fatal(err)
	}
	p, _ := byFolder("acme.horizon")
	z := filepath.Join(t.TempDir(), zipName(p))
	if err := ExportTo(p, z); err != nil {
		t.Fatal(err)
	}
	s.Uninstall("acme.horizon")
	if got, err := s.InstallZip(z, horizonPerms); err != nil || !got.Enabled {
		t.Errorf("reinstall from the export: %+v %v", got, err)
	}
}
