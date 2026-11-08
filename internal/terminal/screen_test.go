package terminal

import "testing"

func TestScreenPlaysTheOutputLikeATerminal(t *testing.T) {
	// A TUI redrawing itself: a spinner line overwritten in place, words
	// placed with cursor moves, and a status bar written at the bottom.
	out := "> fix the login\r\n" +
		"✻ Thinking…\r\n" +
		"\x1b[1A\x1b[2K" + "● Done:\x1b[1Cfixed\x1b[1Cthe\x1b[1Credirect\r\n" +
		"\x1b[6;1H" + "? for shortcuts"
	s := &session{info: SessionInfo{ID: "screen-test"}, spec: SessionSpec{Cols: 40, Rows: 6}, buf: []byte(out)}
	Sessions.add(s)
	defer Sessions.Close(s.info.ID)
	got, err := Screen(s.info.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "> fix the login\n● Done: fixed the redirect\n\n\n\n? for shortcuts"
	if got != want {
		t.Errorf("screen:\n%q\nwant:\n%q", got, want)
	}
	if _, err := Screen("nope"); err == nil {
		t.Error("an unknown terminal has a screen")
	}
}
