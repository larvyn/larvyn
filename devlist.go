package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"larvyn/internal/proc"
)

// DevProcess is one entry of `php artisan dev:list --json` (Laravel 13+): the
// processes `php artisan dev` and `composer run dev` start, including the ones
// the app or its packages register with DevCommands.
type DevProcess struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Color   string `json:"color"`
	Source  string `json:"source"`
}

type devListEntry struct {
	sig   string
	procs []DevProcess
	ok    bool
}

var devListCache sync.Map // project path -> devListEntry

// devListSignature changes whenever something that registers dev processes
// may have changed: installed packages, service providers, bootstrap files.
func devListSignature(root string) string {
	var b strings.Builder
	stamp := func(path string) {
		if fi, err := os.Stat(path); err == nil {
			b.WriteString(path)
			b.WriteString(fi.ModTime().UTC().Format(time.RFC3339Nano))
		}
	}
	stamp(filepath.Join(root, "composer.lock"))
	stamp(filepath.Join(root, "package.json"))
	for _, dir := range []string{"bootstrap", filepath.Join("app", "Providers")} {
		entries, _ := os.ReadDir(filepath.Join(root, dir))
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".php") {
				stamp(filepath.Join(root, dir, e.Name()))
			}
		}
	}
	return b.String()
}

// devProcesses returns the app's dev processes, or ok=false when the app has
// no dev:list command (Laravel 12 and older) or it failed.
func devProcesses(p Project) ([]DevProcess, bool) {
	sig := devListSignature(p.Path)
	if v, found := devListCache.Load(p.Path); found {
		if e := v.(devListEntry); e.sig == sig {
			return e.procs, e.ok
		}
	}
	var procs []DevProcess
	ok := false
	// Only ask apps whose framework ships dev:list: on older ones Artisan
	// reports the unknown command to storage/logs/laravel.log, which shows up
	// as a new error in the Logs view.
	if !hasDevListCommand(p.Path) {
		devListCache.Store(p.Path, devListEntry{sig: sig})
		return nil, false
	}
	if res, err := proc.Artisan(p.Path, 30*time.Second, "dev:list", "--json"); err == nil && res.Code == 0 {
		if raw := extractJSON(res.Stdout); raw != nil && json.Unmarshal(raw, &procs) == nil {
			ok = true
		}
	}
	devListCache.Store(p.Path, devListEntry{sig: sig, procs: procs, ok: ok})
	return procs, ok
}

// hasDevListCommand reports whether the installed laravel/framework defines
// the dev:list command, by looking for its signature in the framework's
// command classes instead of running Artisan.
func hasDevListCommand(root string) bool {
	dir := filepath.Join(root, "vendor", "laravel", "framework", "src", "Illuminate", "Foundation")
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), "Command.php") {
			return nil
		}
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), "dev:list") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// splitCommand splits a dev:list command line into arguments, honouring
// double and single quotes. ok=false means it needs a real shell (pipes,
// redirects, chained commands, variables).
func splitCommand(s string) (args []string, ok bool) {
	var cur strings.Builder
	var quote rune
	inArg := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		case strings.ContainsRune("|&;<>()$`%", r):
			return nil, false
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, len(args) > 0
}
