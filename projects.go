package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ProjectInfo is a project plus everything Larvyn can learn from its files.
type ProjectInfo struct {
	Project
	Exists          bool              `json:"exists"`
	IsLaravel       bool              `json:"isLaravel"`
	LaravelVersion  string            `json:"laravelVersion"`
	PHPConstraint   string            `json:"phpConstraint"`
	AppName         string            `json:"appName"`
	AppURL          string            `json:"appUrl"`
	AppEnv          string            `json:"appEnv"`
	AppDebug        bool              `json:"appDebug"`
	DBConnection    string            `json:"dbConnection"`
	DBDatabase      string            `json:"dbDatabase"`
	QueueConnection string            `json:"queueConnection"`
	CacheStore      string            `json:"cacheStore"`
	SessionDriver   string            `json:"sessionDriver"`
	MailMailer      string            `json:"mailMailer"`
	Branch          string            `json:"branch"`
	Dirty           int               `json:"dirty"`
	Packages        []Package         `json:"packages"`
	Frontend        []string          `json:"frontend"`
	HasVendor       bool              `json:"hasVendor"`
	HasNodeModules  bool              `json:"hasNodeModules"`
	HasEnv          bool              `json:"hasEnv"`
	HasEnvExample   bool              `json:"hasEnvExample"`
	HasAppKey       bool              `json:"hasAppKey"`
	SQLiteFile      string            `json:"sqliteFile"`
	SQLiteMissing   bool              `json:"sqliteMissing"`
	TestRunner      string            `json:"testRunner"`
	ComposerScripts map[string]string `json:"composerScripts"`
	NpmScripts      map[string]string `json:"npmScripts"`
	Counts          map[string]int    `json:"counts"`
	HerdURL         string            `json:"herdUrl"`
}

// Package is a notable Composer package found in a project.
type Package struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Version string `json:"version"`
	Dev     bool   `json:"dev"`
	Kind    string `json:"kind"`
}

var knownPackages = map[string][2]string{
	"laravel/horizon":             {"Horizon", "queue"},
	"laravel/telescope":           {"Telescope", "debug"},
	"laravel/pulse":               {"Pulse", "monitoring"},
	"laravel/reverb":              {"Reverb", "realtime"},
	"laravel/octane":              {"Octane", "server"},
	"laravel/sanctum":             {"Sanctum", "auth"},
	"laravel/passport":            {"Passport", "auth"},
	"laravel/fortify":             {"Fortify", "auth"},
	"laravel/socialite":           {"Socialite", "auth"},
	"laravel/breeze":              {"Breeze", "starter"},
	"laravel/jetstream":           {"Jetstream", "starter"},
	"laravel/cashier":             {"Cashier", "billing"},
	"laravel/scout":               {"Scout", "search"},
	"laravel/pennant":             {"Pennant", "flags"},
	"laravel/folio":               {"Folio", "routing"},
	"laravel/pail":                {"Pail", "debug"},
	"laravel/sail":                {"Sail", "docker"},
	"laravel/pint":                {"Pint", "quality"},
	"laravel/dusk":                {"Dusk", "testing"},
	"laravel/boost":               {"Boost", "ai"},
	"laravel/mcp":                 {"MCP", "ai"},
	"laravel/nova":                {"Nova", "admin"},
	"laravel/wayfinder":           {"Wayfinder", "routing"},
	"livewire/livewire":           {"Livewire", "frontend"},
	"livewire/volt":               {"Volt", "frontend"},
	"livewire/flux":               {"Flux", "frontend"},
	"inertiajs/inertia-laravel":   {"Inertia", "frontend"},
	"filament/filament":           {"Filament", "admin"},
	"pestphp/pest":                {"Pest", "testing"},
	"phpunit/phpunit":             {"PHPUnit", "testing"},
	"larastan/larastan":           {"Larastan", "quality"},
	"nunomaduro/larastan":         {"Larastan", "quality"},
	"rector/rector":               {"Rector", "quality"},
	"spatie/laravel-permission":   {"Permission", "auth"},
	"spatie/laravel-medialibrary": {"Media Library", "storage"},
	"spatie/laravel-data":         {"Laravel Data", "data"},
	"barryvdh/laravel-debugbar":   {"Debugbar", "debug"},
	"barryvdh/laravel-ide-helper": {"IDE Helper", "quality"},
	"tightenco/ziggy":             {"Ziggy", "routing"},
	"predis/predis":               {"Predis", "cache"},
	"stripe/stripe-php":           {"Stripe", "billing"},
	"laravel/ai":                  {"Laravel AI", "ai"},
	"prism-php/prism":             {"Prism", "ai"},
}

var palette = []string{"#FF2D20", "#F97316", "#EAB308", "#22C55E", "#14B8A6", "#3B82F6", "#8B5CF6", "#EC4899"}

type ProjectService struct{}

func init() {
	application.RegisterEvent[[]Project]("projects:changed")
}

func (s *ProjectService) changed() {
	emit("projects:changed", store.Projects())
}

// List returns the saved projects, pinned and recently opened first.
func (s *ProjectService) List() []Project {
	ps := store.Projects()
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].Pinned != ps[j].Pinned {
			return ps[i].Pinned
		}
		return ps[i].LastOpenedAt > ps[j].LastOpenedAt
	})
	return ps
}

// Add registers a folder as a project.
func (s *ProjectService) Add(path string) (Project, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return Project{}, errors.New("folder not found: " + path)
	}
	for _, p := range store.Projects() {
		if strings.EqualFold(filepath.Clean(p.Path), path) {
			return p, nil
		}
	}
	n := len(store.Projects())
	p := Project{
		ID:           newID(),
		Name:         prettyName(path),
		Path:         path,
		Color:        palette[n%len(palette)],
		Port:         8000 + n,
		AddedAt:      time.Now().UnixMilli(),
		LastOpenedAt: time.Now().UnixMilli(),
	}
	if env := readEnvFile(filepath.Join(path, ".env")); env["APP_NAME"] != "" && env["APP_NAME"] != "Laravel" {
		p.Name = env["APP_NAME"]
	}
	err = store.update(func(d *state) { d.Projects = append(d.Projects, p) })
	s.changed()
	return p, err
}

// Pick asks for a folder and adds it.
func (s *ProjectService) Pick() (*Project, error) {
	path, err := application.Get().Dialog.OpenFile().
		CanChooseDirectories(true).CanChooseFiles(false).CanCreateDirectories(true).
		SetTitle("Open a Laravel project").PromptForSingleSelection()
	if err != nil || path == "" {
		return nil, err
	}
	p, err := s.Add(path)
	return &p, err
}

// PickFolder asks for a folder and returns its path.
func (s *ProjectService) PickFolder(title string) (string, error) {
	return application.Get().Dialog.OpenFile().
		CanChooseDirectories(true).CanChooseFiles(false).CanCreateDirectories(true).
		SetTitle(title).PromptForSingleSelection()
}

// Remove forgets a project; its files stay.
func (s *ProjectService) Remove(id string) error {
	for _, sess := range terminals.List() {
		if sess.ProjectID == id {
			_ = terminals.Close(sess.ID)
		}
	}
	err := store.update(func(d *state) {
		out := d.Projects[:0]
		for _, p := range d.Projects {
			if p.ID != id {
				out = append(out, p)
			}
		}
		d.Projects = out
	})
	s.changed()
	return err
}

// Update saves the editable fields of a project.
func (s *ProjectService) Update(p Project) error {
	err := store.update(func(d *state) {
		for i := range d.Projects {
			if d.Projects[i].ID == p.ID {
				d.Projects[i].Name = p.Name
				d.Projects[i].Color = p.Color
				d.Projects[i].Port = p.Port
				d.Projects[i].Pinned = p.Pinned
			}
		}
	})
	s.changed()
	return err
}

// Touch marks a project as just opened.
func (s *ProjectService) Touch(id string) {
	_ = store.update(func(d *state) {
		d.Active = id
		for i := range d.Projects {
			if d.Projects[i].ID == id {
				d.Projects[i].LastOpenedAt = time.Now().UnixMilli()
			}
		}
	})
}

// Active returns the project that was open last.
func (s *ProjectService) Active() string {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.data.Active
}

// Info inspects a project's files.
func (s *ProjectService) Info(id string) (ProjectInfo, error) {
	p, ok := store.Project(id)
	if !ok {
		return ProjectInfo{}, errors.New("project not found")
	}
	return inspect(p), nil
}

// EnsureSQLite creates the SQLite database file a project's .env points at
// when it is missing, so a fresh app does not fail with "Database file at
// path [...] does not exist". It reports whether a file was created.
func (s *ProjectService) EnsureSQLite(id string) (bool, error) {
	p, ok := store.Project(id)
	if !ok {
		return false, errors.New("project not found")
	}
	path := sqlitePath(p.Path, readEnvFile(filepath.Join(p.Path, ".env")))
	if path == "" {
		return false, nil
	}
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, f.Close()
}

// sqlitePath returns the database file of a project that uses SQLite, or ""
// when it uses another driver or an in-memory database. An empty
// DB_DATABASE means Laravel's default, database/database.sqlite.
func sqlitePath(root string, env map[string]string) string {
	if !strings.EqualFold(env["DB_CONNECTION"], "sqlite") {
		return ""
	}
	db := strings.TrimSpace(env["DB_DATABASE"])
	switch {
	case db == "":
		return filepath.Join(root, "database", "database.sqlite")
	case db == ":memory:" || strings.HasPrefix(db, "file:"):
		return ""
	case filepath.IsAbs(db):
		return db
	default:
		return filepath.Join(root, db)
	}
}

func inspect(p Project) ProjectInfo {
	info := ProjectInfo{Project: p, ComposerScripts: map[string]string{}, NpmScripts: map[string]string{}, Counts: map[string]int{}}
	if _, err := os.Stat(p.Path); err != nil {
		return info
	}
	info.Exists = true
	root := p.Path
	_, artisanErr := os.Stat(filepath.Join(root, "artisan"))

	var composer struct {
		Require    map[string]string          `json:"require"`
		RequireDev map[string]string          `json:"require-dev"`
		Scripts    map[string]json.RawMessage `json:"scripts"`
	}
	if b, err := os.ReadFile(filepath.Join(root, "composer.json")); err == nil {
		_ = json.Unmarshal(b, &composer)
	}
	_, hasFramework := composer.Require["laravel/framework"]
	info.IsLaravel = artisanErr == nil && hasFramework
	info.PHPConstraint = composer.Require["php"]
	for name, raw := range composer.Scripts {
		var one string
		var many []string
		if json.Unmarshal(raw, &one) == nil {
			info.ComposerScripts[name] = one
		} else if json.Unmarshal(raw, &many) == nil {
			info.ComposerScripts[name] = strings.Join(many, " && ")
		}
	}

	locked := lockedVersions(root)
	info.LaravelVersion = strings.TrimPrefix(locked["laravel/framework"], "v")
	if info.LaravelVersion == "" {
		info.LaravelVersion = composer.Require["laravel/framework"]
	}
	add := func(reqs map[string]string, dev bool) {
		for name, constraint := range reqs {
			k, ok := knownPackages[name]
			if !ok {
				continue
			}
			v := strings.TrimPrefix(locked[name], "v")
			if v == "" {
				v = constraint
			}
			info.Packages = append(info.Packages, Package{Name: name, Label: k[0], Version: v, Dev: dev, Kind: k[1]})
		}
	}
	add(composer.Require, false)
	add(composer.RequireDev, true)
	sort.Slice(info.Packages, func(i, j int) bool { return info.Packages[i].Label < info.Packages[j].Label })
	if _, ok := composer.RequireDev["pestphp/pest"]; ok {
		info.TestRunner = "pest"
	} else if _, ok := composer.RequireDev["phpunit/phpunit"]; ok {
		info.TestRunner = "phpunit"
	}

	var pkg struct {
		Scripts         map[string]string `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if b, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		_ = json.Unmarshal(b, &pkg)
		info.NpmScripts = pkg.Scripts
		deps := map[string]bool{}
		for k := range pkg.Dependencies {
			deps[k] = true
		}
		for k := range pkg.DevDependencies {
			deps[k] = true
		}
		for _, f := range []struct{ dep, label string }{
			{"vite", "Vite"}, {"react", "React"}, {"vue", "Vue"}, {"svelte", "Svelte"},
			{"@inertiajs/react", "Inertia React"}, {"@inertiajs/vue3", "Inertia Vue"},
			{"tailwindcss", "Tailwind"}, {"alpinejs", "Alpine"}, {"typescript", "TypeScript"},
			{"laravel-echo", "Echo"}, {"@laravel/vite-plugin-wayfinder", "Wayfinder"},
		} {
			if deps[f.dep] {
				info.Frontend = append(info.Frontend, f.label)
			}
		}
	}

	info.HasVendor = isDir(filepath.Join(root, "vendor"))
	info.HasNodeModules = isDir(filepath.Join(root, "node_modules"))
	_, err := os.Stat(filepath.Join(root, ".env.example"))
	info.HasEnvExample = err == nil
	if env := readEnvFile(filepath.Join(root, ".env")); env != nil {
		info.HasEnv = true
		info.AppName = env["APP_NAME"]
		info.AppURL = env["APP_URL"]
		info.AppEnv = env["APP_ENV"]
		info.AppDebug = strings.EqualFold(env["APP_DEBUG"], "true")
		info.HasAppKey = env["APP_KEY"] != ""
		info.DBConnection = env["DB_CONNECTION"]
		info.DBDatabase = env["DB_DATABASE"]
		info.QueueConnection = env["QUEUE_CONNECTION"]
		info.CacheStore = firstNonEmpty(env["CACHE_STORE"], env["CACHE_DRIVER"])
		info.SessionDriver = env["SESSION_DRIVER"]
		info.MailMailer = firstNonEmpty(env["MAIL_MAILER"], env["MAIL_DRIVER"])
		if f := sqlitePath(root, env); f != "" {
			info.SQLiteFile = f
			_, err := os.Stat(f)
			info.SQLiteMissing = err != nil
		}
	}
	if herdSite(root) {
		info.HerdURL = "http://" + strings.ToLower(filepath.Base(root)) + ".test"
	}

	for key, dir := range map[string]string{
		"models": "app/Models", "controllers": "app/Http/Controllers", "migrations": "database/migrations",
		"jobs": "app/Jobs", "events": "app/Events", "listeners": "app/Listeners", "policies": "app/Policies",
		"tests": "tests", "livewire": "app/Livewire", "views": "resources/views", "commands": "app/Console/Commands",
		"mail": "app/Mail", "notifications": "app/Notifications", "requests": "app/Http/Requests",
	} {
		info.Counts[key] = countFiles(filepath.Join(root, dir))
	}

	if out, err := git(root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		info.Branch = strings.TrimSpace(out)
		if st, err := git(root, "status", "--porcelain"); err == nil {
			info.Dirty = len(nonEmptyLines(st))
		}
	}
	return info
}

func herdSite(root string) bool {
	// Herd and Valet serve every folder in a parked directory as <name>.test.
	if which("herd") == "" && which("valet") == "" {
		return false
	}
	parent := strings.ToLower(filepath.Base(filepath.Dir(root)))
	return parent == "herd" || parent == "sites" || parent == "valet"
}

func lockedVersions(root string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(filepath.Join(root, "composer.lock"))
	if err != nil {
		return out
	}
	var lock struct {
		Packages    []struct{ Name, Version string } `json:"packages"`
		PackagesDev []struct{ Name, Version string } `json:"packages-dev"`
	}
	if json.Unmarshal(b, &lock) != nil {
		return out
	}
	for _, p := range append(lock.Packages, lock.PackagesDev...) {
		out[p.Name] = p.Version
	}
	return out
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

var nonWord = regexp.MustCompile(`[-_.]+`)

func prettyName(path string) string {
	words := strings.Fields(nonWord.ReplaceAllString(filepath.Base(path), " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// readEnvFile parses a dotenv file; nil when it does not exist.
func readEnvFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := parseEnvLine(sc.Text())
		if ok {
			out[k] = v
		}
	}
	return out
}

func parseEnvLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	k, v, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	k = strings.TrimSpace(k)
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		v = v[1 : len(v)-1]
	} else if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return k, v, true
}
