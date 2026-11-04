package project

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/updater"
)

// A project's Icon is "" (automatic: the avatar of the user or organization
// that owns its git remote, else its initials), "initials", "custom" (an
// image the user picked) or the name of one of the built-in icons.

// ProjectImages are the pictures a project's badge can show, as data URLs.
// Each is "" when the project has none.
type ProjectImages struct {
	// Git is the avatar of the owner of the project's GitHub remote.
	Git string `json:"git"`
	// GitOwner is that owner, e.g. "laravel".
	GitOwner string `json:"gitOwner"`
	// Custom is the image the user picked.
	Custom string `json:"custom"`
}

const maxCustomIcon = 512 << 10

// avatarMaxAge is how long a downloaded avatar (or the knowledge that there is
// none) is kept before it is fetched again.
const avatarMaxAge = 7 * 24 * time.Hour

var (
	avatarClient = &http.Client{Timeout: 8 * time.Second}
	ownerRe      = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	// avatarMu keeps two badges from downloading the same avatar at once.
	avatarMu sync.Mutex
)

func iconsDir() string   { return filepath.Join(store.HomeDir(), "icons") }
func avatarsDir() string { return filepath.Join(store.HomeDir(), "avatars") }

func customIconPath(id string) string { return filepath.Join(iconsDir(), id+".png") }

// Images returns the git avatar and the custom image of a project.
func (s *ProjectService) Images(id string) (ProjectImages, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return ProjectImages{}, errors.New("project not found")
	}
	var out ProjectImages
	if b, err := os.ReadFile(customIconPath(id)); err == nil {
		out.Custom = dataURL(b)
	}
	if owner := githubOwner(remoteURL(p.Path)); owner != "" {
		out.GitOwner = owner
		if b := githubAvatar(owner); b != nil {
			out.Git = dataURL(b)
		}
	}
	return out, nil
}

// SetCustomIcon saves a PNG (as a data URL or raw base64) as the project's
// custom image. "" removes it.
func (s *ProjectService) SetCustomIcon(id, data string) error {
	if _, ok := store.FindProject(id); !ok {
		return errors.New("project not found")
	}
	if data == "" {
		err := os.Remove(customIconPath(id))
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		return err
	}
	if i := strings.Index(data, ","); strings.HasPrefix(data, "data:") && i > 0 {
		data = data[i+1:]
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return errors.New("the image is not valid")
	}
	if len(b) > maxCustomIcon {
		return errors.New("the image is larger than 512 KB")
	}
	if _, err := png.DecodeConfig(bytes.NewReader(b)); err != nil {
		return errors.New("the image is not a PNG")
	}
	if err := os.MkdirAll(iconsDir(), 0o755); err != nil {
		return err
	}
	tmp := customIconPath(id) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, customIconPath(id))
}

func dataURL(b []byte) string {
	mime := http.DetectContentType(b)
	if !strings.HasPrefix(mime, "image/") {
		return ""
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// remoteURL is the URL of the project's origin remote, or of its first one.
func remoteURL(dir string) string {
	if out, err := proc.Git(dir, "remote", "get-url", "origin"); err == nil {
		return strings.TrimSpace(out)
	}
	out, err := proc.Git(dir, "remote")
	if err != nil {
		return ""
	}
	if names := proc.NonEmptyLines(out); len(names) > 0 {
		if u, err := proc.Git(dir, "remote", "get-url", names[0]); err == nil {
			return strings.TrimSpace(u)
		}
	}
	return ""
}

// githubOwner returns the user or organization of a github.com remote URL
// (https, ssh or scp-like), or "".
func githubOwner(remote string) string {
	r := strings.TrimSpace(remote)
	var path string
	switch {
	case strings.Contains(r, "://"):
		u, err := url.Parse(r)
		if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
			return ""
		}
		path = u.Path
	case strings.Contains(r, ":"):
		// git@github.com:owner/repo.git
		host, rest, _ := strings.Cut(r, ":")
		if i := strings.LastIndex(host, "@"); i >= 0 {
			host = host[i+1:]
		}
		if !strings.EqualFold(host, "github.com") {
			return ""
		}
		path = rest
	default:
		return ""
	}
	owner, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	if !ownerRe.MatchString(owner) {
		return ""
	}
	return owner
}

// githubAvatar returns the owner's avatar, downloading it when the cached one
// is missing or old. A failed download keeps an old copy if there is one.
func githubAvatar(owner string) []byte {
	avatarMu.Lock()
	defer avatarMu.Unlock()
	file := filepath.Join(avatarsDir(), "github-"+strings.ToLower(owner)+".png")
	// A ".none" file records that the owner has no avatar we can reach.
	none := file + ".none"
	cached, _ := os.ReadFile(file)
	if st, err := os.Stat(file); err == nil && time.Since(st.ModTime()) < avatarMaxAge {
		return cached
	}
	if st, err := os.Stat(none); err == nil && time.Since(st.ModTime()) < avatarMaxAge {
		return cached
	}
	b, status := fetchAvatar("https://github.com/" + url.PathEscape(owner) + ".png?size=128")
	_ = os.MkdirAll(avatarsDir(), 0o755)
	switch {
	case b != nil:
		_ = os.WriteFile(file, b, 0o644)
		_ = os.Remove(none)
		return b
	case status == http.StatusNotFound:
		_ = os.Remove(file)
		_ = os.WriteFile(none, nil, 0o644)
		return nil
	}
	// Offline or rate limited: try again next time.
	return cached
}

func fetchAvatar(u string) ([]byte, int) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, 0
	}
	req.Header.Set("User-Agent", "larvyn/"+updater.Version)
	res, err := avatarClient.Do(req)
	if err != nil {
		return nil, 0
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, res.StatusCode
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil || !strings.HasPrefix(http.DetectContentType(b), "image/") {
		return nil, 0
	}
	return b, 200
}
