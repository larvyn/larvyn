package project

import "testing"

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
