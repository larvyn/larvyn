//go:build !darwin

package project

// guardedDirs are the folders Discover must not walk into. Only macOS
// guards folders behind privacy prompts.
func guardedDirs(string, []string, bool) (map[string]bool, bool) { return nil, false }
