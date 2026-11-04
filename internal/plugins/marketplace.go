package plugins

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"larvyn/internal/store"
	"larvyn/internal/updater"
)

// IndexURL is the marketplace index, built by github.com/larvyn/plugins.
var IndexURL = "https://raw.githubusercontent.com/larvyn/plugins/main/plugins.json"

var marketClient = &http.Client{Timeout: time.Minute}

// Listing is one plugin in the marketplace. Name, permissions and network
// come from the larvyn.json in its zip, read when the registry built the
// index; Larvyn checks the zip again before installing it.
type Listing struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Larvyn      string   `json:"larvyn"`
	Permissions []string `json:"permissions"`
	Network     []string `json:"network"`
	Repo        string   `json:"repo"`
	Download    string   `json:"download"`
	Sha256      string   `json:"sha256"`
	Size        int64    `json:"size"`

	// Installed is the installed version, "" when it isn't installed.
	Installed string `json:"installed"`
	// Update is set when the installed copy came from the marketplace and
	// this version is newer. Plugins installed from a folder or a .zip are
	// never updated from here, so a local copy is never overwritten.
	Update bool `json:"update"`
	// Error says why this version can't be installed on this Larvyn.
	Error string `json:"error,omitempty"`
}

// Revocation stops a plugin the registry found harmful. No Versions means
// every version.
type Revocation struct {
	ID       string   `json:"id"`
	Versions []string `json:"versions,omitempty"`
	Reason   string   `json:"reason"`
}

type index struct {
	APIVersion int          `json:"apiVersion"`
	Plugins    []Listing    `json:"plugins"`
	Revoked    []Revocation `json:"revoked"`
}

func fetchIndex() (index, error) {
	var ix index
	req, _ := http.NewRequest("GET", IndexURL, nil)
	req.Header.Set("User-Agent", "larvyn/"+updater.Version)
	res, err := marketClient.Do(req)
	if err != nil {
		return ix, fmt.Errorf("could not reach the marketplace: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return ix, fmt.Errorf("the marketplace answered %s", res.Status)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&ix); err != nil {
		return ix, fmt.Errorf("the marketplace index is damaged: %w", err)
	}
	if ix.APIVersion != 1 {
		return ix, fmt.Errorf("the marketplace index is version %d; update Larvyn", ix.APIVersion)
	}
	return ix, nil
}

// Marketplace lists the marketplace's plugins, marking the installed ones
// and their updates. It also applies the registry's revocations.
func (s *PluginService) Marketplace() ([]Listing, error) {
	ix, err := fetchIndex()
	if err != nil {
		return nil, err
	}
	applyRevoked(ix.Revoked)
	installed := map[string]Plugin{}
	for _, p := range Load() {
		if p.Folder == p.Manifest.ID {
			installed[p.Manifest.ID] = p
		}
	}
	for i := range ix.Plugins {
		l := &ix.Plugins[i]
		if !Compatible(l.Larvyn, updater.Version) {
			l.Error = incompatible(l.Larvyn, updater.Version).Error()
		}
		if p, ok := installed[l.ID]; ok {
			l.Installed = p.Manifest.Version
			// A version this Larvyn can't run is not an update.
			l.Update = l.Error == "" && p.Source == "marketplace" && !p.Linked && updater.CompareVersions(l.Version, p.Manifest.Version) > 0
		}
	}
	return ix.Plugins, nil
}

// Revocation is best-effort and never needs the network to turn a plugin
// on. Larvyn enforces the newest revocation list it has seen: it fetches
// the list when it starts and whenever the marketplace loads, and keeps it
// for the rest of the run. With that list, a revoked version is turned off,
// can't be turned on, and can't be installed, from any source. Before the
// first list arrives, or offline, nothing is refused: a plugin a
// revocation should stop is stopped when a list next arrives, and a
// revocation already recorded for an installed plugin (Revoked in its
// state) keeps refusing it meanwhile.
var (
	revokedMu sync.Mutex
	revoked   []Revocation
)

// revokedReason is why the newest known list revokes this version, or "".
func revokedReason(id, version string) string {
	revokedMu.Lock()
	defer revokedMu.Unlock()
	return reasonIn(revoked, id, version)
}

func reasonIn(list []Revocation, id, version string) string {
	for _, r := range list {
		if r.ID == id && (len(r.Versions) == 0 || slices.Contains(r.Versions, version)) {
			return r.Reason
		}
	}
	return ""
}

// CheckRevoked applies the registry's revocations; Larvyn calls it when it
// starts. Without a network it does nothing.
func CheckRevoked() {
	if ix, err := fetchIndex(); err == nil {
		applyRevoked(ix.Revoked)
	}
}

// applyRevoked turns off every installed plugin whose version is revoked
// and records why, and clears the reason once a version is no longer
// revoked (the plugin stays off until the user turns it on).
func applyRevoked(list []Revocation) {
	reason := func(p Plugin) string { return reasonIn(list, p.Manifest.ID, p.Manifest.Version) }
	revokedMu.Lock()
	revoked = list
	revokedMu.Unlock()
	runMu.Lock()
	defer runMu.Unlock()
	var stop []string
	plugins := Load() // before Update: Load reads the store
	_ = store.Update(func(d *store.State) {
		for _, p := range plugins {
			st, ok := d.Plugins[p.Manifest.ID]
			why := reason(p)
			if !ok || p.Folder != p.Manifest.ID || st.Revoked == why {
				continue
			}
			if why != "" {
				st.Enabled = false
				stop = append(stop, p.Manifest.ID)
			}
			st.Revoked = why
			d.Plugins[p.Manifest.ID] = st
		}
	})
	for _, id := range stop {
		stopSessions(id, "")
	}
}

// InspectListing downloads a marketplace plugin and reads it without
// installing it, for the permission dialog.
func (s *PluginService) InspectListing(id, version string) (Plugin, error) {
	zip, cleanup, err := downloadListing(id, version)
	if err != nil {
		return Plugin{}, err
	}
	defer cleanup()
	p, err := s.InspectZip(zip)
	if err == nil {
		err = sameListing(p.Manifest, id, version)
	}
	return p, err
}

// InstallListing installs the version of a marketplace plugin the user saw
// in the permission dialog; granted must hold every permission it asks for.
func (s *PluginService) InstallListing(id, version string, granted []string) (Plugin, error) {
	zip, cleanup, err := downloadListing(id, version)
	if err != nil {
		return Plugin{}, err
	}
	defer cleanup()
	dir, done, err := unpackPlugin(zip)
	if err != nil {
		return Plugin{}, err
	}
	defer done()
	m, err := ReadManifest(dir)
	if err != nil {
		return Plugin{}, err
	}
	if err := sameListing(m, id, version); err != nil {
		return Plugin{}, err
	}
	return install(dir, granted, "marketplace")
}

func sameListing(m Manifest, id, version string) error {
	if m.ID != id || m.Version != version {
		return fmt.Errorf("the download is %s %s, not %s %s; nothing was installed", m.ID, m.Version, id, version)
	}
	return nil
}

// downloadListing saves the listing's zip to a temporary file after
// checking its size and sha256 against the index.
func downloadListing(id, version string) (string, func(), error) {
	ix, err := fetchIndex()
	if err != nil {
		return "", nil, err
	}
	i := slices.IndexFunc(ix.Plugins, func(l Listing) bool { return l.ID == id })
	if i < 0 {
		return "", nil, fmt.Errorf("%s is not in the marketplace", id)
	}
	l := ix.Plugins[i]
	if l.Version != version {
		return "", nil, fmt.Errorf("the marketplace now has %s %s, not %s; look again", id, l.Version, version)
	}
	if reasonIn(ix.Revoked, id, version) != "" {
		return "", nil, fmt.Errorf("%s %s was revoked", id, version)
	}
	req, _ := http.NewRequest("GET", l.Download, nil)
	req.Header.Set("User-Agent", "larvyn/"+updater.Version)
	res, err := marketClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("download failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", nil, fmt.Errorf("download failed: %s", res.Status)
	}
	f, err := os.CreateTemp("", "larvyn-market-*.zip")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.Remove(f.Name()) }
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, maxInstallBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
	case n > maxInstallBytes:
		err = tooLarge()
	case fmt.Sprintf("%x", h.Sum(nil)) != l.Sha256:
		err = errors.New("the download does not match the marketplace's checksum; nothing was installed")
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, nil
}
