package agents

import (
	"reflect"
	"testing"
)

func builtin(t *testing.T, id string) AgentKind {
	t.Helper()
	for _, k := range agentKinds {
		if k.ID == id {
			return k
		}
	}
	t.Fatalf("no agent %q", id)
	return AgentKind{}
}

// OpenCode gets its first prompt through --prompt, so it is not lost while
// the TUI starts (issue larvyn/releases#3).
func TestOpenCodeTakesThePromptAsAFlag(t *testing.T) {
	prompt := "This error was logged in my Laravel app.\n\nRedisException: Connection refused"
	got := promptArgs(builtin(t, "opencode"), prompt)
	if want := []string{"--prompt", prompt}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPromptArgs(t *testing.T) {
	if got := promptArgs(builtin(t, "claude"), "fix it"); !reflect.DeepEqual(got, []string{"fix it"}) {
		t.Fatalf("claude: got %q", got)
	}
	if got := promptArgs(builtin(t, "aider"), "fix it"); got != nil {
		t.Fatalf("aider types the prompt in, got %q", got)
	}
	if got := promptArgs(builtin(t, "opencode"), ""); got != nil {
		t.Fatalf("no prompt, got %q", got)
	}
}
