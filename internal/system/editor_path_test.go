package system

import (
	"strings"
	"testing"
)

// Locations from logs and stack traces can be any text. One like "+!cmd"
// would run as a command in nvim, so only absolute paths are opened (#52).
func TestOpenInEditorRejectsNonAbsolutePaths(t *testing.T) {
	s := &SystemService{}
	for _, p := range []string{"+!touch /tmp/pwned", "-c", "--help", "app/Models/User.php", ""} {
		err := s.OpenInEditor(p, 1)
		if err == nil || !strings.Contains(err.Error(), "not an absolute path") {
			t.Errorf("OpenInEditor(%q) = %v, want refusal", p, err)
		}
	}
}

func TestNvimEndsOptionsBeforeTheFile(t *testing.T) {
	tmpl := editors["nvim"]
	if len(tmpl) < 2 || tmpl[len(tmpl)-2] != "--" || tmpl[len(tmpl)-1] != "{file}" {
		t.Fatalf("nvim template = %v, want -- before {file}", tmpl)
	}
}
