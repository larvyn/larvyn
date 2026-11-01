package terminal

import "testing"

func TestContainsBell(t *testing.T) {
	if containsBell([]byte("\x1b]0;title\x07")) {
		t.Error("OSC terminator counted as a bell")
	}
	if !containsBell([]byte("done\x07")) {
		t.Error("plain bell missed")
	}
}
