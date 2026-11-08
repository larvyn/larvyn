package proc

import (
	"regexp"
	"strconv"
	"strings"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Z0-9]|\x1b[=>]`)

// cursorForward is CSI n C, which TUIs use to skip over cells instead of
// printing spaces.
var cursorForward = regexp.MustCompile(`\x1b\[(\d*)C`)

// StripANSI removes terminal escape sequences (colours, cursor moves, titles).
// A move forward becomes as many spaces, so words drawn apart stay apart.
func StripANSI(s string) string {
	if strings.Contains(s, "C") {
		s = cursorForward.ReplaceAllStringFunc(s, func(m string) string {
			n, err := strconv.Atoi(m[2 : len(m)-1])
			if err != nil || n < 1 {
				n = 1
			}
			return strings.Repeat(" ", min(n, 500))
		})
	}
	return ansiRe.ReplaceAllString(s, "")
}
