package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/store"
)

var slowCancelled = make(chan bool, 1)

type report struct {
	Failed int `json:"failed"`
}

func (report) Text() string  { return "2 tests failed" }
func (report) ExitCode() int { return 3 }

type row struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Running bool   `json:"running"`
}

func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "lv")
	os.Setenv("LARVYN_HOME", home)
	os.Setenv("LARVYN_SESSION", "me")
	store.Reload()
	api.Register("demo.rows", "Rows", func() []row {
		return []row{{"a1", "Shell", true}, {"b22", "artisan serve", false}}
	})
	api.Register("demo.whoami", "Who calls", func(c api.Caller) string { return c.Session })
	api.Register("demo.add", "Add", func(a, b int) int { return a + b }, "a: first", "b?: second")
	api.MarkReadOnly("demo.whoami")
	api.Register("demo.report", "Report", func() report { return report{} })
	api.Register("demo.slow", "Slow", func(c api.Caller) string {
		select {
		case <-c.Ctx.Done():
			slowCancelled <- true
		case <-time.After(10 * time.Second):
			slowCancelled <- false
		}
		return "finished"
	})
	srv, err := api.Listen()
	if err != nil {
		panic(err)
	}
	code := m.Run()
	srv.Close()
	os.RemoveAll(home)
	os.Exit(code)
}

func runCLI(args ...string) (stdout, stderr string, code int) {
	var o, e bytes.Buffer
	code = Run(args, &o, &e)
	return o.String(), e.String(), code
}

func TestCommands(t *testing.T) {
	out, errOut, code := runCLI("demo", "rows")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	want := "ID   TITLE          RUNNING\na1   Shell          yes\nb22  artisan serve  no\n"
	if out != want {
		t.Errorf("table:\n%s\nwant:\n%s", out, want)
	}
	if out, _, _ := runCLI("demo", "whoami"); out != "me\n" {
		t.Errorf("caller: %q", out)
	}
	if out, _, _ := runCLI("demo", "add", "2", "--b", "3"); out != "5\n" {
		t.Errorf("add: %q", out)
	}
	if out, _, _ := runCLI("api", "call", "demo.add", `{"a":1,"b":1}`); out != "2\n" {
		t.Errorf("api call: %q", out)
	}
	out, _, _ = runCLI("demo", "rows", "--json")
	var rows []row
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 2 {
		t.Errorf("--json: %q %v", out, err)
	}
}

func TestErrorsExitNonZero(t *testing.T) {
	_, errOut, code := runCLI("demo", "add")
	if code != 1 || !strings.Contains(errOut, "missing a") {
		t.Errorf("missing arg: %d %q", code, errOut)
	}
	if _, _, code := runCLI("api", "call"); code != 2 {
		t.Errorf("api call without a method: %d", code)
	}
}

func TestHelp(t *testing.T) {
	out, _, code := runCLI("help")
	if code != 0 || !strings.Contains(out, "demo add <a> [--b int]") || !strings.Contains(out, "      a  first") {
		t.Errorf("help:\n%s", out)
	}
	// help for a method shows it and doesn't run it.
	out, _, _ = runCLI("help", "demo", "rows")
	if strings.Contains(out, "a1") || !strings.Contains(out, "demo rows") {
		t.Errorf("help demo rows:\n%s", out)
	}
	// A group with no method lists its methods.
	if out, _, _ := runCLI("demo"); !strings.Contains(out, "demo whoami") {
		t.Errorf("group:\n%s", out)
	}
}

func TestIsCommand(t *testing.T) {
	for args, want := range map[string]bool{"": false, "context": true, "--help": true, "-psn_0_123": false, "help": true} {
		if got := IsCommand(strings.Fields(args)); got != want {
			t.Errorf("IsCommand(%q) = %v", args, got)
		}
	}
}

func TestHuman(t *testing.T) {
	cases := map[string]string{
		`"text"`:    "text\n",
		`"line\n"`:  "line\n",
		`null`:      "",
		`[]`:        "(none)\n",
		`["a","b"]`: "a\nb\n",
		`42`:        "42\n",
		`{"b":1,"a":"x","empty":"","list":["p","q"]}`: "b:    1\na:    x\nlist: p, q\n",
		`{"name":"n","items":[{"k":"v"}]}`:            "name: n\nitems:\n  K\n  v\n",
	}
	for in, want := range cases {
		if got := Human(json.RawMessage(in)); got != want {
			t.Errorf("Human(%s) = %q, want %q", in, got, want)
		}
	}
}
