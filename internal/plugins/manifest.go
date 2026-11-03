package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"larvyn/internal/updater"
)

// APIVersion is the plugin API this Larvyn implements. A manifest asking for
// another one is refused instead of half working.
const APIVersion = 1

// ManifestFile is the file every plugin folder holds.
const ManifestFile = "larvyn.json"

// maxManifestSize keeps a broken or hostile manifest from being read whole.
const maxManifestSize = 256 << 10

// Manifest is a plugin's larvyn.json.
type Manifest struct {
	APIVersion  int    `json:"apiVersion"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	// Larvyn is the range of Larvyn versions the plugin works with, such as
	// ">=0.2.0" or ">=0.2.0 <0.4.0".
	Larvyn      string   `json:"larvyn"`
	Permissions []string `json:"permissions"`
	// Network lists the domains the plugin's views may connect to. It needs
	// the network permission, and the permission needs it.
	Network []string `json:"network,omitempty"`
	// Env lists the .env keys the plugin may read and change, such as
	// MAIL_MAILER. It needs the project.env.keys permission, and the
	// permission needs it; no other key is ever shown or written.
	Env         []string    `json:"env,omitempty"`
	Contributes Contributes `json:"contributes"`
	// Requires lists the programs the plugin needs on PATH, such as mailpit.
	Requires []Requirement `json:"requires,omitempty"`
	// Backend is a program for Tier 2 plugins; Larvyn does not run those yet.
	Backend *Backend `json:"backend,omitempty"`
}

// Requirement is a program a plugin needs. Larvyn checks PATH for it and,
// when it is missing, offers the install command for the user's system; it
// runs only in a visible terminal, after the user confirmed it.
type Requirement struct {
	Program string `json:"program"`
	// Install maps "mac", "linux" and "windows" to a shell command.
	Install map[string]string `json:"install,omitempty"`
	URL     string            `json:"url,omitempty"`
}

// Contributes is what a plugin adds to Larvyn.
type Contributes struct {
	Commands []Command   `json:"commands,omitempty"`
	Stack    []StackItem `json:"stack,omitempty"`
	Views    []View      `json:"views,omitempty"`
}

// Command is a shell command shown in ⌘K and the project actions. It runs
// in a project terminal, exactly as written.
type Command struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Run   string `json:"run"`
}

// StackItem is a long-running process added to the project's stack, such
// as a queue dashboard or a mail catcher.
type StackItem struct {
	Key   string   `json:"key"`
	Title string   `json:"title"`
	Argv  []string `json:"argv"`
}

// View is a page shown in the sidebar, inside a sandboxed iframe.
type View struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Entry is the HTML file, relative to the plugin folder.
	Entry string `json:"entry"`
}

// Backend is the program a Tier 2 plugin runs.
type Backend struct {
	Run []string `json:"run"`
}

// Permission is something a plugin may ask to do. Sensitive ones are shown
// in red when the user is asked.
type Permission struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Sensitive bool   `json:"sensitive"`
}

// Permissions are all the permissions of API v1.
var Permissions = []Permission{
	{"project.read", "Read the project's name, path and stack", false},
	{"project.env.read", "Read the project's .env, including secrets like APP_KEY and DB_PASSWORD", true},
	{"terminal.run", "Run commands in a terminal", true},
	{"artisan.run", "Run Artisan commands", true},
	{"stack.control", "Start and stop the project's services", false},
	{"ui.notify", "Show notifications", false},
	{"storage", "Keep its own settings and data", false},
	{"network", "Connect to the internet", true},
	{"project.env.keys", "Read and change these .env values", true},
}

func knownPermission(name string) bool {
	for _, p := range Permissions {
		if p.Name == name {
			return true
		}
	}
	return false
}

var (
	pluginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)+$`)
	itemIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	versionPattern  = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	envKeyPattern   = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	programPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	// A domain (optionally *.example.com) or a local host, each with an
	// optional port: api.example.com, localhost:8025, 127.0.0.1:1025.
	domainPattern    = regexp.MustCompile(`^((\*\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}|localhost|127\.0\.0\.1)(:[0-9]{1,5})?$`)
	comparatorPrefix = regexp.MustCompile(`^(>=|<=|>|<|=)?`)
)

// ReadManifest reads and checks the manifest in dir. The returned manifest
// is filled in as far as it could be read, even when err is set.
func ReadManifest(dir string) (Manifest, error) {
	var m Manifest
	f, err := os.Open(filepath.Join(dir, ManifestFile))
	if err != nil {
		return m, err
	}
	defer f.Close()
	buf := make([]byte, maxManifestSize+1)
	n, _ := io.ReadFull(f, buf)
	if n > maxManifestSize {
		return m, fmt.Errorf("%s is larger than %d KB", ManifestFile, maxManifestSize>>10)
	}
	return ParseManifest(buf[:n], dir)
}

// ParseManifest checks a manifest. dir is the plugin folder, used to check
// that the files it names exist; "" skips that.
func ParseManifest(data []byte, dir string) (Manifest, error) {
	var m Manifest
	// The API version decides which fields exist, so it is read first.
	var head struct {
		APIVersion int `json:"apiVersion"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return m, fmt.Errorf("%s is not valid JSON: %w", ManifestFile, err)
	}
	if head.APIVersion != APIVersion {
		_ = json.Unmarshal(data, &m)
		if head.APIVersion > APIVersion {
			return m, fmt.Errorf("needs plugin API %d; this Larvyn has API %d, update Larvyn", head.APIVersion, APIVersion)
		}
		return m, fmt.Errorf("apiVersion must be %d", APIVersion)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	return m, m.check(dir)
}

func (m Manifest) check(dir string) error {
	switch {
	case !pluginIDPattern.MatchString(m.ID) || len(m.ID) > 64:
		return fmt.Errorf("id %q must look like publisher.name: lowercase letters, digits and dashes", m.ID)
	case strings.TrimSpace(m.Name) == "":
		return errors.New("name is missing")
	case !versionPattern.MatchString(m.Version):
		return fmt.Errorf("version %q is not a semantic version like 1.0.0", m.Version)
	}
	if m.Homepage != "" && !webLink(m.Homepage) {
		return fmt.Errorf("homepage %q must be an http or https link", m.Homepage)
	}
	if err := checkRequires(m.Requires); err != nil {
		return err
	}
	if err := checkRange(m.Larvyn); err != nil {
		return err
	}
	if m.Backend != nil {
		return errors.New("backends (Tier 2 plugins) are not supported yet")
	}

	perms := map[string]bool{}
	for _, p := range m.Permissions {
		if !knownPermission(p) {
			return fmt.Errorf("unknown permission %q", p)
		}
		if perms[p] {
			return fmt.Errorf("permission %q is listed twice", p)
		}
		perms[p] = true
	}
	if perms["network"] != (len(m.Network) > 0) {
		return errors.New(`the "network" permission and the network domains go together: list the domains the plugin connects to`)
	}
	if perms["project.env.keys"] != (len(m.Env) > 0) {
		return errors.New(`the "project.env.keys" permission and the env keys go together: list the .env keys the plugin may read and change`)
	}
	seenKey := map[string]bool{}
	for _, k := range m.Env {
		if !envKeyPattern.MatchString(k) {
			return fmt.Errorf("env key %q must look like MAIL_HOST: capital letters, digits and underscores", k)
		}
		if seenKey[k] {
			return fmt.Errorf("env key %q is listed twice", k)
		}
		seenKey[k] = true
	}
	for _, d := range m.Network {
		if !validHost(d) {
			return fmt.Errorf("network domain %q is not a host like api.example.com or localhost:8025", d)
		}
	}

	c := m.Contributes
	if len(c.Commands)+len(c.Stack)+len(c.Views) == 0 {
		return errors.New("contributes is empty: the plugin adds nothing")
	}
	if (len(c.Commands) > 0 || len(c.Stack) > 0) && !perms["terminal.run"] {
		return errors.New(`commands and stack items run in a terminal, so they need the "terminal.run" permission`)
	}
	seen := map[string]bool{}
	for _, cmd := range c.Commands {
		if err := checkItem("command", cmd.ID, cmd.Title, seen); err != nil {
			return err
		}
		if strings.TrimSpace(cmd.Run) == "" {
			return fmt.Errorf("command %q has nothing to run", cmd.ID)
		}
	}
	seen = map[string]bool{}
	for _, it := range c.Stack {
		if err := checkItem("stack item", it.Key, it.Title, seen); err != nil {
			return err
		}
		if len(it.Argv) == 0 || strings.TrimSpace(it.Argv[0]) == "" {
			return fmt.Errorf("stack item %q has no argv", it.Key)
		}
	}
	seen = map[string]bool{}
	for _, v := range c.Views {
		if err := checkItem("view", v.ID, v.Title, seen); err != nil {
			return err
		}
		if err := checkEntry(dir, v.Entry); err != nil {
			return fmt.Errorf("view %q: %w", v.ID, err)
		}
	}
	return nil
}

// validHost reports whether d is a network host a plugin may list: a domain
// or a local host, with an optional port from 1 to 65535.
func validHost(d string) bool {
	if !domainPattern.MatchString(d) {
		return false
	}
	if _, port, ok := strings.Cut(d, ":"); ok {
		n, err := strconv.Atoi(port)
		return err == nil && n >= 1 && n <= 65535
	}
	return true
}

func webLink(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func checkRequires(reqs []Requirement) error {
	seen := map[string]bool{}
	for _, r := range reqs {
		// A program name, not a path: Larvyn looks it up on PATH.
		if !programPattern.MatchString(r.Program) {
			return fmt.Errorf("requires: program %q must be a program name like mailpit, not a path", r.Program)
		}
		if seen[r.Program] {
			return fmt.Errorf("requires: program %q is listed twice", r.Program)
		}
		seen[r.Program] = true
		for osName, cmd := range r.Install {
			if osName != "mac" && osName != "linux" && osName != "windows" {
				return fmt.Errorf("requires %s: install has %q; use mac, linux or windows", r.Program, osName)
			}
			if strings.TrimSpace(cmd) == "" || strings.ContainsAny(cmd, "\n\r") {
				return fmt.Errorf("requires %s: the %s install command must be one line", r.Program, osName)
			}
		}
		if r.URL != "" && !webLink(r.URL) {
			return fmt.Errorf("requires %s: url %q must be an http or https link", r.Program, r.URL)
		}
	}
	return nil
}

func checkItem(kind, id, title string, seen map[string]bool) error {
	switch {
	case !itemIDPattern.MatchString(id) || len(id) > 64:
		return fmt.Errorf("%s id %q must be lowercase letters, digits and dashes", kind, id)
	case seen[id]:
		return fmt.Errorf("%s id %q is used twice", kind, id)
	case strings.TrimSpace(title) == "":
		return fmt.Errorf("%s %q has no title", kind, id)
	}
	seen[id] = true
	return nil
}

// checkEntry makes sure a view's HTML file is inside the plugin folder,
// following symlinks, so a plugin can't serve files from elsewhere.
func checkEntry(dir, entry string) error {
	clean := path.Clean(entry)
	if entry == "" || path.IsAbs(entry) || filepath.IsAbs(entry) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(entry, `\`) {
		return fmt.Errorf("entry %q must be a path inside the plugin folder, with forward slashes", entry)
	}
	if !strings.HasSuffix(clean, ".html") {
		return fmt.Errorf("entry %q must be an .html file", entry)
	}
	if dir == "" {
		return nil
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(clean)))
	if err != nil {
		return fmt.Errorf("entry %q does not exist", entry)
	}
	if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("entry %q points outside the plugin folder", entry)
	}
	return nil
}

// checkRange checks a version range: comparators like >=0.2.0 separated by
// spaces, all of which must hold.
func checkRange(r string) error {
	if strings.TrimSpace(r) == "" {
		return errors.New(`larvyn is missing: the Larvyn versions the plugin works with, such as ">=0.2.0"`)
	}
	for _, c := range strings.Fields(r) {
		v := c[len(comparatorPrefix.FindString(c)):]
		if !versionPattern.MatchString(v) {
			return fmt.Errorf("larvyn %q: %q is not a comparator like >=0.2.0", r, c)
		}
	}
	return nil
}

// Compatible reports whether Larvyn version current is in range r, which
// must have passed checkRange. Larvyn's own prerelease tag is ignored, so
// 0.2.0-beta.3 counts as 0.2.0, and development builds match every range.
func Compatible(r, current string) bool {
	current = strings.TrimPrefix(current, "v")
	if current == "dev" || current == "" {
		return true
	}
	current, _, _ = strings.Cut(current, "-")
	for _, c := range strings.Fields(r) {
		op := comparatorPrefix.FindString(c)
		cmp := updater.CompareVersions(current, c[len(op):])
		ok := false
		switch op {
		case ">=":
			ok = cmp >= 0
		case ">":
			ok = cmp > 0
		case "<=":
			ok = cmp <= 0
		case "<":
			ok = cmp < 0
		default:
			ok = cmp == 0
		}
		if !ok {
			return false
		}
	}
	return true
}
