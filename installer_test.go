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

func TestRepairAppURL(t *testing.T) {
	cases := []struct {
		in, want string
		changed  bool
	}{
		{"APP_NAME=Laravel\nAPP_URL=http://localhost:8000:8000\nDB_CONNECTION=sqlite\n", "APP_NAME=Laravel\nAPP_URL=http://localhost:8000\nDB_CONNECTION=sqlite\n", true},
		{"APP_URL=\"http://localhost:8000:8000\"\n", "APP_URL=\"http://localhost:8000\"\n", true},
		{"APP_URL=https://app.test:443:443\n", "APP_URL=https://app.test:443\n", true},
		// Fine as they are.
		{"APP_URL=http://localhost:8000\n", "APP_URL=http://localhost:8000\n", false},
		{"APP_URL=http://checklist-app.test\n", "APP_URL=http://checklist-app.test\n", false},
		// Two different ports is not this mistake; leave it for the user.
		{"APP_URL=http://localhost:8000:9000\n", "APP_URL=http://localhost:8000:9000\n", false},
		// Only APP_URL is touched.
		{"ASSET_URL=http://localhost:8000:8000\n", "ASSET_URL=http://localhost:8000:8000\n", false},
	}
	for _, c := range cases {
		got, changed := repairAppURL(c.in)
		if got != c.want || changed != c.changed {
			t.Errorf("repairAppURL(%q) = %q, %v; want %q, %v", c.in, got, changed, c.want, c.changed)
		}
	}
}
