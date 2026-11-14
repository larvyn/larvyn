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

func TestBellAcrossReads(t *testing.T) {
	// An OSC 7501 report cut before its BEL: that BEL ends it, it isn't a bell.
	var st bellScan
	if st.scanBell([]byte("\x1b]7501;state=do")) || st.scanBell([]byte("ne\x07")) {
		t.Error("BEL ending a split OSC counted as a bell")
	}
	// Cut between ESC and ].
	st = bellScan{}
	if st.scanBell([]byte("out\x1b")) || st.scanBell([]byte("]0;title\x07")) {
		t.Error("BEL ending an OSC split after ESC counted as a bell")
	}
	// A pending ESC with no OSC open: the BEL after it still rings.
	st = bellScan{}
	if st.scanBell([]byte("out\x1b")) || !st.scanBell([]byte("\x07")) {
		t.Error("bell after a split ESC missed")
	}
	// An OSC ended by ST leaves the next BEL a bell, even a read later.
	st = bellScan{}
	if st.scanBell([]byte("\x1b]0;title\x1b\\")) || !st.scanBell([]byte("\x07")) {
		t.Error("bell after an ST-terminated OSC missed")
	}
}
