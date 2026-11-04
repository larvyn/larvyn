package plugins

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"larvyn/internal/store"
	"larvyn/internal/updater"
)

// market serves a marketplace index and the zips it lists. Changing its
// fields changes what the next request sees.
type market struct {
	zips    map[string][]byte // download path -> zip
	plugins []Listing
	revoked []Revocation
}

func (m *market) publish(t *testing.T, srv *httptest.Server, version string) {
	t.Helper()
	manifest := edit(t, func(x map[string]any) { x["version"] = version })
	data, err := os.ReadFile(makeZip(t, entry{name: "acme.horizon/larvyn.json", body: manifest}, entry{name: "acme.horizon/ui/index.html", body: indexHTML}))
	if err != nil {
		t.Fatal(err)
	}
	path := "/acme.horizon-" + version + ".zip"
	m.zips[path] = data
	m.plugins = []Listing{{
		ID: "acme.horizon", Name: "Horizon", Version: version, Larvyn: ">=0.1.0", Permissions: horizonPerms,
		Download: srv.URL + path, Sha256: fmt.Sprintf("%x", sha256.Sum256(data)), Size: int64(len(data)),
	}}
}

func newMarket(t *testing.T) (*market, *httptest.Server) {
	t.Helper()
	reset(t)
	m := &market{zips: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/plugins.json" {
			json.NewEncoder(w).Encode(index{APIVersion: 1, Plugins: m.plugins, Revoked: m.revoked})
			return
		}
		if z, ok := m.zips[r.URL.Path]; ok {
			w.Write(z)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	old := IndexURL
	IndexURL = srv.URL + "/plugins.json"
	t.Cleanup(func() { IndexURL = old })
	return m, srv
}

func listing(t *testing.T, s *PluginService) Listing {
	t.Helper()
	l, err := s.Marketplace()
	if err != nil || len(l) != 1 {
		t.Fatalf("Marketplace = %v, %v", l, err)
	}
	return l[0]
}

func TestMarketplaceInstallAndUpdate(t *testing.T) {
	m, srv := newMarket(t)
	m.publish(t, srv, "1.0.0")
	s := &PluginService{}

	if l := listing(t, s); l.Installed != "" || l.Update {
		t.Fatalf("not installed yet: %+v", l)
	}
	p, err := s.InspectListing("acme.horizon", "1.0.0")
	if err != nil || p.Manifest.Version != "1.0.0" {
		t.Fatalf("InspectListing = %+v, %v", p.Manifest, err)
	}
	if _, err := s.InstallListing("acme.horizon", "1.0.0", nil); err == nil {
		t.Error("installing without granting its permissions must fail")
	}
	if p, err = s.InstallListing("acme.horizon", "1.0.0", horizonPerms); err != nil || !p.Enabled || p.Source != "marketplace" {
		t.Fatalf("InstallListing = %+v, %v", p, err)
	}

	// What the user decided survives the update.
	if err := store.Update(func(d *store.State) {
		st := d.Plugins["acme.horizon"]
		st.OffIn, st.Confirmed = []string{"proj"}, map[string]string{"command:restart": "php artisan horizon:terminate"}
		d.Plugins["acme.horizon"] = st
	}); err != nil {
		t.Fatal(err)
	}
	m.publish(t, srv, "1.1.0")
	if l := listing(t, s); l.Installed != "1.0.0" || !l.Update {
		t.Fatalf("1.1.0 is an update: %+v", l)
	}
	if _, err := s.InstallListing("acme.horizon", "1.0.0", horizonPerms); err == nil || !strings.Contains(err.Error(), "now has") {
		t.Errorf("a version the marketplace no longer lists: %v", err)
	}
	if _, err := s.InstallListing("acme.horizon", "1.1.0", horizonPerms); err != nil {
		t.Fatal(err)
	}
	st := store.Plugins()["acme.horizon"]
	if len(st.OffIn) != 1 || st.Confirmed["command:restart"] == "" || st.Source != "marketplace" {
		t.Errorf("state after the update: %+v", st)
	}
	if l := listing(t, s); l.Installed != "1.1.0" || l.Update {
		t.Errorf("up to date: %+v", l)
	}

	// A newer version this Larvyn can't run is not offered as an update.
	m.publish(t, srv, "1.2.0")
	m.plugins[0].Larvyn = ">=99.0.0"
	old := updater.Version
	updater.Version = "v0.1.0-beta.11" // "dev" runs everything
	t.Cleanup(func() { updater.Version = old })
	if l := listing(t, s); l.Update || l.Error == "" {
		t.Errorf("incompatible version: %+v", l)
	}
}

func TestMarketplaceLeavesLocalPluginsAlone(t *testing.T) {
	m, srv := newMarket(t)
	m.publish(t, srv, "2.0.0")
	s := &PluginService{}
	dir := filepath.Join(t.TempDir(), "acme.horizon")
	writePlugin(t, dir, validManifest) // 1.0.0, from a folder
	if _, err := s.Install(dir, horizonPerms); err != nil {
		t.Fatal(err)
	}
	if l := listing(t, s); l.Installed != "1.0.0" || l.Update {
		t.Errorf("a plugin installed from a folder must not be offered marketplace updates: %+v", l)
	}
}

func TestMarketplaceRefusesABadDownload(t *testing.T) {
	m, srv := newMarket(t)
	m.publish(t, srv, "1.0.0")
	s := &PluginService{}

	m.plugins[0].Sha256 = strings.Repeat("0", 64)
	if _, err := s.InstallListing("acme.horizon", "1.0.0", horizonPerms); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Errorf("wrong checksum: %v", err)
	}
	// A zip that holds another version than its listing says.
	m.publish(t, srv, "1.0.0")
	good := m.plugins[0]
	m.publish(t, srv, "9.9.9")
	m.plugins[0].Version = good.Version
	if _, err := s.InstallListing("acme.horizon", "1.0.0", horizonPerms); err == nil || !strings.Contains(err.Error(), "nothing was installed") {
		t.Errorf("mismatched zip: %v", err)
	}
	if _, err := s.InstallListing("nobody.nothing", "1.0.0", nil); err == nil {
		t.Error("a plugin the marketplace doesn't list")
	}
	if len(Load()) != 0 {
		t.Error("nothing should be installed")
	}
}

func TestRevokedPluginTurnsOff(t *testing.T) {
	m, srv := newMarket(t)
	m.publish(t, srv, "1.0.0")
	s := &PluginService{}
	if _, err := s.InstallListing("acme.horizon", "1.0.0", horizonPerms); err != nil {
		t.Fatal(err)
	}

	m.revoked = []Revocation{{ID: "acme.horizon", Versions: []string{"0.9.0"}, Reason: "old"}}
	CheckRevoked()
	if p, _ := find("acme.horizon"); !p.Enabled || p.Revoked != "" {
		t.Fatalf("another version is revoked, not this one: %+v", p)
	}

	m.revoked = []Revocation{{ID: "acme.horizon", Reason: "Sends .env values away."}}
	CheckRevoked()
	p, _ := find("acme.horizon")
	if p.Enabled || p.Revoked != "Sends .env values away." {
		t.Fatalf("revoked: %+v", p)
	}
	if _, err := s.Enable("acme.horizon", horizonPerms); err == nil {
		t.Error("a revoked plugin must not turn on")
	}
	if _, err := s.InspectListing("acme.horizon", "1.0.0"); err == nil {
		t.Error("a revoked version must not install")
	}

	m.revoked = nil
	CheckRevoked()
	if p, _ := find("acme.horizon"); p.Revoked != "" || p.Enabled {
		t.Errorf("no longer revoked, but stays off until turned on: %+v", p)
	}
	if _, err := s.Enable("acme.horizon", horizonPerms); err != nil {
		t.Error(err)
	}
}

func TestPluginIcon(t *testing.T) {
	m, srv := newMarket(t)
	m.publish(t, srv, "1.0.0")
	s := &PluginService{}
	dir := filepath.Join(Dir(), "acme.horizon")
	writePlugin(t, dir, validManifest)
	if p, _ := find("acme.horizon"); p.Icon != "" {
		t.Errorf("no icon.svg, no icon: %q", p.Icon)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64"/></svg>`
	os.WriteFile(filepath.Join(dir, IconFile), []byte(svg), 0o644)
	if p, _ := find("acme.horizon"); !strings.HasPrefix(p.Icon, "data:image/svg+xml;base64,") {
		t.Errorf("icon: %q", p.Icon)
	}
	os.WriteFile(filepath.Join(dir, IconFile), []byte("GIF89a not an svg"), 0o644)
	if p, _ := find("acme.horizon"); p.Icon != "" {
		t.Errorf("a file that isn't an SVG: %q", p.Icon)
	}

	// The marketplace shows an inline SVG, never a remote URL.
	for icon, want := range map[string]bool{"data:image/svg+xml;base64,PHN2Zy8+": true, "https://tracker.example/icon.svg": false} {
		m.plugins[0].Icon = icon
		if got := listing(t, s).Icon != ""; got != want {
			t.Errorf("listing icon %q shown = %v", icon, got)
		}
	}
}

// A version revoked while Larvyn runs can't be turned on or installed from
// a folder before the next check marks it.
func TestRevokedRefusedFromEverySource(t *testing.T) {
	m, srv := newMarket(t)
	m.publish(t, srv, "1.0.0")
	s := &PluginService{}
	t.Cleanup(func() { applyRevoked(nil) })
	dir := filepath.Join(t.TempDir(), "acme.horizon")
	writePlugin(t, dir, validManifest) // 1.0.0

	m.revoked = []Revocation{{ID: "acme.horizon", Versions: []string{"1.0.0"}, Reason: "bad"}}
	CheckRevoked() // nothing installed yet: only the list is kept
	if _, err := s.Install(dir, horizonPerms); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Errorf("install a revoked version from a folder: %v", err)
	}

	applyRevoked(nil)
	if _, err := s.Install(dir, horizonPerms); err != nil {
		t.Fatal(err)
	}
	s.Disable("acme.horizon")
	revokedMu.Lock()
	revoked = []Revocation{{ID: "acme.horizon", Reason: "bad"}} // a list that hasn't been applied yet
	revokedMu.Unlock()
	if _, err := s.Enable("acme.horizon", horizonPerms); err == nil {
		t.Error("Enable must check the newest list, not only the recorded reason")
	}

	// Offline, with no list: nothing is refused.
	applyRevoked(nil)
	IndexURL = "http://127.0.0.1:1/plugins.json"
	CheckRevoked()
	if _, err := s.Enable("acme.horizon", horizonPerms); err != nil {
		t.Errorf("offline without a list: %v", err)
	}
}

func TestRevocationWithoutReason(t *testing.T) {
	list := []Revocation{{ID: "acme.horizon", Reason: "  "}}
	if why := reasonIn(list, "acme.horizon", "1.0.0"); why == "" {
		t.Error("a revocation without a reason must still revoke")
	}
	if why := reasonIn([]Revocation{{ID: "acme.horizon", Reason: "bad"}}, "acme.horizon", "1.0.0"); why != "bad" {
		t.Errorf("reason = %q", why)
	}
	if why := reasonIn(list, "acme.other", "1.0.0"); why != "" {
		t.Errorf("no match: %q", why)
	}
}

// Offline, a reinstall of the version recorded as revoked stays refused;
// another version installs.
func TestReinstallOfRecordedRevocation(t *testing.T) {
	reset(t)
	applyRevoked(nil)
	s := &PluginService{}
	dir := filepath.Join(t.TempDir(), "acme.horizon")
	writePlugin(t, dir, validManifest) // 1.0.0
	if _, err := s.Install(dir, horizonPerms); err != nil {
		t.Fatal(err)
	}
	store.Update(func(d *store.State) {
		st := d.Plugins["acme.horizon"]
		st.Enabled, st.Revoked = false, "bad"
		d.Plugins["acme.horizon"] = st
	})
	if _, err := s.Install(dir, horizonPerms); err == nil {
		t.Error("the same revoked version must not install again")
	}
	if p, _ := find("acme.horizon"); p.Enabled || p.Revoked != "bad" {
		t.Errorf("the revocation must stay: %+v", p)
	}
	writePlugin(t, dir, edit(t, func(m map[string]any) { m["version"] = "1.0.1" }))
	if p, err := s.Install(dir, horizonPerms); err != nil || !p.Enabled || p.Revoked != "" {
		t.Errorf("a fixed version installs: %+v, %v", p, err)
	}
}

func TestBadIndexAndDownloadLinks(t *testing.T) {
	m, srv := newMarket(t)
	m.publish(t, srv, "1.0.0")
	s := &PluginService{}
	m.plugins[0].Download = "http://bad host/x.zip"
	if _, err := s.InstallListing("acme.horizon", "1.0.0", horizonPerms); err == nil || !strings.Contains(err.Error(), "bad download link") {
		t.Errorf("malformed download link: %v", err)
	}
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"apiVersion":1,"plugins":[],"pad":"`))
		w.Write([]byte(strings.Repeat("x", 8<<20)))
		w.Write([]byte(`"}`))
	}))
	defer big.Close()
	IndexURL = big.URL
	if _, err := s.Marketplace(); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("an index over 8 MB: %v", err)
	}
}

func TestOfficial(t *testing.T) {
	for link, want := range map[string]bool{
		"https://github.com/larvyn/plugin-examples/releases/download/larvyn.hello-v0.1.1/larvyn.hello-0.1.1.zip": true,
		"https://github.com/larvynx/evil/releases/download/v1/x.zip":                                             false,
		"https://github.com/someone/larvyn/releases/download/v1/x.zip":                                           false,
		"http://github.com/larvyn/plugin-examples/x.zip":                                                         false,
		"https://github.com.evil.example/larvyn/x.zip":                                                           false,
		"https://user@github.com/larvyn/x.zip":                                                                   false,
	} {
		if got := official(link); got != want {
			t.Errorf("official(%q) = %v", link, got)
		}
	}
	m, srv := newMarket(t)
	m.publish(t, srv, "1.0.0") // served from 127.0.0.1
	if l := listing(t, &PluginService{}); l.Official {
		t.Error("a plugin downloaded from elsewhere is not official")
	}
}
