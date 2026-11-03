package project

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"larvyn/internal/dotenv"
	"larvyn/internal/flock"
	"larvyn/internal/store"
)

// Flock is optional: everything here only does something when it is installed.

// FlockInfo is what the Overview's Local site card shows for a project.
type FlockInfo struct {
	// State is flock.State(): "" (not installed), "installed" (not set up
	// yet), "stopped" or "running".
	State   string `json:"state"`
	Version string `json:"version"`
	// Bin is Flock's command, for its setup in Larvyn's console.
	Bin string `json:"bin"`
	// Domain is where `flk link` serves the project: <folder>.test.
	Domain string `json:"domain"`
	// Site is the project's site, nil while Flock does not serve it.
	Site      *flock.Site        `json:"site"`
	Worktrees []flock.Site       `json:"worktrees"`
	PHP       []flock.PHPVersion `json:"php"`
}

// Flock describes how Flock serves the project.
func (s *ProjectService) Flock(id string) (FlockInfo, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return FlockInfo{}, errors.New("project not found")
	}
	flock.Forget()
	info := FlockInfo{State: flock.State(), Bin: flock.Bin(), Domain: siteName(filepath.Base(p.Path)) + ".test"}
	if info.State != flock.Running {
		return info, nil
	}
	info.Version = flock.Version()
	if site, ok := flock.SiteFor(p.Path); ok {
		info.Site = &site
		for _, w := range flock.Sites() {
			if w.Parent == site.Name {
				info.Worktrees = append(info.Worktrees, w)
			}
		}
	}
	info.PHP, _ = flock.PHPVersions()
	return info, nil
}

// FlockServe serves the project at <folder>.test and points APP_URL at it
// when APP_URL was still a default. It returns the site's URL.
func (s *ProjectService) FlockServe(id string) (string, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return "", errors.New("project not found")
	}
	site, err := flock.Link(p.Path)
	if err != nil {
		return "", err
	}
	syncAppURL(p, "", site.URL)
	return site.URL, nil
}

// FlockUnserve stops serving the project. Worktrees go with it.
func (s *ProjectService) FlockUnserve(id string) error {
	site, err := projectSite(id)
	if err != nil {
		return err
	}
	if site.Parked {
		return errors.New("Flock serves every folder in " + filepath.Dir(site.Path) + ": remove that project folder in Flock's settings to stop serving it")
	}
	return flock.Unlink(site.Name)
}

// FlockSecure turns HTTPS on or off and moves APP_URL along. It returns the
// new URL.
func (s *ProjectService) FlockSecure(id string, on bool) (string, error) {
	site, err := projectSite(id)
	if err != nil {
		return "", err
	}
	if err := flock.Secure(site.Name, on); err != nil {
		return "", err
	}
	p, _ := store.FindProject(id)
	now, ok := flock.SiteFor(p.Path)
	if !ok {
		return "", errors.New("Flock no longer serves this project")
	}
	syncAppURL(p, site.URL, now.URL)
	return now.URL, nil
}

// FlockUsePHP makes the project's site use a PHP minor version, installing it
// first when Flock does not have it yet; "" goes back to the default.
func (s *ProjectService) FlockUsePHP(id, minor string) error {
	site, err := projectSite(id)
	if err != nil {
		return err
	}
	if minor != "" {
		versions, err := flock.PHPVersions()
		if err != nil {
			return err
		}
		installed := false
		for _, v := range versions {
			installed = installed || (v.Minor == minor && v.Installed != "")
		}
		if !installed {
			if err := flock.InstallPHP(minor); err != nil {
				return err
			}
		}
	}
	return flock.UsePHP(site.Name, minor)
}

// FlockStart starts Flock's daemon when it is set up but not running.
func (s *ProjectService) FlockStart() error {
	return flock.Start()
}

// FlockServed is sent when Larvyn served a project with Flock by itself.
type FlockServed struct {
	ProjectID string `json:"projectId"`
	URL       string `json:"url"`
}

// autoServe serves a newly added Laravel app with Flock when Flock runs and
// does not serve it yet, so it has its .test URL without a command.
func (s *ProjectService) autoServe(p store.Project) {
	if store.CurrentSettings().NoFlockAutoServe || flock.State() != flock.Running {
		return
	}
	if _, err := os.Stat(filepath.Join(p.Path, "artisan")); err != nil {
		return
	}
	if _, ok := flock.SiteFor(p.Path); ok {
		return
	}
	if site, err := flock.Link(p.Path); err == nil {
		syncAppURL(p, "", site.URL)
		emit("flock:served", FlockServed{ProjectID: p.ID, URL: site.URL})
	}
}

func projectSite(id string) (flock.Site, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return flock.Site{}, errors.New("project not found")
	}
	flock.Forget()
	site, ok := flock.SiteFor(p.Path)
	if !ok {
		return flock.Site{}, errors.New("Flock does not serve this project")
	}
	return site, nil
}

// syncAppURL points APP_URL in the project's .env at url when it still names
// a default (empty or localhost) or the site's previous URL, so Laravel's
// links, redirects and Vite follow the site. A URL the user chose stays; the
// Doctor points out the difference instead. The old .env is backed up.
func syncAppURL(p store.Project, previous, next string) bool {
	path := filepath.Join(p.Path, ".env")
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		key, value, ok := dotenv.ParseLine(line)
		if !ok || key != "APP_URL" {
			continue
		}
		if !replaceableAppURL(value, previous) || sameURL(value, next) {
			return false
		}
		lines[i] = "APP_URL=" + next
		backup := filepath.Join(store.HomeDir(), "backups")
		_ = os.MkdirAll(backup, 0o755)
		_ = os.WriteFile(filepath.Join(backup, fmt.Sprintf("%s-.env-%d", p.ID, time.Now().Unix())), raw, 0o600)
		return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600) == nil
	}
	return false
}

func replaceableAppURL(value, previous string) bool {
	if strings.TrimSpace(value) == "" || (previous != "" && sameURL(value, previous)) {
		return true
	}
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "0.0.0.0", "[::1]", "::1":
		return true
	}
	return false
}

func sameURL(a, b string) bool {
	trim := func(s string) string { return strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/")) }
	return trim(a) == trim(b)
}

// siteName is the name Flock gives a folder: lower case, every other
// character than a-z, 0-9 and - turned into -.
func siteName(folder string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(folder) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
