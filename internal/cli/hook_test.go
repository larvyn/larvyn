package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"larvyn/internal/api"
)

func hook(session string) string {
	var out bytes.Buffer
	if code := runHook(api.Caller{Session: session}, &out); code != 0 {
		return "exit code"
	}
	return out.String()
}

func TestHookTellsTheAgentWhereItRuns(t *testing.T) {
	var got struct {
		Out struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(hook("t1")), &got); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"terminal t1", "/w/wt/fix, a git worktree of the project shop", "branch larvyn/fix", "https://fix.shop.test", "dev stack: vite.", "larvyn help"} {
		if !strings.Contains(got.Out.Context, want) {
			t.Errorf("missing %q in:\n%s", want, got.Out.Context)
		}
	}
	if got.Out.Event != "SessionStart" {
		t.Errorf("event %q", got.Out.Event)
	}
}

func TestHookStaysQuietOutsideLarvyn(t *testing.T) {
	// Not in a Larvyn terminal, or one the app doesn't know: nothing, and
	// exit 0, so the agent's session starts as usual.
	for _, s := range []string{"", "gone"} {
		if out := hook(s); out != "" {
			t.Errorf("session %q: %q", s, out)
		}
	}
}

func TestStateHookSetsSessionStateQuietly(t *testing.T) {
	var out bytes.Buffer
	// Outside a Larvyn terminal: silent and exit 0.
	t.Setenv("LARVYN_SESSION", "")
	if code := Run([]string{"agents", "hook", "done"}, &out, &out); code != 0 || out.Len() != 0 {
		t.Fatalf("outside Larvyn: code=%d out=%q", code, out.String())
	}
	// Inside a session: calls agents.state and stays silent.
	t.Setenv("LARVYN_SESSION", "t1")
	if code := Run([]string{"agents", "hook", "blocked"}, &out, &out); code != 0 || out.Len() != 0 {
		t.Fatalf("blocked hook: code=%d out=%q", code, out.String())
	}
	if lastState != "t1:blocked" {
		t.Fatalf("lastState = %q, want t1:blocked", lastState)
	}
}
