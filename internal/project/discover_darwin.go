//go:build darwin

package project

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// macProtected are the home folders macOS guards behind a privacy prompt
// ("Larvyn would like to access your Documents folder", your Photo Library,
// Apple Music…). Opening the project picker must not raise them.
var macProtected = []string{"Desktop", "Documents", "Downloads", "Library", "Music", "Movies", "Pictures", "Public"}

// guardedDirs are the folders Discover must not walk into on macOS: the
// protected home folders and every mounted volume (network shares ask for
// their own permission). A folder that already holds one of the user's
// projects is walked anyway: access to it was granted when it was added.
func guardedDirs(home string, added []string) map[string]bool {
	guarded := map[string]bool{}
	allowed := func(dir string) bool {
		d := strings.ToLower(filepath.Clean(dir))
		for _, p := range added {
			p = strings.ToLower(filepath.Clean(p))
			if p == d || strings.HasPrefix(p, d+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	if home != "" {
		for _, name := range macProtected {
			if dir := filepath.Join(home, name); !allowed(dir) {
				guarded[dir] = true
			}
		}
	}
	// The mount table lists every volume without touching any of them.
	if n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT); err == nil && n > 0 {
		buf := make([]unix.Statfs_t, n)
		if n, err = unix.Getfsstat(buf, unix.MNT_NOWAIT); err == nil {
			for _, fs := range buf[:n] {
				dir := unix.ByteSliceToString(fs.Mntonname[:])
				if dir == "" || dir == "/" || dir == "/System/Volumes/Data" || allowed(dir) {
					continue
				}
				guarded[dir] = true
			}
		}
	}
	return guarded
}
