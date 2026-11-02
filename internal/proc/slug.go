package proc

import (
	"regexp"
	"strings"
	"time"
)

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a title into a branch or folder name: lowercase, dashes.
func Slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "task-" + time.Now().Format("0102-1504")
	}
	return s
}
