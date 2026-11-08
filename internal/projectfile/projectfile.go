// Package projectfile reads and writes .larvyn/project.json, the settings a
// team commits with its code: shared commands, extra dev-stack items, the
// plugins the project uses and how to prepare an agent's worktree. Personal
// settings stay in Larvyn's state folder (internal/store).
//
// A wrong entry is skipped with a warning and the rest still loads. A file
// that doesn't parse keeps the last version that loaded, with the error. The
// file is read again when it changes on disk (git pull, branch switch).
package projectfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"larvyn/internal/safefile"
)

// APIVersion is the schema this Larvyn reads, like a plugin's larvyn.json.
const APIVersion = 1

// Dir and Name locate the file inside a project: .larvyn/project.json.
const (
	Dir  = ".larvyn"
	Name = "project.json"
)

// maxSize keeps a broken or hostile file from being read whole.
const maxSize = 256 << 10

// Rel is the file's path relative to the project root, with forward slashes.
var Rel = Dir + "/" + Name

// Command is a shell command for ⌘K and the project's actions, the same
// shape as a plugin command. It runs in a project terminal as written.
type Command struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Run   string `json:"run"`
}

// StackItem is an extra long-running process for the dev stack, the same
// shape as a plugin stack item.
type StackItem struct {
	Key   string   `json:"key"`
	Title string   `json:"title"`
	Argv  []string `json:"argv"`
}

// Plugin is a plugin the project uses, with an optional version range such
// as ">=1.2.0". In the file it is an object or just the id.
type Plugin struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
}

// UnmarshalJSON takes "publisher.name" as well as {"id": …, "version": …}.
func (p *Plugin) UnmarshalJSON(b []byte) error {
	var id string
	if json.Unmarshal(b, &id) == nil {
		*p = Plugin{ID: id}
		return nil
	}
	type plain Plugin
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var v plain
	if err := dec.Decode(&v); err != nil {
		return err
	}
	*p = Plugin(v)
	return nil
}

// Worktree is how a new agent worktree is prepared.
type Worktree struct {
	// Setup are shell commands run one after another in each new agent
	// worktree before the agent starts, such as composer install.
	Setup []string `json:"setup,omitempty"`
}

// File is the valid content of project.json.
type File struct {
	APIVersion int         `json:"apiVersion"`
	Commands   []Command   `json:"commands,omitempty"`
	Stack      []StackItem `json:"stack,omitempty"`
	Plugins    []Plugin    `json:"plugins,omitempty"`
	Worktree   Worktree    `json:"worktree"`
}

// Loaded is what Load found.
type Loaded struct {
	// Path is the file's full path, whether or not it exists.
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	// File holds the entries that loaded. When Error is set it is the last
	// version that loaded, or empty.
	File File `json:"file"`
	// Warnings are the entries that were skipped, and why.
	Warnings []string `json:"warnings"`
	// Error is set when the file can't be read or doesn't parse.
	Error string `json:"error"`
}

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	pluginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)+$`)
	rangePattern    = regexp.MustCompile(`^(>=|<=|>|<|=)?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
)

// Path is where a project's file is.
func Path(root string) string { return filepath.Join(root, Dir, Name) }

type cached struct {
	// valid is set once the entry holds a read of the file.
	valid  bool
	mod    time.Time
	size   int64
	exists bool
	loaded Loaded
	// good is the last version that parsed.
	good *File
	// reported is the file state Changed last saw, kept apart from the
	// read cache so a Load can't take a change before the watcher sees it.
	reported *fileState
}

type fileState struct {
	mod    time.Time
	size   int64
	exists bool
}

var (
	mu    sync.Mutex
	cache = map[string]*cached{}
)

// Load reads the project's file, from the cache while it has not changed.
func Load(root string) Loaded {
	return load(root)
}

// Changed reads the file again if it changed and reports whether it did
// since the last call to Changed for this project (or since the first read
// of it), whatever Load read in between.
func Changed(root string) bool {
	load(root)
	mu.Lock()
	defer mu.Unlock()
	c := cache[Path(root)]
	now := fileState{mod: c.mod, size: c.size, exists: c.exists}
	before := c.reported
	c.reported = &now
	return before != nil && *before != now
}

func load(root string) Loaded {
	path := Path(root)
	st, statErr := os.Lstat(path)
	exists := statErr == nil
	mu.Lock()
	defer mu.Unlock()
	c := cache[path]
	if c != nil && c.valid && c.exists == exists && (!exists || (st.ModTime().Equal(c.mod) && st.Size() == c.size)) {
		return c.loaded
	}
	if c == nil {
		c = &cached{}
		cache[path] = c
	}
	c.valid, c.exists = true, exists
	c.loaded = Loaded{Path: path, Exists: exists}
	if exists {
		c.mod, c.size = st.ModTime(), st.Size()
		b, err := safefile.ReadRegularIn(root, filepath.Join(Dir, Name), maxSize)
		if err != nil {
			if errors.Is(err, safefile.ErrTooLarge) {
				err = fmt.Errorf("%s is larger than %d KB", Rel, maxSize>>10)
			}
			c.loaded.Error = err.Error()
		} else if f, warnings, err := Parse(b); err != nil {
			c.loaded.Error = err.Error()
		} else {
			c.good = &f
			c.loaded.Warnings = warnings
		}
		if c.good != nil {
			c.loaded.File = *c.good
		}
	} else {
		c.mod, c.size = time.Time{}, 0
		c.good = nil
	}
	if c.reported == nil {
		// The first read is the watcher's baseline: an edit made after the
		// window read the file, before the watcher's first look, still counts.
		c.reported = &fileState{mod: c.mod, size: c.size, exists: c.exists}
	}
	return c.loaded
}

// Forget drops what is cached about a project, so the next Load reads it.
// What Changed last reported stays, so the watcher still sees the change.
func Forget(root string) {
	mu.Lock()
	if c := cache[Path(root)]; c != nil {
		// The last good version stays too: if the file breaks before the
		// next read, its commands and stack items are still there.
		cache[Path(root)] = &cached{good: c.good, reported: c.reported}
	}
	mu.Unlock()
}

// Parse checks a project.json. A wrong entry is left out with a warning;
// an error means the file as a whole can't be used.
func Parse(data []byte) (File, []string, error) {
	var f File
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return f, nil, fmt.Errorf("%s is not valid JSON: %w", Rel, err)
	}
	if top == nil {
		return f, nil, fmt.Errorf("%s must hold a JSON object", Rel)
	}
	if err := json.Unmarshal(top["apiVersion"], &f.APIVersion); err != nil || f.APIVersion != APIVersion {
		if f.APIVersion > APIVersion {
			return File{}, nil, fmt.Errorf("%s needs apiVersion %d; this Larvyn reads %d, update Larvyn", Rel, f.APIVersion, APIVersion)
		}
		return File{}, nil, fmt.Errorf("%s: apiVersion must be %d", Rel, APIVersion)
	}
	var warnings []string
	warn := func(format string, a ...any) { warnings = append(warnings, fmt.Sprintf(format, a...)) }
	for k := range top {
		switch k {
		case "apiVersion", "commands", "stack", "plugins", "worktree":
		default:
			warn("unknown field %q (it may need a newer Larvyn)", k)
		}
	}

	seen := map[string]bool{}
	for i, raw := range entries(top["commands"], "commands", warn) {
		var c Command
		if err := strict(raw, &c); err != nil {
			warn("commands[%d]: %v", i, err)
			continue
		}
		if err := checkCommand(c, seen); err != nil {
			warn("commands[%d]: %v", i, err)
			continue
		}
		seen[c.ID] = true
		f.Commands = append(f.Commands, c)
	}

	seen = map[string]bool{}
	for i, raw := range entries(top["stack"], "stack", warn) {
		var s StackItem
		if err := strict(raw, &s); err != nil {
			warn("stack[%d]: %v", i, err)
			continue
		}
		switch {
		case !validID(s.Key):
			warn("stack[%d]: key %q must be lowercase letters, digits and dashes", i, s.Key)
		case seen[s.Key]:
			warn("stack[%d]: key %q is used twice", i, s.Key)
		case strings.TrimSpace(s.Title) == "":
			warn("stack[%d]: %q has no title", i, s.Key)
		case len(s.Argv) == 0 || strings.TrimSpace(s.Argv[0]) == "":
			warn("stack[%d]: %q has no argv", i, s.Key)
		default:
			seen[s.Key] = true
			f.Stack = append(f.Stack, s)
		}
	}

	seen = map[string]bool{}
	for i, raw := range entries(top["plugins"], "plugins", warn) {
		var p Plugin
		if err := json.Unmarshal(raw, &p); err != nil {
			warn("plugins[%d]: %v", i, err)
			continue
		}
		switch {
		case !pluginIDPattern.MatchString(p.ID) || len(p.ID) > 64:
			warn("plugins[%d]: id %q must look like publisher.name", i, p.ID)
		case seen[p.ID]:
			warn("plugins[%d]: %q is listed twice", i, p.ID)
		case checkRange(p.Version) != nil:
			warn("plugins[%d]: %v", i, checkRange(p.Version))
		default:
			seen[p.ID] = true
			f.Plugins = append(f.Plugins, p)
		}
	}

	if raw, ok := top["worktree"]; ok && string(raw) != "null" {
		var w map[string]json.RawMessage
		if err := json.Unmarshal(raw, &w); err != nil || w == nil {
			warn("worktree must be an object")
		} else {
			for k := range w {
				if k != "setup" {
					warn("worktree: unknown field %q (it may need a newer Larvyn)", k)
				}
			}
			for i, raw := range entries(w["setup"], "worktree.setup", warn) {
				var line string
				if err := json.Unmarshal(raw, &line); err != nil {
					warn("worktree.setup[%d] must be a command line (a string)", i)
					continue
				}
				if strings.TrimSpace(line) == "" {
					warn("worktree.setup[%d] is empty", i)
					continue
				}
				f.Worktree.Setup = append(f.Worktree.Setup, line)
			}
		}
	}
	return f, warnings, nil
}

// entries splits a JSON array; anything else is a warning.
func entries(raw json.RawMessage, name string, warn func(string, ...any)) []json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		warn("%s must be a list", name)
		return nil
	}
	return list
}

// strict decodes one entry, refusing fields the schema doesn't have.
func strict(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func validID(id string) bool { return idPattern.MatchString(id) && len(id) <= 64 }

// checkCommand checks a command, shared or local; seen are the ids taken.
func checkCommand(c Command, seen map[string]bool) error {
	switch {
	case !validID(c.ID):
		return fmt.Errorf("id %q must be lowercase letters, digits and dashes", c.ID)
	case seen[c.ID]:
		return fmt.Errorf("id %q is used twice", c.ID)
	case strings.TrimSpace(c.Title) == "":
		return fmt.Errorf("%q has no title", c.ID)
	case strings.TrimSpace(c.Run) == "":
		return fmt.Errorf("%q has nothing to run", c.ID)
	}
	return nil
}

// CheckCommand checks a command the user typed in, before it is saved.
func CheckCommand(c Command) error { return checkCommand(c, nil) }

// checkRange checks a plugin version range: comparators like >=1.2.0
// separated by spaces. Empty means any version.
func checkRange(r string) error {
	for _, c := range strings.Fields(r) {
		if !rangePattern.MatchString(c) {
			return fmt.Errorf("version %q: %q is not a comparator like >=1.2.0", r, c)
		}
	}
	return nil
}
