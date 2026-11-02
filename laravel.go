package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"larvyn/internal/dotenv"
	"larvyn/internal/proc"
	"larvyn/internal/store"
)

//go:embed bridge.php
var bridgeSource []byte

//go:embed create.php
var createSource []byte

// createScript returns the path of create.php, Larvyn's stand-in for
// `laravel new` when the installer is missing, written next to the app state.
func createScript() string {
	path := filepath.Join(store.HomeDir(), "create.php")
	_ = os.MkdirAll(store.HomeDir(), 0o755)
	if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, createSource) {
		_ = os.WriteFile(path, createSource, 0o644)
	}
	return path
}

var bridgeOnce sync.Once
var bridgePath string

// bridge returns the path of the PHP bridge, written next to the app state.
func bridge() string {
	bridgeOnce.Do(func() {
		bridgePath = filepath.Join(store.HomeDir(), "bridge.php")
		_ = os.MkdirAll(store.HomeDir(), 0o755)
		if b, err := os.ReadFile(bridgePath); err != nil || !bytes.Equal(b, bridgeSource) {
			_ = os.WriteFile(bridgePath, bridgeSource, 0o644)
		}
	})
	return bridgePath
}

// callBridge boots the project's Laravel app and runs one bridge action.
func callBridge(projectID, action string, payload any, timeout time.Duration) (json.RawMessage, error) {
	p, ok := store.FindProject(projectID)
	if !ok {
		return nil, errors.New("project not found")
	}
	if !isDir(filepath.Join(p.Path, "vendor")) {
		return nil, errors.New("vendor/ is missing: run composer install first")
	}
	in, _ := json.Marshal(payload)
	// Deprecations from newer PHP versions in config files are noise here.
	res, err := proc.PHP(p.Path, timeout, in, "-d", "error_reporting=E_ALL & ~E_DEPRECATED", "-d", "display_errors=stderr", bridge(), p.Path, action)
	if err != nil {
		return nil, err
	}
	const mark = "\n@@LARVYN@@"
	i := strings.LastIndex(res.Stdout, mark)
	if i < 0 {
		msg := strings.TrimSpace(proc.FirstNonEmpty(res.Stderr, res.Stdout))
		if msg == "" {
			msg = fmt.Sprintf("php exited with code %d", res.Code)
		}
		return nil, errors.New(proc.StripANSI(msg))
	}
	return json.RawMessage(res.Stdout[i+len(mark):]), nil
}

type LaravelService struct {
	mu       sync.Mutex
	commands map[string]json.RawMessage
}

func projectPath(id string) (string, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return "", errors.New("project not found")
	}
	return p.Path, nil
}

// Bridge runs a bridge action and returns its JSON answer.
func (l *LaravelService) Bridge(projectID, action string, payload map[string]any) (json.RawMessage, error) {
	timeout := 60 * time.Second
	if action == "tinker" {
		timeout = 120 * time.Second
	}
	return callBridge(projectID, action, payload, timeout)
}

// Commands lists the project's artisan commands (cached until refresh).
func (l *LaravelService) Commands(projectID string, refresh bool) (json.RawMessage, error) {
	l.mu.Lock()
	if l.commands == nil {
		l.commands = map[string]json.RawMessage{}
	}
	cached, ok := l.commands[projectID]
	l.mu.Unlock()
	if ok && !refresh {
		return cached, nil
	}
	dir, err := projectPath(projectID)
	if err != nil {
		return nil, err
	}
	res, err := proc.Artisan(dir, 60*time.Second, "list", "--format=json")
	if err != nil {
		return nil, err
	}
	out := proc.ExtractJSON(res.Stdout)
	if out == nil {
		return nil, errors.New(proc.StripANSI(proc.FirstNonEmpty(res.Stderr, res.Stdout, "artisan list failed")))
	}
	l.mu.Lock()
	l.commands[projectID] = out
	l.mu.Unlock()
	return out, nil
}

// Routes returns `artisan route:list --json`.
func (l *LaravelService) Routes(projectID string) (json.RawMessage, error) {
	dir, err := projectPath(projectID)
	if err != nil {
		return nil, err
	}
	res, err := proc.Artisan(dir, 60*time.Second, "route:list", "--json")
	if err != nil {
		return nil, err
	}
	out := proc.ExtractJSON(res.Stdout)
	if out == nil {
		return nil, errors.New(proc.StripANSI(proc.FirstNonEmpty(res.Stderr, res.Stdout, "route:list failed")))
	}
	return out, nil
}

// About returns `artisan about --json`.
func (l *LaravelService) About(projectID string) (json.RawMessage, error) {
	dir, err := projectPath(projectID)
	if err != nil {
		return nil, err
	}
	res, err := proc.Artisan(dir, 60*time.Second, "about", "--json")
	if err != nil {
		return nil, err
	}
	out := proc.ExtractJSON(res.Stdout)
	if out == nil {
		return nil, errors.New(proc.StripANSI(proc.FirstNonEmpty(res.Stderr, res.Stdout, "about failed")))
	}
	return out, nil
}

// ModelDetail returns `artisan model:show <class> --json`.
func (l *LaravelService) ModelDetail(projectID, class string) (json.RawMessage, error) {
	dir, err := projectPath(projectID)
	if err != nil {
		return nil, err
	}
	res, err := proc.Artisan(dir, 60*time.Second, "model:show", class, "--json")
	if err != nil {
		return nil, err
	}
	out := proc.ExtractJSON(res.Stdout)
	if out == nil {
		return nil, errors.New(proc.StripANSI(proc.FirstNonEmpty(res.Stderr, res.Stdout, "model:show failed")))
	}
	return out, nil
}

// Artisan runs a short artisan command and captures its output.
func (l *LaravelService) Artisan(projectID string, args []string) (RunResult, error) {
	dir, err := projectPath(projectID)
	if err != nil {
		return RunResult{}, err
	}
	res, err := proc.Artisan(dir, 5*time.Minute, args...)
	res.Stdout = proc.StripANSI(res.Stdout)
	res.Stderr = proc.StripANSI(res.Stderr)
	return res, err
}

// ---- .env -----------------------------------------------------------------

// EnvFile is a dotenv file with its parsed keys.
type EnvFile struct {
	Path    string            `json:"path"`
	Exists  bool              `json:"exists"`
	Raw     string            `json:"raw"`
	Values  map[string]string `json:"values"`
	Missing []string          `json:"missing"` // in .env.example but not here
	Extra   []string          `json:"extra"`   // here but not in .env.example
	Files   []string          `json:"files"`
}

// Env reads .env (or another .env.* file) and compares it with .env.example.
func (l *LaravelService) Env(projectID, name string) (EnvFile, error) {
	dir, err := projectPath(projectID)
	if err != nil {
		return EnvFile{}, err
	}
	if name == "" {
		name = ".env"
	}
	if strings.ContainsAny(name, `/\`) || !strings.HasPrefix(name, ".env") {
		return EnvFile{}, errors.New("not an env file")
	}
	f := EnvFile{Path: filepath.Join(dir, name), Values: map[string]string{}}
	if b, err := os.ReadFile(f.Path); err == nil {
		f.Exists = true
		f.Raw = string(b)
		f.Values = dotenv.Read(f.Path)
	}
	if example := dotenv.Read(filepath.Join(dir, ".env.example")); example != nil && name != ".env.example" {
		for k := range example {
			if _, ok := f.Values[k]; !ok {
				f.Missing = append(f.Missing, k)
			}
		}
		for k := range f.Values {
			if _, ok := example[k]; !ok {
				f.Extra = append(f.Extra, k)
			}
		}
		sort.Strings(f.Missing)
		sort.Strings(f.Extra)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".env*"))
	for _, m := range matches {
		f.Files = append(f.Files, filepath.Base(m))
	}
	return f, nil
}

// SaveEnv writes an env file, keeping a backup of the previous one.
func (l *LaravelService) SaveEnv(projectID, name, raw string) error {
	dir, err := projectPath(projectID)
	if err != nil {
		return err
	}
	if name == "" {
		name = ".env"
	}
	if strings.ContainsAny(name, `/\`) || !strings.HasPrefix(name, ".env") {
		return errors.New("not an env file")
	}
	path := filepath.Join(dir, name)
	if old, err := os.ReadFile(path); err == nil {
		_ = os.MkdirAll(filepath.Join(store.HomeDir(), "backups"), 0o755)
		_ = os.WriteFile(filepath.Join(store.HomeDir(), "backups", fmt.Sprintf("%s-%s-%d", projectID, name, time.Now().Unix())), old, 0o600)
	}
	return os.WriteFile(path, []byte(raw), 0o644)
}

// SetEnv sets one key in .env, appending it when absent.
func (l *LaravelService) SetEnv(projectID, key, value string) error {
	f, err := l.Env(projectID, ".env")
	if err != nil {
		return err
	}
	if strings.ContainsAny(value, " #\"'=") {
		value = `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	lines := strings.Split(f.Raw, "\n")
	found := false
	for i, line := range lines {
		if k, _, ok := dotenv.ParseLine(line); ok && k == key {
			lines[i] = key + "=" + value
			found = true
		}
	}
	if !found {
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, key+"="+value, "")
	}
	return l.SaveEnv(projectID, ".env", strings.Join(lines, "\n"))
}

// ---- logs -----------------------------------------------------------------

// LogEntry is one record from a Laravel log file.
type LogEntry struct {
	Time      string `json:"time"`
	Env       string `json:"env"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Exception string `json:"exception"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Context   string `json:"context"`
	Stack     string `json:"stack"`
	Offset    int64  `json:"offset"`
}

// LogChunk is what changed in a log file since an offset.
type LogChunk struct {
	File    string     `json:"file"`
	Entries []LogEntry `json:"entries"`
	Offset  int64      `json:"offset"`
	Size    int64      `json:"size"`
	Reset   bool       `json:"reset"`
}

type LogFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

var logHead = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2}[ T][\d:.+\-]+)\] (\w+)\.(\w+): (.*)$`)
var logExc = regexp.MustCompile(`\(([A-Za-z0-9_\\]+)(?:\(code: [^)]*\))?: (.*) at (.+?):(\d+)\)`)

// LogFiles lists storage/logs.
func (l *LaravelService) LogFiles(projectID string) ([]LogFile, error) {
	dir, err := projectPath(projectID)
	if err != nil {
		return nil, err
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "storage", "logs"))
	var out []LogFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		if fi, err := e.Info(); err == nil {
			out = append(out, LogFile{Name: e.Name(), Size: fi.Size(), Modified: fi.ModTime().UnixMilli()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	return out, nil
}

// ReadLog returns entries added after offset; a negative offset reads the
// tail of the file.
func (l *LaravelService) ReadLog(projectID, file string, offset int64) (LogChunk, error) {
	dir, err := projectPath(projectID)
	if err != nil {
		return LogChunk{}, err
	}
	if file == "" {
		file = "laravel.log"
	}
	if strings.ContainsAny(file, `/\`) {
		return LogChunk{}, errors.New("bad log name")
	}
	chunk := LogChunk{File: file}
	f, err := os.Open(filepath.Join(dir, "storage", "logs", file))
	if err != nil {
		return chunk, nil
	}
	defer f.Close()
	fi, _ := f.Stat()
	chunk.Size = fi.Size()
	const tail = 3 << 20
	if offset < 0 || offset > fi.Size() {
		chunk.Reset = true
		offset = fi.Size() - tail
		if offset < 0 {
			offset = 0
		}
	}
	if offset == fi.Size() {
		chunk.Offset = offset
		return chunk, nil
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return chunk, err
	}
	data, _ := io.ReadAll(f)
	// Only parse up to the last complete line; the rest is read next time.
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		data = data[:i+1]
	} else {
		chunk.Offset = offset
		return chunk, nil
	}
	chunk.Offset = offset + int64(len(data))
	chunk.Entries = parseLog(data, offset)
	return chunk, nil
}

func parseLog(data []byte, base int64) []LogEntry {
	var out []LogEntry
	var cur *LogEntry
	var stack strings.Builder
	flush := func() {
		if cur != nil {
			cur.Stack = strings.TrimRight(stack.String(), "\n")
			out = append(out, *cur)
		}
		stack.Reset()
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	pos := base
	for sc.Scan() {
		line := sc.Text()
		lineStart := pos
		pos += int64(len(line)) + 1
		if m := logHead.FindStringSubmatch(line); m != nil {
			flush()
			e := LogEntry{Time: m[1], Env: m[2], Level: strings.ToLower(m[3]), Offset: lineStart}
			msg := m[4]
			if i := strings.Index(msg, ` {"`); i >= 0 {
				e.Context = msg[i+1:]
				msg = msg[:i]
			} else if i := strings.Index(msg, ` {`); i >= 0 && strings.HasSuffix(msg, "}") {
				e.Context = msg[i+1:]
				msg = msg[:i]
			}
			e.Message = msg
			if x := logExc.FindStringSubmatch(e.Context); x != nil {
				// The context is JSON, so class names and Windows paths
				// arrive with their backslashes escaped.
				e.Exception = unescapeJSON(x[1])
				e.File = unescapeJSON(x[3])
				fmt.Sscanf(x[4], "%d", &e.Line)
			}
			cur = &e
			continue
		}
		if cur != nil {
			if line == "[stacktrace]" || line == `"}` {
				continue
			}
			stack.WriteString(unescapeJSON(line))
			stack.WriteByte('\n')
		}
	}
	flush()
	if len(out) > 2000 {
		out = out[len(out)-2000:]
	}
	return out
}

func unescapeJSON(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\\`, `\`), `\/`, `/`)
}

// ClearLog empties a log file.
func (l *LaravelService) ClearLog(projectID, file string) error {
	dir, err := projectPath(projectID)
	if err != nil {
		return err
	}
	if strings.ContainsAny(file, `/\`) || !strings.HasSuffix(file, ".log") {
		return errors.New("bad log name")
	}
	return os.Truncate(filepath.Join(dir, "storage", "logs", file), 0)
}

// ---- files ----------------------------------------------------------------

// ReadFile returns a project file's text (used for previews).
func (l *LaravelService) ReadFile(projectID, rel string) (string, error) {
	dir, err := projectPath(projectID)
	if err != nil {
		return "", err
	}
	path := rel
	if !filepath.IsAbs(rel) {
		path = filepath.Join(dir, rel)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if fi.Size() > 2<<20 {
		return "", errors.New("file too large to preview")
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

// ResolveClass maps an App\ class to its file and, if given, a method line.
func (l *LaravelService) ResolveClass(projectID, class, method string) (map[string]any, error) {
	dir, err := projectPath(projectID)
	if err != nil {
		return nil, err
	}
	class = strings.TrimPrefix(class, `\`)
	if !strings.HasPrefix(class, `App\`) {
		return nil, errors.New("only App\\ classes can be opened")
	}
	path := filepath.Join(dir, "app", filepath.FromSlash(strings.ReplaceAll(strings.TrimPrefix(class, `App\`), `\`, "/"))+".php")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	line := 1
	if method != "" {
		re := regexp.MustCompile(`function\s+` + regexp.QuoteMeta(method) + `\s*\(`)
		if loc := re.FindIndex(b); loc != nil {
			line = bytes.Count(b[:loc[0]], []byte("\n")) + 1
		}
	}
	return map[string]any{"path": path, "line": line}, nil
}

// PathExists reports whether a project-relative path exists.
func (l *LaravelService) PathExists(projectID, rel string) bool {
	dir, err := projectPath(projectID)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, rel))
	return err == nil
}
