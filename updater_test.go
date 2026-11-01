package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.1.0-beta.5", "v0.1.0-beta.4", 1},
		{"v0.1.0-beta.10", "v0.1.0-beta.9", 1},
		{"v0.1.0", "v0.1.0-beta.9", 1},
		{"v0.1.0-beta.3", "v0.1.0", -1},
		{"v0.2.0-beta.1", "v0.1.9", 1},
		{"v1.0.0", "v1.0", 0},
		{"v0.1.0-beta.4", "v0.1.0-beta.4", 0},
		{"v0.1.0-rc.1", "v0.1.0-beta.9", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("larvyn")
	sum := sha256.Sum256(data)
	sums := []byte(hex.EncodeToString(sum[:]) + "  larvyn-linux-amd64.tar.gz\nabc  other\n")
	if err := verifyChecksum(data, sums, "larvyn-linux-amd64.tar.gz"); err != nil {
		t.Fatal(err)
	}
	if verifyChecksum([]byte("tampered"), sums, "larvyn-linux-amd64.tar.gz") == nil {
		t.Fatal("tampered data passed")
	}
	if verifyChecksum(data, sums, "missing.exe") == nil {
		t.Fatal("missing asset passed")
	}
}

func tgz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestUntar(t *testing.T) {
	dir := t.TempDir()
	if err := untar(tgz(t, map[string]string{"larvyn": "bin", "larvyn.png": "png"}), dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "larvyn")); string(b) != "bin" {
		t.Fatalf("larvyn = %q", b)
	}
	if untar(tgz(t, map[string]string{"../escape": "x"}), t.TempDir()) == nil {
		t.Fatal("path traversal was extracted")
	}
}
