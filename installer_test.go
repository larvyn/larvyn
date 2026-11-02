package main

import (
	"strings"
	"testing"
)

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"5.15.0", "5.25.2", true},
		{"5.25.1", "5.25.2", true},
		{"5.25.2", "5.25.2", false},
		{"5.32.0", "5.25.2", false},
		{"5.100.0", "5.25.2", false},
		{"5.25", "5.25.2", true},
		{"6.0", "5.25.2", false},
	}
	for _, c := range cases {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestInstallerOutdated(t *testing.T) {
	if msg := installerOutdated("5.15.0"); !strings.Contains(msg, "5.15.0") || !strings.Contains(msg, minLaravelInstaller) {
		t.Errorf("5.15.0 should be outdated, got %q", msg)
	}
	for _, v := range []string{"", "5.25.2", "5.32.0", "6.0.0"} {
		if msg := installerOutdated(v); msg != "" {
			t.Errorf("%q should not be outdated, got %q", v, msg)
		}
	}
}
