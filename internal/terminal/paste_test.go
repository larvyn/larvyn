package terminal

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveImage(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("TMP", os.Getenv("TMPDIR")) // Windows
	png := []byte("\x89PNG\r\n\x1a\nrest-of-image")
	s := &TerminalService{}
	path, err := s.SaveImage(base64.StdEncoding.EncodeToString(png))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".png" || !strings.HasPrefix(path, pastedImageDir()) {
		t.Errorf("unexpected path %q", path)
	}
	if b, _ := os.ReadFile(path); string(b) != string(png) {
		t.Errorf("saved %q, want the image bytes", b)
	}

	if _, err := s.SaveImage(base64.StdEncoding.EncodeToString([]byte("#!/bin/sh\nrm -rf ~"))); err == nil {
		t.Error("saved data that is not an image")
	}
	if _, err := s.SaveImage("%%%"); err == nil {
		t.Error("accepted data that is not base64")
	}
}

func TestSaveImageCleansOldImages(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("TMP", os.Getenv("TMPDIR"))
	dir := pastedImageDir()
	os.MkdirAll(dir, 0o700)
	old := filepath.Join(dir, "paste-old.png")
	os.WriteFile(old, []byte("x"), 0o600)
	past := time.Now().Add(-2 * pastedImageTTL)
	os.Chtimes(old, past, past)

	s := &TerminalService{}
	if _, err := s.SaveImage(base64.StdEncoding.EncodeToString([]byte("GIF89a..."))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("an image older than a day was kept")
	}
}

func TestImageExt(t *testing.T) {
	cases := map[string]string{
		"\x89PNG\r\n\x1a\n...":         ".png",
		"\xff\xd8\xff\xe0...":          ".jpg",
		"GIF87a...":                    ".gif",
		"RIFF\x00\x00\x00\x00WEBPVP8 ": ".webp",
		"<svg></svg>":                  "",
		"":                             "",
	}
	for in, want := range cases {
		if got := imageExt([]byte(in)); got != want {
			t.Errorf("imageExt(%q) = %q, want %q", in, got, want)
		}
	}
}
