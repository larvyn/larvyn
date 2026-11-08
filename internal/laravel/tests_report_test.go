package laravel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTestResultsIncompleteReport(t *testing.T) {
	t.Setenv("LARVYN_HOME", t.TempDir())
	l := &LaravelService{}
	for name, body := range map[string]string{"empty": "", "truncated": `<?xml version="1.0"?><testsuites><testsuite name="x">`} {
		if err := os.MkdirAll(filepath.Dir(JUnitPath("p")), 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(JUnitPath("p"), []byte(body), 0o644)
		r, err := l.TestResults("p")
		if err != nil || !r.Incomplete || r.Exists {
			t.Fatalf("%s report: got %+v, %v; want incomplete without error", name, r, err)
		}
	}
}
