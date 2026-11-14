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
	api.Register("agents.setup", "Install the larvyn skill, its hooks and the MCP server in the agents you have (Claude Code, Codex, Cursor, Gemini CLI, Windsurf, OpenCode), so they know they run in Larvyn; safe to run again", apiAgentSetup,
		"dry-run?: only list the changes")
	api.Register("agents.remove", "Take the larvyn skill, its hooks and the MCP server out of the agents' config again", apiAgentRemove,
		"dry-run?: only list the changes")
	api.Register("agents.skill", "Print the larvyn skill (SKILL.md), for an agent that keeps its skills somewhere else: larvyn agents skill > <skills>/larvyn/SKILL.md", apiAgentSkill)
	api.MarkReadOnly("agents.skill")
}

// SkillText is the larvyn skill, printed as it is.
type SkillText struct {
	Skill string `json:"skill"`
}

func (s SkillText) Text() string { return s.Skill }

func apiAgentSkill() SkillText { return SkillText{Skill: string(agentsetup.Skill())} }

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
