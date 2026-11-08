// Package api is the local API that lets agents, the larvyn CLI and (later)
// the MCP server drive the running app. It listens on a unix socket in the
// state folder and checks a token that the app writes there at start; it is
// never an HTTP server on the network (see #50).
//
// The package only knows how to describe and call methods. main registers
// them from the services (see api.go there), so this package imports nothing
// of Larvyn's except store, and the CLI that imports it stays small.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Caller is who makes a call: the terminal the CLI runs in (LARVYN_SESSION,
// set in every terminal Larvyn opens) and its working directory. A method
// that takes a Caller as its first parameter gets it filled in; it is not
// one of the method's params.
type Caller struct {
	Session string `json:"session,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	// Ctx ends when the client goes away (Ctrl+C on larvyn stack wait), so
	// a method that waits can stop. Never nil in a method.
	Ctx context.Context `json:"-"`
}

// Param is one of a method's arguments. A name ending in "?" is optional:
// it gets its zero value when left out. A last param ending in "..." is a
// []string that takes the rest of the command's words as they are, flags
// included: `larvyn artisan migrate --force`.
type Param struct {
	Name string `json:"name"`
	Doc  string `json:"doc,omitempty"`
	// Type is filled in from the function's signature.
	Type     string `json:"type"`
	Optional bool   `json:"optional,omitempty"`
	Variadic bool   `json:"variadic,omitempty"`
}

// Method is a function agents can call.
type Method struct {
	Name   string  `json:"name"`
	Doc    string  `json:"doc"`
	Params []Param `json:"params"`

	fn         reflect.Value
	wantCaller bool
	types      []reflect.Type
}

var callerType = reflect.TypeFor[Caller]()
var errorType = reflect.TypeFor[error]()

var (
	mu      sync.RWMutex
	methods = map[string]*Method{}
)

// Register adds a method. fn takes an optional Caller and then one argument
// per param, in order, and returns a result, an error, or both. params are
// "name" or "name?" (optional), each followed by a short description after
// a colon: "lines?: how many lines, 100 by default". It panics on a
// mismatch, which is a programming error found at start.
func Register(name, doc string, fn any, params ...string) {
	v := reflect.ValueOf(fn)
	t := v.Type()
	if t.Kind() != reflect.Func {
		panic("api.Register " + name + ": not a function")
	}
	m := &Method{Name: name, Doc: doc, fn: v}
	in := 0
	if t.NumIn() > 0 && t.In(0) == callerType {
		m.wantCaller = true
		in = 1
	}
	if t.NumIn()-in != len(params) {
		panic(fmt.Sprintf("api.Register %s: %d params named for %d arguments", name, len(params), t.NumIn()-in))
	}
	switch t.NumOut() {
	case 0:
	case 1, 2:
		if t.NumOut() == 2 && t.Out(1) != errorType {
			panic("api.Register " + name + ": the second result must be an error")
		}
	default:
		panic("api.Register " + name + ": too many results")
	}
	for i, p := range params {
		pname, pdoc, _ := strings.Cut(p, ":")
		pname = strings.TrimSpace(pname)
		opt := strings.HasSuffix(pname, "?")
		pname = strings.TrimSuffix(pname, "?")
		variadic := strings.HasSuffix(pname, "...")
		pname = strings.TrimSuffix(pname, "...")
		pt := t.In(in + i)
		if variadic && (i != len(params)-1 || pt != reflect.TypeFor[[]string]()) {
			panic("api.Register " + name + ": only a last []string param can take the rest of the words")
		}
		m.Params = append(m.Params, Param{Name: pname, Doc: strings.TrimSpace(pdoc), Type: typeName(pt), Optional: opt, Variadic: variadic})
		m.types = append(m.types, pt)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := methods[name]; dup {
		panic("api.Register " + name + ": registered twice")
	}
	methods[name] = m
}

// Methods lists every method, sorted by name.
func Methods() []Method {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Method, 0, len(methods))
	for _, m := range methods {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func lookup(name string) *Method {
	mu.RLock()
	defer mu.RUnlock()
	return methods[name]
}

// Usage is a method's one-line synopsis: "terminal.read <id> [--lines N]".
func (m Method) Usage() string {
	parts := []string{m.Name}
	for _, p := range m.Params {
		if p.Variadic {
			if p.Optional {
				parts = append(parts, "["+p.Name+"...]")
			} else {
				parts = append(parts, "<"+p.Name+"...>")
			}
			continue
		}
		if p.Optional {
			if p.Type == "bool" {
				parts = append(parts, "[--"+p.Name+"]")
			} else {
				parts = append(parts, "[--"+p.Name+" "+p.Type+"]")
			}
		} else {
			parts = append(parts, "<"+p.Name+">")
		}
	}
	return strings.Join(parts, " ")
}

func typeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "[]" + typeName(t.Elem())
	}
	return "json"
}

// Call runs a method. params is a JSON object keyed by param name (from
// `larvyn api call` and MCP); args and flags are the CLI's words, as
// strings: args fill the params in order, and flags (--name value)
// any param by name.
func Call(name string, caller Caller, params json.RawMessage, args []string, flags map[string]string) (any, error) {
	m := lookup(name)
	if m == nil {
		return nil, fmt.Errorf("unknown method %q (larvyn api list shows them)", name)
	}
	vals := make([]reflect.Value, len(m.Params))
	set := make([]bool, len(m.Params))
	if len(params) > 0 && string(params) != "null" {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(params, &obj); err != nil {
			return nil, errors.New("params must be a JSON object")
		}
		for k, raw := range obj {
			i := m.index(k)
			if i < 0 {
				return nil, fmt.Errorf("%s has no param %q", name, k)
			}
			v := reflect.New(m.types[i])
			if err := json.Unmarshal(raw, v.Interface()); err != nil {
				return nil, fmt.Errorf("%s: %v", k, err)
			}
			vals[i], set[i] = v.Elem(), true
		}
	}
	// Flags first: the arguments then fill the params no flag named.
	for k, s := range flags {
		i := m.index(k)
		if i < 0 {
			return nil, fmt.Errorf("%s has no option --%s", name, k)
		}
		v, err := fromString(m.types[i], s)
		if err != nil {
			return nil, fmt.Errorf("--%s: %v", k, err)
		}
		vals[i], set[i] = v, true
	}
	next := 0
	for _, a := range args {
		for next < len(m.Params) && set[next] {
			next++
		}
		if next == len(m.Params) {
			return nil, fmt.Errorf("too many arguments: %s", m.Usage())
		}
		if m.Params[next].Variadic {
			if !vals[next].IsValid() {
				vals[next] = reflect.ValueOf([]string{})
			}
			vals[next] = reflect.Append(vals[next], reflect.ValueOf(a))
			continue
		}
		v, err := fromString(m.types[next], a)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", m.Params[next].Name, err)
		}
		vals[next], set[next] = v, true
	}
	for i, p := range m.Params {
		if p.Variadic && vals[i].IsValid() {
			set[i] = true
		}
	}
	in := make([]reflect.Value, 0, len(vals)+1)
	if caller.Ctx == nil {
		caller.Ctx = context.Background()
	}
	if m.wantCaller {
		in = append(in, reflect.ValueOf(caller))
	}
	for i, p := range m.Params {
		if !set[i] {
			if !p.Optional {
				return nil, fmt.Errorf("missing %s: %s", p.Name, m.Usage())
			}
			vals[i] = reflect.Zero(m.types[i])
		}
		in = append(in, vals[i])
	}
	return m.invoke(in)
}

func (m *Method) invoke(in []reflect.Value) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s failed: %v", m.Name, r)
		}
	}()
	out := m.fn.Call(in)
	switch len(out) {
	case 1:
		if out[0].Type() == errorType {
			if e, _ := out[0].Interface().(error); e != nil {
				return nil, e
			}
			return nil, nil
		}
		return out[0].Interface(), nil
	case 2:
		if e, _ := out[1].Interface().(error); e != nil {
			return nil, e
		}
		return out[0].Interface(), nil
	}
	return nil, nil
}

func (m *Method) index(name string) int {
	for i, p := range m.Params {
		if p.Name == name {
			return i
		}
	}
	return -1
}

// fromString turns a CLI word into a param's type. A bare flag (--enter) is
// "" and means true.
func fromString(t reflect.Type, s string) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		if s == "" {
			v.SetBool(true)
			break
		}
		b, err := strconv.ParseBool(s)
		if err != nil {
			return v, fmt.Errorf("%q is not true or false", s)
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return v, fmt.Errorf("%q is not a number", s)
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return v, fmt.Errorf("%q is not a number", s)
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return v, fmt.Errorf("%q is not a number", s)
		}
		v.SetFloat(f)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String && !strings.HasPrefix(strings.TrimSpace(s), "[") {
			// --args "a b" is not split: a list of words is given as JSON.
			v.Set(reflect.ValueOf([]string{s}).Convert(t))
			break
		}
		fallthrough
	default:
		p := reflect.New(t)
		if err := json.Unmarshal([]byte(s), p.Interface()); err != nil {
			return v, fmt.Errorf("not valid JSON: %v", err)
		}
		v = p.Elem()
	}
	return v, nil
}

// Resolve finds the method a CLI command names: the longest run of leading
// words that is a method ("terminal read abc" → "terminal.read", with "abc"
// left as an argument). ok is false when no prefix is a method.
func Resolve(words []string) (name string, rest []string, ok bool) {
	for n := len(words); n > 0; n-- {
		cand := strings.Join(words[:n], ".")
		if lookup(cand) != nil {
			return cand, words[n:], true
		}
	}
	return "", words, false
}

// Group lists the methods under a prefix ("terminal" → terminal.list…), for
// a command that names a group and no method.
func Group(prefix string) []Method {
	var out []Method
	for _, m := range Methods() {
		if strings.HasPrefix(m.Name, prefix+".") {
			out = append(out, m)
		}
	}
	return out
}

// leadingWords are the words before the first flag: where the method name is.
func leadingWords(argv []string) []string {
	for i, a := range argv {
		if strings.HasPrefix(a, "-") {
			return argv[:i]
		}
	}
	return argv
}

// parseArgv splits a command's words into arguments and --flags. A bool
// flag takes no value unless it is written --name=false; any other flag
// takes the next word. "--" ends the flags, so text starting with a dash
// can be passed as an argument.
//
// The larvyn command's own --json and --help go to opts, except among the
// words of a variadic param, which are the command's: `artisan route:list
// --json` gives artisan its --json. A "--" in front of those words is
// dropped.
func parseArgv(m *Method, argv []string, opts *cliFlags) (args []string, flags map[string]string, err error) {
	flags = map[string]string{}
	// The words before a variadic param, after which every word is its own.
	fixed := -1
	if n := len(m.Params); n > 0 && m.Params[n-1].Variadic {
		fixed = n - 1
	}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if fixed >= 0 && len(args) >= fixed {
			rest := argv[i:]
			if rest[0] == "--" {
				rest = rest[1:]
			}
			args = append(args, rest...)
			break
		}
		if opts != nil && opts.take(a) {
			continue
		}
		if a == "--" {
			args = append(args, argv[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") || len(a) == 2 {
			args = append(args, a)
			continue
		}
		name, val, hasVal := strings.Cut(a[2:], "=")
		j := m.index(name)
		if j < 0 {
			return nil, nil, fmt.Errorf("%s has no option --%s: %s", m.Name, name, m.Usage())
		}
		if !hasVal && m.types[j].Kind() != reflect.Bool {
			if i+1 == len(argv) {
				return nil, nil, fmt.Errorf("--%s needs a value", name)
			}
			i++
			val = argv[i]
		}
		flags[name] = val
	}
	return args, flags, nil
}

// MethodRow is a line of `larvyn api list`.
type MethodRow struct {
	Method string `json:"method"`
	Usage  string `json:"usage"`
	Doc    string `json:"doc"`
}

func init() {
	Register("api.list", "List every method and its params (larvyn help shows each param)", func() []MethodRow {
		var out []MethodRow
		for _, m := range Methods() {
			out = append(out, MethodRow{Method: m.Name, Usage: m.Usage(), Doc: m.Doc})
		}
		return out
	})
}
