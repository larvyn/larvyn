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

// FlockUnserve stops serving the project (worktrees go with it) and puts
// APP_URL back when it named the site.
func (s *ProjectService) FlockUnserve(id string) error {
	site, err := projectSite(id)
	if err != nil {
		return err
	}
	if site.Parked {
		return errors.New("Flock serves every folder in " + filepath.Dir(site.Path) + ": remove that project folder in Flock's settings to stop serving it")
	}
	if err := flock.Unlink(site.Name); err != nil {
		return err
	}
	// The app runs without Flock from now on: APP_URL goes back with it.
	p, _ := store.FindProject(id)
	if sameSite(dotenv.Read(filepath.Join(p.Path, ".env"))["APP_URL"], site.URL) {
		_, err := s.RestoreAppURL(id)
		return err
	}
	// The user set APP_URL to something else meanwhile: that value stays, and
	// the one remembered from before is stale, so a later serve and stop
	// cannot bring it back over theirs.
	return rememberAppURL(id, "", true)
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
// Doctor points out the difference instead.
func syncAppURL(p store.Project, previous, next string) bool {
	value, ok := dotenv.Read(filepath.Join(p.Path, ".env"))["APP_URL"]
	if !ok || !replaceableAppURL(value, previous) || sameURL(value, next) {
		return false
	}
	return setAppURL(p, next, previous == "") == nil
}

// setAppURL writes APP_URL into the project's .env, after backing the file
// up. With remember, the APP_URL it replaces is kept (once) so RestoreAppURL
// can bring it back when Flock no longer serves the app.
func setAppURL(p store.Project, value string, remember bool) error {
	path := filepath.Join(p.Path, ".env")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	old, found := "", false
	for i, line := range lines {
		if key, v, ok := dotenv.ParseLine(line); ok && key == "APP_URL" {
			old, found = v, true
			lines[i] = "APP_URL=" + value
		}
	}
	if !found {
		return errors.New("APP_URL is not in .env")
	}
	// Save the URL to come back to before touching .env, so a failure
	// here never leaves .env pointing at Flock with nothing to restore.
	if remember && old != "" && !sameURL(old, value) {
		if err := rememberAppURL(p.ID, old, false); err != nil {
			return fmt.Errorf("could not save the current APP_URL: %w", err)
		}
	}
	if err := backupEnv(p.ID, raw); err != nil {
		return fmt.Errorf("could not back up .env: %w", err)
	}
	return replaceFile(path, []byte(strings.Join(lines, "\n")))
}

// rememberAppURL stores the APP_URL to restore when Flock stops serving the
// project: only when none is stored yet, unless overwrite ("" forgets it).
func rememberAppURL(id, url string, overwrite bool) error {
	return store.Update(func(d *store.State) {
		for i := range d.Projects {
			if d.Projects[i].ID == id && (overwrite || d.Projects[i].AppURLBeforeFlock == "") {
				d.Projects[i].AppURLBeforeFlock = url
			}
		}
	})
}

// backupEnv keeps a copy of a .env about to change in Larvyn's backups
// folder, under a name no other backup can take.
func backupEnv(id string, raw []byte) error {
	dir := filepath.Join(store.HomeDir(), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, fmt.Sprintf("%s-.env-%d-*", id, time.Now().Unix()))
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	return f.Close()
}

// replaceFile writes data next to path and renames it over path once it is
// complete, so a failed write never leaves a truncated file. The file keeps
// its permissions.
func replaceFile(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".larvyn-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// FlockUseSiteURL points APP_URL at the URL Flock serves the app at,
// remembering the one it had.
func (s *ProjectService) FlockUseSiteURL(id string) error {
	site, err := projectSite(id)
	if err != nil {
		return err
	}
	p, _ := store.FindProject(id)
	return setAppURL(p, site.URL, true)
}

// RestoreAppURL puts back the APP_URL the project had before Flock served it
// (artisan serve's URL when Larvyn did not see one), for running the app
// without Flock. It returns the URL it set.
func (s *ProjectService) RestoreAppURL(id string) (string, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return "", errors.New("project not found")
	}
	value := p.AppURLBeforeFlock
	if value == "" {
		port := p.Port
		if port == 0 {
			port = 8000
		}
		value = fmt.Sprintf("http://127.0.0.1:%d", port)
	}
	if err := setAppURL(p, value, false); err != nil {
		return "", err
	}
	if err := rememberAppURL(id, "", true); err != nil {
		return "", err
	}
	return value, nil
}

func replaceableAppURL(value, previous string) bool {
	if strings.TrimSpace(value) == "" || (previous != "" && sameSite(value, previous)) {
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

// sameSite reports whether two URLs name the same host and port, whatever
// their scheme: APP_URL still names Flock's site after HTTPS was switched in
// Flock's own app, which changes only the scheme.
func sameSite(a, b string) bool {
	ua, errA := url.Parse(strings.TrimSpace(a))
	ub, errB := url.Parse(strings.TrimSpace(b))
	if errA != nil || errB != nil || ua.Host == "" {
		return false
	}
	return strings.EqualFold(ua.Host, ub.Host)
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
