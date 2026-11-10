//go:build darwin

package project

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// macWidenable are the guarded home folders that often hold code: the
// scan looks in them once the user asks for it (Discover(true)).
var macWidenable = []string{"Desktop", "Documents", "Downloads"}

// macProtected are the other home folders macOS guards behind a privacy
// prompt (your Photo Library, Apple Music…). Projects never live there.
var macProtected = []string{"Library", "Music", "Movies", "Pictures", "Public"}

// guardedDirs are the folders Discover must not walk into on macOS, where
// reading them raises a permission prompt: the protected home folders and
// every mounted volume (network shares ask for their own permission). A
// folder that already holds one of the user's projects is walked anyway:
// access to it was granted when it was added. everywhere lets the scan into
// Desktop, Documents and Downloads. narrowed reports whether any of those
// was left out.
func guardedDirs(home string, added []string, everywhere bool) (guarded map[string]bool, narrowed bool) {
	guarded = map[string]bool{}
	allowed := func(dir string) bool {
		for _, p := range added {
			if within(p, dir) {
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
		if !everywhere {
			for _, name := range macWidenable {
				dir := filepath.Join(home, name)
				if allowed(dir) {
					continue
				}
				guarded[dir] = true
				// Listing home is allowed and says whether the folder exists
				// without looking inside it.
				if _, err := os.Lstat(dir); err == nil {
					narrowed = true
				}
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
	return guarded, narrowed
}
