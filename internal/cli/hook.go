package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"larvyn/internal/api"
)

// `larvyn agents hook` is the session-start hook `larvyn agents setup`
// installs in Claude Code and Codex (internal/agentsetup). In a terminal
// Larvyn opened it tells the agent where it runs, as the hook's
// additionalContext. Anywhere else, or when the app doesn't answer, it
// prints nothing and exits 0: the hook runs in every session of the agent,
// and must never get in the way of one.
func runHook(caller api.Caller, stdout io.Writer) int {
	if caller.Session == "" {
		return 0
	}
	resp, err := api.Dial(api.Request{Method: "context", Caller: caller})
	if err != nil || resp.Error != "" {
		return 0
	}
	var ctx struct {
		Project  string          `json:"project"`
		Path     string          `json:"path"`
		Dir      string          `json:"dir"`
		Worktree bool            `json:"worktree"`
		Branch   string          `json:"branch"`
		URL      string          `json:"url"`
		Terminal string          `json:"terminal"`
		Stack    []hookStackItem `json:"stack"`
	}
	if json.Unmarshal(resp.Result, &ctx) != nil {
		return 0
	}
	out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "SessionStart",
		"additionalContext": hookContext(ctx.Project, ctx.Path, ctx.Dir, ctx.Branch, ctx.URL, ctx.Terminal, ctx.Worktree, ctx.Stack),
	}})
	fmt.Fprintln(stdout, string(out))
	return 0
}

type hookStackItem struct {
	Key     string `json:"key"`
	Running bool   `json:"running"`
}

func hookContext(project, path, dir, branch, url, terminal string, worktree bool, stack []hookStackItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This session runs in Larvyn, the Laravel workbench, in its terminal %s. ", terminal)
	if worktree {
		fmt.Fprintf(&b, "You work in %s, a git worktree of the project %s (%s)", dir, project, path)
	} else {
		fmt.Fprintf(&b, "You work in the project %s at %s", project, dir)
	}
	if branch != "" {
		fmt.Fprintf(&b, ", on branch %s", branch)
	}
	b.WriteString(".")
	if url != "" {
		fmt.Fprintf(&b, " The app is served at %s.", url)
	}
	var running []string
	for _, s := range stack {
		if s.Running {
			running = append(running, s.Key)
		}
	}
	if len(running) > 0 {
		fmt.Fprintf(&b, " Running in its dev stack: %s.", strings.Join(running, ", "))
	}
	b.WriteString("\n\nThe `larvyn` command drives Larvyn from here: `larvyn stack start vite --wait` instead of starting servers yourself, " +
		"`larvyn test` and `larvyn artisan …` in your folder, `larvyn logs errors`, `larvyn terminal read <id>`, and " +
		"`larvyn agents start \"<task>\"` / `larvyn agents wait <id>` to hand work to another agent. `larvyn help` lists every command; the larvyn skill explains them.")
	return b.String()
}
