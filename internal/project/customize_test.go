package project

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"larvyn/internal/store"
)

func TestUpdateSavesIconAndColor(t *testing.T) {
	ps := &ProjectService{}
	p, err := ps.Add(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Remove(p.ID)

	p.Name = "  Shop  "
	p.Color = "#0ea5e9"
	p.Icon = "shopping-cart"
	if err := ps.Update(p); err != nil {
		t.Fatal(err)
	}
	got, _ := store.FindProject(p.ID)
	if got.Name != "Shop" || got.Color != "#0EA5E9" || got.Icon != "shopping-cart" {
		t.Fatalf("got %+v", got)
	}

	// Bad values keep the name and color and fall back to initials.
	bad := got
	bad.Name = " "
	bad.Color = "red; background: url(x)"
	bad.Icon = "<script>"
	if err := ps.Update(bad); err != nil {
		t.Fatal(err)
	}
	got, _ = store.FindProject(p.ID)
	if got.Name != "Shop" || got.Color != "#0EA5E9" || got.Icon != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestGithubOwner(t *testing.T) {
	cases := map[string]string{
		"git@github.com:laravel/framework.git":        "laravel",
		"https://github.com/ivanmercedes/larvyn":      "ivanmercedes",
		"https://user:tok@github.com/Acme-Co/app.git": "Acme-Co",
		"ssh://git@github.com/spatie/laravel-data":    "spatie",
		"git@gitlab.com:group/app.git":                "",
		"https://example.com/github.com/x/y":          "",
		"/home/me/repo":                               "",
		"":                                            "",
	}
	for in, want := range cases {
		if got := githubOwner(in); got != want {
			t.Errorf("githubOwner(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetCustomIcon(t *testing.T) {
	ps := &ProjectService{}
	p, err := ps.Add(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A 1x1 PNG.
	const px = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	if err := ps.SetCustomIcon(p.ID, "data:image/png;base64,"+px); err != nil {
		t.Fatal(err)
	}
	img, err := ps.Images(p.ID)
	if err != nil || !strings.HasPrefix(img.Custom, "data:image/png;base64,") {
		t.Fatalf("custom = %q, %v", img.Custom, err)
	}
	if err := ps.SetCustomIcon(p.ID, base64.StdEncoding.EncodeToString([]byte("<svg/>"))); err == nil {
		t.Fatal("a non-PNG was accepted")
	}
	if err := ps.Remove(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(customIconPath(p.ID)); !os.IsNotExist(err) {
		t.Fatal("the custom icon stayed after the project was removed")
	}
}
