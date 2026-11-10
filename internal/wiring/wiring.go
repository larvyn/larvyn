// Package wiring connects the services through their hooks, so they don't
// import each other. main calls Connect before the app starts, and tests
// that run services together call it in their TestMain.
package wiring

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"larvyn/internal/agents"
	"larvyn/internal/git"
	"larvyn/internal/plugins"
	"larvyn/internal/project"
	"larvyn/internal/stack"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// Connect sets every hook. It is safe to call more than once.
func Connect() {
	// Removing a worktree stops the terminals running in it.
	git.CloseSessionsIn = func(dir string) {
		for _, s := range terminal.Sessions.List() {
			if strings.EqualFold(filepath.Clean(s.Cwd), filepath.Clean(dir)) {
				_ = terminal.Sessions.Close(s.ID)
			}
		}
	}
	terminal.AgentRelaunch = agents.RelaunchArgv
	terminal.ResolveResumeID = agents.ResolveResumeID
	agents.CreateWorktree = func(projectID, name, base string) (string, string, error) {
		wt, err := (&git.GitService{}).CreateWorktree(git.WorktreeRequest{ProjectID: projectID, Name: name, Base: base, CopyEnv: true})
		return wt.Path, wt.Branch, err
	}
	agents.RemoveWorktree = func(projectID, path string) error {
		return (&git.GitService{}).RemoveWorktree(projectID, path, true)
	}
	// Plugin stack items join the stack; plugins.StackItems only lists the
	// plugins that are on and may run commands.
	stack.PluginItems = func(p store.Project) []stack.StackItem {
		var out []stack.StackItem
		for _, it := range plugins.StackItems(p.ID) {
			argv := append([]string(nil), it.Argv...)
			if argv[0] == "php" {
				argv[0] = store.PHP()
			}
			si := stack.StackItem{Key: it.Key, Label: it.Title, Description: it.PluginName + " plugin · " + it.Line,
				Argv: argv, Available: it.Missing == "", Plugin: it.PluginName}
			if it.Missing != "" {
				si.Reason = "needs " + it.Missing + ": install it from Settings → Plugins"
			}
			pluginID, projectID, key := it.PluginID, p.ID, it.ItemKey
			si.Guard = func(start func() (terminal.SessionInfo, error)) (terminal.SessionInfo, error) {
				return plugins.GuardStackStart(pluginID, projectID, key, start)
			}
			if it.NeedsConfirm {
				si.NeedConfirm = plugins.ConfirmError{PluginID: it.PluginID, PluginName: it.PluginName, Key: it.ItemKey, Title: it.Title, Line: it.Line}
			}
			out = append(out, si)
		}
		return out
	}
	// Plugin views control the stack (stack.control) through the stack
	// service, like the sidebar does.
	plugins.StackControl = func(projectID, key, action string) (terminal.SessionInfo, error) {
		s := &stack.StackService{}
		switch action {
		case "start":
			return s.Start(projectID, key)
		case "restart":
			return s.Restart(projectID, key)
		case "stop":
			return terminal.SessionInfo{}, s.Stop(projectID, key)
		}
		return terminal.SessionInfo{}, fmt.Errorf("unknown stack action %q", action)
	}
	// The plugins a project's .larvyn/project.json lists: installed, in its
	// version range, and on in the project.
	project.PluginCheck = func(id, versionRange, projectID string) string {
		for _, p := range plugins.Load() {
			if p.Manifest.ID != id {
				continue
			}
			switch {
			case versionRange != "" && !plugins.Compatible(versionRange, p.Manifest.Version):
				return fmt.Sprintf("version %s is installed; the project needs %s", p.Manifest.Version, versionRange)
			case p.Revoked != "":
				return "revoked: " + p.Revoked
			case p.Error != "":
				return p.Error
			case !p.Enabled:
				return "installed but off: turn it on in Settings → Plugins"
			case slices.Contains(p.OffIn, projectID):
				return "turned off in this project"
			}
			return ""
		}
		return "missing"
	}
	stack.Inspect = func(p store.Project) stack.Facts {
		info := project.Inspect(p)
		f := stack.Facts{IsLaravel: info.IsLaravel, HasVendor: info.HasVendor, HasNodeModules: info.HasNodeModules, NpmScripts: info.NpmScripts}
		for _, pkg := range info.Packages {
			f.Packages = append(f.Packages, pkg.Name)
		}
		return f
	}
}
