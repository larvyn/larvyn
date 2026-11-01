// Package updater checks the public releases for a newer Larvyn and replaces
// the running installation with it.
package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Releases are published to the public distribution repository.
const releasesAPI = "https://api.github.com/repos/larvyn/releases/releases?per_page=10"

// Version is the running build ("dev" locally); main sets it at start.
var Version = "dev"

// Info tells the window whether a newer release exists.
type Info struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	NotesURL  string `json:"notesUrl"`
	Published string `json:"published"`
	// CanInstall is false when Larvyn cannot replace itself (development
	// build, unsupported platform); the window then only links the notes.
	CanInstall bool   `json:"canInstall"`
	Reason     string `json:"reason"`
}

type ghRelease struct {
	Tag       string `json:"tag_name"`
	URL       string `json:"html_url"`
	Draft     bool   `json:"draft"`
	Published string `json:"published_at"`
}

var httpClient = &http.Client{Timeout: 2 * time.Minute}

// currentVersion is the running version; LARVYN_UPDATE_TEST_VERSION pretends
// to be an older build so the update flow can be tried from a dev build.
func currentVersion() string {
	if v := os.Getenv("LARVYN_UPDATE_TEST_VERSION"); v != "" {
		return v
	}
	return Version
}

// Check looks up the newest release (betas included) and compares it with
// this build.
func Check() (Info, error) {
	info := Info{Current: currentVersion()}
	if info.Current == "dev" {
		info.Reason = "Development build: updates are not checked."
		return info, nil
	}
	rel, err := latestRelease()
	if err != nil {
		return info, err
	}
	info.Latest, info.NotesURL, info.Published = rel.Tag, rel.URL, rel.Published
	info.Available = compareVersions(rel.Tag, info.Current) > 0
	if _, err := updateAsset(); err != nil {
		info.Reason = err.Error()
	} else {
		info.CanInstall = true
	}
	return info, nil
}

func latestRelease() (ghRelease, error) {
	req, _ := http.NewRequest("GET", releasesAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "larvyn/"+Version)
	res, err := httpClient.Do(req)
	if err != nil {
		return ghRelease{}, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return ghRelease{}, fmt.Errorf("GitHub answered %s", res.Status)
	}
	var list []ghRelease
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return ghRelease{}, err
	}
	for _, r := range list {
		if !r.Draft && r.Tag != "" {
			return r, nil
		}
	}
	return ghRelease{}, errors.New("no releases published yet")
}

// updateAsset is the release file for this platform, as install.sh/.ps1 use.
func updateAsset() (string, error) {
	switch {
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		return "larvyn-linux-amd64.tar.gz", nil
	case runtime.GOOS == "darwin":
		return "larvyn-darwin-universal.tar.gz", nil
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return "larvyn-windows-amd64.exe", nil
	}
	return "", fmt.Errorf("automatic updates are not available for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// Install downloads tag, verifies it against the release checksums, replaces
// this installation and starts the new one. The caller quits the running app
// afterwards.
func Install(tag string) error {
	if !strings.HasPrefix(tag, "v") || strings.ContainsAny(tag, "/\\ ") {
		return errors.New("invalid release tag")
	}
	asset, err := updateAsset()
	if err != nil {
		return err
	}
	base := "https://github.com/larvyn/releases/releases/download/" + tag + "/"
	data, err := download(base + asset)
	if err != nil {
		return err
	}
	sums, err := download(base + "checksums.txt")
	if err != nil {
		return err
	}
	if err := verifyChecksum(data, sums, asset); err != nil {
		return err
	}
	relaunch, err := applyUpdate(data)
	if err != nil {
		return err
	}
	if err := relaunch.Start(); err != nil {
		return fmt.Errorf("updated, but could not restart Larvyn: %w (open it again)", err)
	}
	return nil
}

func download(url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "larvyn/"+Version)
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("download failed: %s (%s)", res.Status, url)
	}
	return io.ReadAll(io.LimitReader(res.Body, 200<<20))
}

func verifyChecksum(data, sums []byte, asset string) error {
	got := sha256.Sum256(data)
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			if strings.EqualFold(f[0], hex.EncodeToString(got[:])) {
				return nil
			}
			return errors.New("the downloaded update does not match its checksum; nothing was changed")
		}
	}
	return fmt.Errorf("%s is missing from checksums.txt", asset)
}

// applyUpdate swaps the new build in and returns the command that starts it.
func applyUpdate(data []byte) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return nil, err
	}
	switch runtime.GOOS {
	case "windows":
		// A running .exe cannot be overwritten but can be renamed.
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return nil, notWritable(exe, err)
		}
		if err := os.WriteFile(exe, data, 0o755); err != nil {
			_ = os.Rename(old, exe)
			return nil, notWritable(exe, err)
		}
		cmd := exec.Command(exe)
		return cmd, nil
	case "darwin":
		app := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // …/Larvyn.app/Contents/MacOS/larvyn
		if !strings.HasSuffix(app, ".app") {
			return nil, errors.New("Larvyn is not running from an .app bundle; run the install command to update")
		}
		staging, err := os.MkdirTemp(filepath.Dir(app), ".larvyn-update-")
		if err != nil {
			return nil, notWritable(app, err)
		}
		defer os.RemoveAll(staging)
		if err := untar(data, staging); err != nil {
			return nil, err
		}
		fresh := filepath.Join(staging, "larvyn.app")
		if _, err := os.Stat(fresh); err != nil {
			return nil, errors.New("the update archive has no larvyn.app")
		}
		old := filepath.Join(staging, "previous.app")
		if err := os.Rename(app, old); err != nil {
			return nil, notWritable(app, err)
		}
		if err := os.Rename(fresh, app); err != nil {
			_ = os.Rename(old, app)
			return nil, notWritable(app, err)
		}
		return exec.Command("open", "-n", app), nil
	default:
		staging, err := os.MkdirTemp(filepath.Dir(exe), ".larvyn-update-")
		if err != nil {
			return nil, notWritable(exe, err)
		}
		defer os.RemoveAll(staging)
		if err := untar(data, staging); err != nil {
			return nil, err
		}
		fresh := filepath.Join(staging, "larvyn")
		if err := os.Chmod(fresh, 0o755); err != nil {
			return nil, errors.New("the update archive has no larvyn binary")
		}
		// Renaming over a running binary is fine on Linux: the old inode
		// lives until this process exits.
		if err := os.Rename(fresh, exe); err != nil {
			return nil, notWritable(exe, err)
		}
		return exec.Command(exe), nil
	}
}

func notWritable(path string, err error) error {
	return fmt.Errorf("could not replace %s (%v); run the install command again to update", path, err)
}

// untar extracts a .tar.gz into dir, refusing paths that escape it.
func untar(data []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("the update archive is damaged: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("the update archive is damaged: %w", err)
		}
		target := filepath.Join(dir, h.Name)
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe path in update archive: %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o777)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			f.Close()
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			// .app bundles may contain relative symlinks inside the bundle.
			if filepath.IsAbs(h.Linkname) || strings.Contains(h.Linkname, "..") {
				return fmt.Errorf("unsafe link in update archive: %s", h.Name)
			}
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		}
	}
}

// Cleanup removes what a previous update left behind (Windows keeps the
// replaced .exe until the next start).
func Cleanup() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}

// compareVersions compares two tags like v0.1.0-beta.10 by semver rules:
// a prerelease sorts before its release, numeric parts compare as numbers.
func compareVersions(a, b string) int {
	ma, pa, _ := strings.Cut(strings.TrimPrefix(a, "v"), "-")
	mb, pb, _ := strings.Cut(strings.TrimPrefix(b, "v"), "-")
	if c := compareDotted(ma, mb, true); c != 0 {
		return c
	}
	switch {
	case pa == pb:
		return 0
	case pa == "":
		return 1
	case pb == "":
		return -1
	}
	return compareDotted(pa, pb, false)
}

func compareDotted(a, b string, pad bool) int {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(x) || i < len(y); i++ {
		if i >= len(x) || i >= len(y) {
			if pad {
				continue
			}
			if i >= len(x) {
				return -1
			}
			return 1
		}
		nx, ex := strconv.Atoi(x[i])
		ny, ey := strconv.Atoi(y[i])
		switch {
		case ex == nil && ey == nil:
			if nx != ny {
				if nx < ny {
					return -1
				}
				return 1
			}
		case ex == nil:
			return -1
		case ey == nil:
			return 1
		default:
			if c := strings.Compare(x[i], y[i]); c != 0 {
				return c
			}
		}
	}
	return 0
}
