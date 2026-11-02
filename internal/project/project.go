package project

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"larvyn/internal/dotenv"
	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/terminal"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ProjectInfo is a project plus everything Larvyn can learn from its files.
type ProjectInfo struct {
	store.Project
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
	application.RegisterEvent[[]store.Project]("projects:changed")
}

func (s *ProjectService) changed() {
	emit("projects:changed", store.Projects())
}

// List returns the saved projects, pinned and recently opened first.
func (s *ProjectService) List() []store.Project {
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
func (s *ProjectService) Add(path string) (store.Project, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return store.Project{}, errors.New("folder not found: " + path)
	}
	for _, p := range store.Projects() {
		if strings.EqualFold(filepath.Clean(p.Path), path) {
			return p, nil
		}
	}
	n := len(store.Projects())
	p := store.Project{
		ID:           store.NewID(),
		Name:         prettyName(path),
		Path:         path,
		Color:        palette[n%len(palette)],
		Port:         8000 + n,
		AddedAt:      time.Now().UnixMilli(),
		LastOpenedAt: time.Now().UnixMilli(),
	}
	if env := dotenv.Read(filepath.Join(path, ".env")); env["APP_NAME"] != "" && env["APP_NAME"] != "Laravel" {
		p.Name = env["APP_NAME"]
	}
	err = store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) })
	s.changed()
	return p, err
}

// Pick asks for a folder and adds it.
func (s *ProjectService) Pick() (*store.Project, error) {
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

// closeSessions stops and closes every terminal of a project.
func closeSessions(id string) {
	for _, sess := range terminal.Sessions.List() {
		if sess.ProjectID == id {
			_ = terminal.Sessions.Close(sess.ID)
		}
	}
}

// Close stops a project's terminals and dev services and leaves it, so the
// window goes back to the welcome screen. The project stays in Larvyn.
func (s *ProjectService) Close(id string) error {
	closeSessions(id)
	return store.Update(func(d *store.State) {
		if d.Active == id {
			d.Active = ""
		}
	})
}

// Remove forgets a project; its files stay.
func (s *ProjectService) Remove(id string) error {
	closeSessions(id)
	err := store.Update(func(d *store.State) {
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
func (s *ProjectService) Update(p store.Project) error {
	err := store.Update(func(d *store.State) {
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
	_ = store.Update(func(d *store.State) {
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
	return store.ActiveID()
}

// Info inspects a project's files.
func (s *ProjectService) Info(id string) (ProjectInfo, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return ProjectInfo{}, errors.New("project not found")
	}
	return Inspect(p), nil
}

// EnsureSQLite creates the SQLite database file a project's .env points at
// when it is missing, so a fresh app does not fail with "Database file at
// path [...] does not exist". It reports whether a file was created.
func (s *ProjectService) EnsureSQLite(id string) (bool, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return false, errors.New("project not found")
	}
	path := sqlitePath(p.Path, dotenv.Read(filepath.Join(p.Path, ".env")))
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

// doubledPortRe matches an APP_URL whose port was appended twice, as old
// laravel/installer versions write it for Laravel 13 (http://localhost:8000:8000).
var doubledPortRe = regexp.MustCompile(`(?m)^(APP_URL=["']?https?://[^\s:/"']+:(\d+)):(\d+)(["']?)[ \t]*$`)

// RepairEnv fixes known mistakes that scaffolding tools leave in .env and
// reports whether it changed anything. Today that is a doubled APP_URL port,
// which makes every artisan command fail with "Host is malformed".
func (s *ProjectService) RepairEnv(id string) (bool, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return false, errors.New("project not found")
	}
	path := filepath.Join(p.Path, ".env")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	fixed, changed := repairAppURL(string(b))
	if !changed {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(fixed), 0o644)
}

// repairAppURL turns APP_URL=http://host:8000:8000 back into http://host:8000.
func repairAppURL(env string) (string, bool) {
	changed := false
	out := doubledPortRe.ReplaceAllStringFunc(env, func(line string) string {
		m := doubledPortRe.FindStringSubmatch(line)
		if m[2] != m[3] {
			return line
		}
		changed = true
		return m[1] + m[4]
	})
	return out, changed
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

func Inspect(p store.Project) ProjectInfo {
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
	if env := dotenv.Read(filepath.Join(root, ".env")); env != nil {
		info.HasEnv = true
		info.AppName = env["APP_NAME"]
		info.AppURL = env["APP_URL"]
		info.AppEnv = env["APP_ENV"]
		info.AppDebug = strings.EqualFold(env["APP_DEBUG"], "true")
		info.HasAppKey = env["APP_KEY"] != ""
		info.DBConnection = env["DB_CONNECTION"]
		info.DBDatabase = env["DB_DATABASE"]
		info.QueueConnection = env["QUEUE_CONNECTION"]
		info.CacheStore = proc.FirstNonEmpty(env["CACHE_STORE"], env["CACHE_DRIVER"])
		info.SessionDriver = env["SESSION_DRIVER"]
		info.MailMailer = proc.FirstNonEmpty(env["MAIL_MAILER"], env["MAIL_DRIVER"])
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

	if out, err := proc.Git(root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		info.Branch = strings.TrimSpace(out)
		if st, err := proc.Git(root, "status", "--porcelain"); err == nil {
			info.Dirty = len(proc.NonEmptyLines(st))
		}
	}
	return info
}

func herdSite(root string) bool {
	// Herd and Valet serve every folder in a parked directory as <name>.test.
	if proc.Which("herd") == "" && proc.Which("valet") == "" {
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

var nonWord = regexp.MustCompile(`[-_.]+`)

func prettyName(path string) string {
	words := strings.Fields(nonWord.ReplaceAllString(filepath.Base(path), " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// emit sends an event to the window, once the app is running.
func emit(name string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data)
	}
}
