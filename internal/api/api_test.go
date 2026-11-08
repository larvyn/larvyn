package api

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"larvyn/internal/store"
)

func TestMain(m *testing.M) {
	// A short path: unix socket paths are limited to about 100 bytes.
	home, _ := os.MkdirTemp("", "lv")
	os.Setenv("LARVYN_HOME", home)
	store.Reload()
	register()
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func register() {
	Register("test.echo", "Echo", func(c Caller, text string, times int, upper bool) string {
		if upper {
			text = strings.ToUpper(text)
		}
		return strings.Repeat(text, max(times, 1)) + "@" + c.Session
	}, "text: what to echo", "times?: how often", "upper?: shout")
	Register("test.fail", "Fail", func() error { return errors.New("it failed") })
	Register("test.list", "List", func(words []string) []string { return words }, "words")
	Register("test.panic", "Panic", func() int { panic("boom") })
}

func call(t *testing.T, argv ...string) Response {
	t.Helper()
	return Handle(Request{Argv: argv, Caller: Caller{Session: "s1"}})
}

func result(t *testing.T, r Response) any {
	t.Helper()
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	var v any
	if err := json.Unmarshal(r.Result, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestArgvBecomesParams(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"test", "echo", "hi"}, "hi@s1"},
		{[]string{"test", "echo", "hi", "2"}, "hihi@s1"},
		{[]string{"test", "echo", "hi", "--times", "3"}, "hihihi@s1"},
		{[]string{"test", "echo", "--times=2", "hi"}, "hihi@s1"},
		// A bool flag takes no value: "hi" after it is the text.
		{[]string{"test", "echo", "--upper", "hi"}, "HI@s1"},
		{[]string{"test", "echo", "hi", "--upper=false"}, "hi@s1"},
		// -- ends the flags.
		{[]string{"test", "echo", "--", "--upper"}, "--upper@s1"},
		// The name as help prints it, with a dot.
		{[]string{"test.echo", "hi", "2"}, "hihi@s1"},
	}
	for _, c := range cases {
		if got := result(t, call(t, c.argv...)); got != c.want {
			t.Errorf("%v = %v, want %q", c.argv, got, c.want)
		}
	}
}

func TestBadArgv(t *testing.T) {
	for _, argv := range [][]string{
		{"test", "echo"},                     // missing text
		{"test", "echo", "a", "1", "x", "y"}, // too many
		{"test", "echo", "a", "--times", "many"},
		{"test", "echo", "a", "--nope"},
		{"test", "echo", "a", "--times"},
		{"nothing", "here"},
	} {
		if r := call(t, argv...); r.Error == "" {
			t.Errorf("%v: no error", argv)
		}
	}
}

func TestErrorsAndPanics(t *testing.T) {
	if r := call(t, "test", "fail"); r.Error != "it failed" {
		t.Errorf("fail: %q", r.Error)
	}
	if r := call(t, "test", "panic"); !strings.Contains(r.Error, "boom") {
		t.Errorf("panic: %q", r.Error)
	}
}

func TestCallByNameWithJSON(t *testing.T) {
	r := Handle(Request{Method: "test.echo", Params: json.RawMessage(`{"text":"a","times":2,"upper":true}`)})
	if got := result(t, r); got != "AA@" {
		t.Errorf("got %v", got)
	}
	r = Handle(Request{Method: "test.list", Params: json.RawMessage(`{"words":["a","b"]}`)})
	if got, _ := json.Marshal(result(t, r)); string(got) != `["a","b"]` {
		t.Errorf("list: %s", got)
	}
	if r := Handle(Request{Method: "test.echo", Params: json.RawMessage(`{"text":1}`)}); r.Error == "" {
		t.Error("a number for a string param was taken")
	}
	if r := Handle(Request{Method: "test.echo", Params: json.RawMessage(`{"txt":"a"}`)}); r.Error == "" {
		t.Error("an unknown param was taken")
	}
}

func TestListFlagTakesJSONOrOneWord(t *testing.T) {
	if got, _ := json.Marshal(result(t, call(t, "test", "list", `["a","b c"]`))); string(got) != `["a","b c"]` {
		t.Errorf("json list: %s", got)
	}
	if got, _ := json.Marshal(result(t, call(t, "test", "list", "a b"))); string(got) != `["a b"]` {
		t.Errorf("word: %s", got)
	}
}

func TestUsage(t *testing.T) {
	if r := call(t); len(r.Usage) == 0 {
		t.Error("no command: no usage")
	}
	r := call(t, "test")
	if len(r.Usage) != 4 {
		t.Errorf("group: %d methods", len(r.Usage))
	}
	r = Handle(Request{Argv: []string{"test", "echo"}, Help: true})
	if len(r.Usage) != 1 || r.Usage[0].Usage() != "test.echo <text> [--times int] [--upper]" {
		t.Errorf("help: %+v", r.Usage)
	}
	if r.Usage[0].Params[1].Type != "int" || !r.Usage[0].Params[1].Optional || r.Usage[0].Params[0].Doc != "what to echo" {
		t.Errorf("params: %+v", r.Usage[0].Params)
	}
}

func TestRegisterChecksTheSignature(t *testing.T) {
	for name, fn := range map[string]func(){
		"not a func":     func() { Register("x.a", "", 1) },
		"params count":   func() { Register("x.b", "", func(a string) {}) },
		"second result":  func() { Register("x.c", "", func() (int, int) { return 0, 0 }) },
		"registered dup": func() { Register("test.echo", "", func() {}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			fn()
		}()
	}
}

func TestServerRoundTrip(t *testing.T) {
	srv, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if runtime.GOOS != "windows" {
		st, err := os.Stat(SocketPath())
		if err != nil || st.Mode().Perm()&0o077 != 0 {
			t.Errorf("socket mode %v %v", st.Mode(), err)
		}
		st, err = os.Stat(TokenPath())
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("token mode %v %v", st.Mode(), err)
		}
	}
	r, err := Dial(Request{Argv: []string{"test", "echo", "x"}, Caller: Caller{Session: "abc"}})
	if err != nil || result(t, r) != "x@abc" {
		t.Fatalf("dial: %+v %v", r, err)
	}
	// A second app with the same state folder leaves the first alone.
	if _, err := Listen(); err == nil {
		t.Error("a second Listen took the socket")
	}
	if r, err := Dial(Request{Argv: []string{"test", "echo", "y"}}); err != nil || r.Error != "" {
		t.Errorf("after a second Listen: %+v %v", r, err)
	}
}

func TestServerRejectsABadToken(t *testing.T) {
	srv, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	c, err := net.Dial("unix", SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	b, _ := json.Marshal(Request{Token: "nope", Argv: []string{"test", "echo", "x"}})
	c.Write(append(b, '\n'))
	var r Response
	if err := json.NewDecoder(c).Decode(&r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Error, "invalid token") || r.Result != nil {
		t.Errorf("bad token got %+v", r)
	}
}

func TestCloseRemovesSocketAndToken(t *testing.T) {
	srv, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()
	for _, p := range []string{SocketPath(), TokenPath()} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s left behind", filepath.Base(p))
		}
	}
	if _, err := Dial(Request{}); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("dial after close: %v", err)
	}
}

func TestStaleSocketIsReplaced(t *testing.T) {
	// A socket file from a run that crashed: nothing listens on it.
	ln, err := net.Listen("unix", SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	srv, err := Listen()
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	srv.Close()
}
