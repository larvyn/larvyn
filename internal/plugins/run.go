package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// The plugin's commands and stack items (Tier 0). Every run goes through
// allow, which checks the plugin is on and was granted terminal.run, and
// through a first-run confirmation of the exact command line, both enforced
// here rather than by the frontend.

// SessionKeyPrefix starts the terminal key of everything a plugin runs, so
// its processes can be found and stopped when the plugin is turned off.
func SessionKeyPrefix(pluginID string) string { return "plugin:" + pluginID + ":" }

// allow is the permission gate: the plugin must be on, and perm must be one
// it asks for and the user granted.
func allow(p Plugin, perm string) error {
	if !p.Enabled {
		return fmt.Errorf("plugin %s is off", p.Manifest.ID)
	}
	asked, granted := false, false
	for _, x := range p.Manifest.Permissions {
		asked = asked || x == perm
	}
	for _, x := range p.Granted {
		granted = granted || x == perm
	}
	if !asked || !granted {
		return fmt.Errorf("plugin %s may not %s", p.Manifest.ID, perm)
	}
	return nil
}

// PluginCommand is a plugin command for ⌘K.
type PluginCommand struct {
	PluginID   string `json:"pluginId"`
	PluginName string `json:"pluginName"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Run        string `json:"run"`
	// NeedsConfirm is set until the user confirmed this exact command line.
	NeedsConfirm bool `json:"needsConfirm"`
}

// Commands lists the commands of the plugins that are on.
func (s *PluginService) Commands() []PluginCommand {
	var out []PluginCommand
	for _, p := range Active() {
		if allow(p, "terminal.run") != nil {
			continue
		}
		confirmed := store.Plugins()[p.Manifest.ID].Confirmed
		for _, c := range p.Manifest.Contributes.Commands {
			out = append(out, PluginCommand{PluginID: p.Manifest.ID, PluginName: p.Manifest.Name, ID: c.ID, Title: c.Title, Run: c.Run,
				NeedsConfirm: confirmed["command:"+c.ID] != c.Run})
		}
	}
	return out
}

// RunCommand runs a plugin command in the project's folder, in a terminal.
// confirmed is the command line the user saw: it must match the plugin's
// current one if this line was never confirmed before.
func (s *PluginService) RunCommand(projectID, pluginID, commandID, confirmed string) (terminal.SessionInfo, error) {
	proj, ok := store.FindProject(projectID)
	if !ok {
		return terminal.SessionInfo{}, errors.New("project not found")
	}
	p, err := find(pluginID)
	if err != nil {
		return terminal.SessionInfo{}, err
	}
	if err := allow(p, "terminal.run"); err != nil {
		return terminal.SessionInfo{}, err
	}
	for _, c := range p.Manifest.Contributes.Commands {
		if c.ID != commandID {
			continue
		}
		if err := confirm(pluginID, "command:"+c.ID, c.Run, confirmed); err != nil {
			return terminal.SessionInfo{}, err
		}
		return terminal.Sessions.Start(terminal.SessionSpec{ProjectID: proj.ID, Kind: "run", Key: SessionKeyPrefix(pluginID) + c.ID,
			Title: c.Title, Cwd: proj.Path, Command: c.Run})
	}
	return terminal.SessionInfo{}, fmt.Errorf("plugin %s has no command %s", pluginID, commandID)
}

// confirm records that the user confirmed line for key, or checks that they
// did before: line is what the plugin runs now, confirmed what the user saw.
func confirm(pluginID, key, line, confirmed string) error {
	if store.Plugins()[pluginID].Confirmed[key] == line {
		return nil
	}
	if confirmed != line {
		return errors.New("this command changed or was never confirmed; confirm it again")
	}
	return store.Update(func(d *store.State) {
		st := d.Plugins[pluginID]
		if st.Confirmed == nil {
			st.Confirmed = map[string]string{}
		}
		st.Confirmed[key] = line
		d.Plugins[pluginID] = st
	})
}

// PluginStackItem is a plugin's stack item, as the stack service shows it.
type PluginStackItem struct {
	PluginID   string
	PluginName string
	Key        string // plugin:<id>:<key>
	ItemKey    string
	Title      string
	Argv       []string
	// Line is Argv as one command line, for the confirmation.
	Line string
	// Missing names a required program that is not installed.
	Missing string
	// NeedsConfirm is set until the user confirmed Line.
	NeedsConfirm bool
}

// StackItems returns the stack items of the plugins that are on.
func StackItems() []PluginStackItem {
	var out []PluginStackItem
	states := store.Plugins()
	for _, p := range Active() {
		if allow(p, "terminal.run") != nil {
			continue
		}
		for _, it := range p.Manifest.Contributes.Stack {
			line := commandLine(it.Argv)
			item := PluginStackItem{PluginID: p.Manifest.ID, PluginName: p.Manifest.Name, Key: SessionKeyPrefix(p.Manifest.ID) + it.Key,
				ItemKey: it.Key, Title: it.Title, Argv: it.Argv, Line: line,
				NeedsConfirm: states[p.Manifest.ID].Confirmed["stack:"+it.Key] != line}
			for _, m := range p.Missing {
				if m.Program == it.Argv[0] {
					item.Missing = m.Program
				}
			}
			out = append(out, item)
		}
	}
	return out
}

// ConfirmError is what starting an unconfirmed plugin stack item returns, so
// the frontend can ask and retry. Its message is "plugin-confirm:" + JSON.
type ConfirmError struct {
	PluginID   string `json:"pluginId"`
	PluginName string `json:"pluginName"`
	Key        string `json:"key"`
	Title      string `json:"title"`
	Line       string `json:"line"`
}

func (e ConfirmError) Error() string {
	b, _ := json.Marshal(e)
	return "plugin-confirm:" + string(b)
}

// ConfirmStackItem records that the user confirmed a plugin stack item's
// command line before its first start.
func (s *PluginService) ConfirmStackItem(pluginID, key, confirmed string) error {
	p, err := find(pluginID)
	if err != nil {
		return err
	}
	if err := allow(p, "terminal.run"); err != nil {
		return err
	}
	for _, it := range p.Manifest.Contributes.Stack {
		if it.Key == key {
			return confirm(pluginID, "stack:"+key, commandLine(it.Argv), confirmed)
		}
	}
	return fmt.Errorf("plugin %s has no stack item %s", pluginID, key)
}

// commandLine shows argv as a shell line, quoting what needs it.
func commandLine(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\"'\\$`;&|<>(){}*?!#~") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		parts[i] = a
	}
	return strings.Join(parts, " ")
}

// stopSessions ends everything a plugin runs, when it is turned off or
// uninstalled.
func stopSessions(pluginID string) {
	for _, sess := range terminal.Sessions.List() {
		if strings.HasPrefix(sess.Key, SessionKeyPrefix(pluginID)) {
			_ = terminal.Sessions.Close(sess.ID)
		}
	}
}
