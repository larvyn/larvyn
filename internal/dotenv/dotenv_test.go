package dotenv

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	orig := "APP_NAME=Shop\n# mail\nMAIL_MAILER=log\nMAIL_PORT=2525\n"
	os.WriteFile(path, []byte(orig), 0o640)
	before, err := Set(path, map[string]string{"MAIL_MAILER": "smtp", "MAIL_PORT": "1025", "MAIL_HOST": "127.0.0.1", "MAIL_FROM_NAME": "My Shop"})
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != orig {
		t.Errorf("before = %q", before)
	}
	b, _ := os.ReadFile(path)
	want := "APP_NAME=Shop\n# mail\nMAIL_MAILER=smtp\nMAIL_PORT=1025\nMAIL_FROM_NAME=\"My Shop\"\nMAIL_HOST=127.0.0.1\n"
	if string(b) != want {
		t.Errorf("got:\n%s\nwant:\n%s", b, want)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o640 {
		t.Errorf("mode changed to %v", st.Mode().Perm())
	}
	if got := Read(path)["MAIL_FROM_NAME"]; got != "My Shop" {
		t.Errorf("quoted value reads back as %q", got)
	}
}
