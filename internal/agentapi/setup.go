package agentapi

import (
	"fmt"
	"strings"

	"larvyn/internal/agents"
	"larvyn/internal/agentsetup"
	"larvyn/internal/api"
)

// Setting the agents up from the CLI, as Settings → Agents does.
func init() {
	api.Register("agents.setup", "Install the larvyn skill and session hook in Claude Code and Codex, so agents know they run in Larvyn; safe to run again", apiAgentSetup,
		"dry-run?: only list the changes")
	api.Register("agents.remove", "Take the larvyn skill and session hook out of the agents' config again", apiAgentRemove,
		"dry-run?: only list the changes")
}

// SetupResult is what larvyn agents setup and remove print.
type SetupResult struct {
	DryRun  bool                `json:"dryRun"`
	Changes []agentsetup.Change `json:"changes"`
}

func (r SetupResult) Text() string {
	if len(r.Changes) == 0 {
		return "Nothing to change.\n"
	}
	var b strings.Builder
	if r.DryRun {
		b.WriteString("Would change (dry run):\n")
	}
	for _, c := range r.Changes {
		fmt.Fprintf(&b, "%-9s %s: %s\n", c.Action, c.What, c.Path)
		if c.Note != "" {
			fmt.Fprintf(&b, "          %s\n", c.Note)
		}
	}
	return b.String()
}

func apiAgentSetup(dryRun bool) (SetupResult, error) {
	cs, err := (&agents.AgentService{}).SetupAgents(false, dryRun)
	return SetupResult{DryRun: dryRun, Changes: cs}, err
}

func apiAgentRemove(dryRun bool) (SetupResult, error) {
	cs, err := (&agents.AgentService{}).SetupAgents(true, dryRun)
	return SetupResult{DryRun: dryRun, Changes: cs}, err
}
