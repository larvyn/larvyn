package agents

import (
	"larvyn/internal/agentsetup"
	"larvyn/internal/clibin"
)

// SetupAgents installs the larvyn skill, session hook and MCP server in the installed
// agents' config (Settings → Agents, larvyn agents setup), or with remove
// takes them out. With dryRun it only lists what it would change.
func (a *AgentService) SetupAgents(remove, dryRun bool) ([]agentsetup.Change, error) {
	o, err := agentsetup.Defaults(clibin.Path())
	if err != nil {
		return nil, err
	}
	if remove {
		return agentsetup.Remove(o, dryRun)
	}
	return agentsetup.Install(o, dryRun)
}
