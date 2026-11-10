//go:build !darwin

package project

// guardedDirs are the folders Discover must not walk into. Only macOS
// guards folders behind privacy prompts.
func guardedDirs(string, []string) map[string]bool { return nil }
