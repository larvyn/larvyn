package dotenv

import "testing"

func TestParseEnvLine(t *testing.T) {
	cases := map[string][2]string{
		`APP_NAME="My App"`:        {"APP_NAME", "My App"},
		`DB_PASSWORD='s3cr#t'`:     {"DB_PASSWORD", "s3cr#t"},
		`CACHE_STORE=redis # note`: {"CACHE_STORE", "redis"},
		`export FOO=bar`:           {"FOO", "bar"},
		`EMPTY=`:                   {"EMPTY", ""},
	}
	for line, want := range cases {
		k, v, ok := ParseLine(line)
		if !ok || k != want[0] || v != want[1] {
			t.Errorf("%q: got %q=%q ok=%v", line, k, v, ok)
		}
	}
	if _, _, ok := ParseLine("# comment"); ok {
		t.Error("comment parsed as a key")
	}
}
