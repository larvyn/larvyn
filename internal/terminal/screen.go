package terminal

import (
	"fmt"
	"strings"

	"github.com/hinshun/vt10x"
)

// Screen is what a terminal shows now, as plain text: its scrollback played
// into a terminal emulator of its size. Programs that draw with the cursor
// (agents' TUIs, progress bars) only make sense this way; their raw output
// without escape sequences is scrambled. Trailing spaces and blank lines
// are dropped. It is a function, not a method, so Wails does not bind it.
func Screen(id string) (text string, err error) {
	s, err := Sessions.get(id)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	buf := append([]byte(nil), s.buf...)
	cols, rows := s.spec.Cols, s.spec.Rows
	s.mu.Unlock()
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("could not draw the screen: %v", r)
		}
	}()
	vt := vt10x.New(vt10x.WithSize(cols, rows))
	_, _ = vt.Write(buf)
	lines := strings.Split(vt.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n"), nil
}
