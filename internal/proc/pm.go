package proc

import (
	"os"
	"path/filepath"
)

// PackageManager names the JavaScript package manager a project uses, from its
// lock file: pnpm, yarn, bun, or npm when there is none of theirs.
func PackageManager(root string) string {
	for file, pm := range map[string]string{"pnpm-lock.yaml": "pnpm", "yarn.lock": "yarn", "bun.lockb": "bun", "bun.lock": "bun"} {
		if _, err := os.Stat(filepath.Join(root, file)); err == nil {
			return pm
		}
	}
	return "npm"
}
