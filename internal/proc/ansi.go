package proc

import "regexp"

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Z0-9]|\x1b[=>]`)

// StripANSI removes terminal escape sequences (colours, cursor moves, titles).
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }
