package laravel

import (
	"os"
	"strings"
	"testing"

	"larvyn/internal/store"
)

func TestParseLog(t *testing.T) {
	log := `[2026-10-30 12:00:01] local.ERROR: Undefined variable $user {"exception":"[object] (ErrorException(code: 0): Undefined variable $user at /app/app/Http/Controllers/HomeController.php:14)
[stacktrace]
#0 /app/vendor/laravel/framework/src/Foundation/Bootstrap/HandleExceptions.php(256): handleError()
#1 /app/app/Http/Controllers/HomeController.php(14): foo()
"}
[2026-10-30 12:00:02] local.INFO: User logged in {"id":1}
`
	entries := parseLog([]byte(log), 0)
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	e := entries[0]
	if e.Level != "error" || e.Exception != "ErrorException" || e.Line != 14 || !strings.HasSuffix(e.File, "HomeController.php") {
		t.Errorf("bad error entry: %+v", e)
	}
	if !strings.Contains(e.Stack, "#1 /app/app/Http") || strings.Contains(e.Stack, `"}`) {
		t.Errorf("bad stack: %q", e.Stack)
	}
	win := parseLog([]byte(`[2026-10-30 12:00:01] local.ERROR: boom {"exception":"[object] (Illuminate\\View\\ViewException(code: 0): boom at C:\\app\\x.php:3)"}`+"\n"), 0)
	if win[0].Exception != `Illuminate\View\ViewException` || win[0].File != `C:\app\x.php` {
		t.Errorf("escaped names not unescaped: %+v", win[0])
	}
	if entries[1].Level != "info" || entries[1].Message != "User logged in" || entries[1].Context != `{"id":1}` {
		t.Errorf("bad info entry: %+v", entries[1])
	}
}

func TestTestResults(t *testing.T) {
	p, err := addProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(store.HomeDir()+"/junit", 0o755)
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="Tests\Feature" tests="3">
    <testsuite name="Tests\Feature\PostTest" file="tests/Feature/PostTest.php" tests="3">
      <testcase name="it lists posts" file="tests/Feature/PostTest.php::it lists posts" class="Tests\Feature\PostTest" time="0.12"/>
      <testcase name="it stores a post" file="tests/Feature/PostTest.php::it stores a post" class="Tests\Feature\PostTest" time="0.30">
        <failure type="PHPUnit\Framework\ExpectationFailedException">it stores a postExpected response status code [201] but received 422.
at tests/Feature/PostTest.php:21</failure>
      </testcase>
      <testcase name="it is skipped" class="Tests\Feature\PostTest" time="0"><skipped/></testcase>
    </testsuite>
  </testsuite>
</testsuites>`
	os.WriteFile(junitPath(p.ID), []byte(xml), 0o644)
	r, err := (&LaravelService{}).TestResults(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Tests != 3 || r.Passed != 1 || r.Failed != 1 || r.Skipped != 1 {
		t.Fatalf("counts: %+v", r)
	}
	f := r.Cases[1]
	if f.Status != "failed" || !strings.HasPrefix(f.Message, "Expected response") || !strings.Contains(f.Message, "422") || f.File != "tests/Feature/PostTest.php" || f.Line != 21 {
		t.Errorf("failure: %+v", f)
	}
}
