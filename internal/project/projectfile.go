package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"larvyn/internal/projectfile"
	"larvyn/internal/store"
	"larvyn/internal/terminal"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The project's own settings: its commands, local (only this user, in the
// state folder) and shared (the team, in .larvyn/project.json), and what the
// shared file asks for.

// ProjectCommand is a command for ⌘K and the project actions.
type ProjectCommand struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Run   string `json:"run"`
	// Shared is set for a command from .larvyn/project.json, unset for the
	// user's own.
	Shared bool `json:"shared"`
}

// PluginNeed is a plugin the project file lists.
type PluginNeed struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	// Problem is "" when the plugin is installed, matches and is on, or
	// says what is wrong: "missing", or a sentence.
	Problem string `json:"problem"`
}

// ProjectFileInfo is .larvyn/project.json as Larvyn loaded it.
type ProjectFileInfo struct {
	// Path is where the file is, or would be.
	Path     string                  `json:"path"`
	Exists   bool                    `json:"exists"`
	Error    string                  `json:"error"`
	Warnings []string                `json:"warnings"`
	Commands []projectfile.Command   `json:"commands"`
	Stack    []projectfile.StackItem `json:"stack"`
	Plugins  []PluginNeed            `json:"plugins"`
	Setup    []string                `json:"setup"`
}

// commandsMu makes each change to the commands (save, delete, move) happen
// whole, from reading them to the last write or its undo: two moves of the
// same command at once could otherwise each remove the other's copy.
var commandsMu sync.Mutex

// PluginCheck tells whether a plugin the project needs is ready: "" when it
// is, "missing" when it is not installed, otherwise what is wrong. main sets
// it from the plugin host, so this package does not depend on it.
var PluginCheck = func(id, versionRange, projectID string) string { return "missing" }

func init() {
	application.RegisterEvent[string]("projectfile:changed")
}

// File reads the project's .larvyn/project.json.
func (s *ProjectService) File(id string) (ProjectFileInfo, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return ProjectFileInfo{}, errors.New("project not found")
	}
	l := projectfile.Load(p.Path)
	info := ProjectFileInfo{Path: l.Path, Exists: l.Exists, Error: l.Error, Warnings: l.Warnings,
		Commands: l.File.Commands, Stack: l.File.Stack, Setup: l.File.Worktree.Setup}
	for _, pl := range l.File.Plugins {
		info.Plugins = append(info.Plugins, PluginNeed{ID: pl.ID, Version: pl.Version, Problem: PluginCheck(pl.ID, pl.Version, id)})
	}
	return info, nil
}

// Commands lists the project's commands: the shared ones, then the user's
// own. A local command with the id of a shared one replaces it, so anyone
// can tweak a team command for themselves.
func (s *ProjectService) Commands(id string) ([]ProjectCommand, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return nil, errors.New("project not found")
	}
	return commandsOf(p), nil
}

func commandsOf(p store.Project) []ProjectCommand {
	local := map[string]bool{}
	for _, c := range p.Commands {
		local[c.ID] = true
	}
	out := []ProjectCommand{}
	for _, c := range projectfile.Load(p.Path).File.Commands {
		if !local[c.ID] {
			out = append(out, ProjectCommand{ID: c.ID, Title: c.Title, Run: c.Run, Shared: true})
		}
	}
	for _, c := range p.Commands {
		out = append(out, ProjectCommand{ID: c.ID, Title: c.Title, Run: c.Run})
	}
	return out
}

// SaveCommand adds a command, or changes the one with its id, in the
// project file when Shared is set and in the user's state otherwise. An
// empty id is made from the title.
func (s *ProjectService) SaveCommand(id string, c ProjectCommand) (ProjectCommand, error) {
	commandsMu.Lock()
	defer commandsMu.Unlock()
	p, ok := store.FindProject(id)
	if !ok {
		return c, errors.New("project not found")
	}
	c.Title, c.Run = strings.TrimSpace(c.Title), strings.TrimSpace(c.Run)
	if c.ID == "" {
		c.ID = freeID(slug(c.Title), p)
	}
	cmd := projectfile.Command{ID: c.ID, Title: c.Title, Run: c.Run}
	if err := projectfile.CheckCommand(cmd); err != nil {
		return c, err
	}
	var err error
	if c.Shared {
		err = projectfile.EditCommands(p.Path, func(list []projectfile.Command) ([]projectfile.Command, error) {
			return upsert(list, cmd), nil
		})
	} else {
		err = editLocal(id, func(list []store.Command) []store.Command {
			out := append([]store.Command(nil), list...)
			for i := range out {
				if out[i].ID == c.ID {
					out[i] = store.Command(cmd)
					return out
				}
			}
			return append(out, store.Command(cmd))
		})
	}
	if err == nil {
		emit("projectfile:changed", id)
	}
	return c, err
}

// DeleteCommand removes a command from the project file (shared) or the
// user's state.
func (s *ProjectService) DeleteCommand(id, commandID string, shared bool) error {
	commandsMu.Lock()
	defer commandsMu.Unlock()
	p, ok := store.FindProject(id)
	if !ok {
		return errors.New("project not found")
	}
	var err error
	if shared {
		err = projectfile.EditCommands(p.Path, func(list []projectfile.Command) ([]projectfile.Command, error) {
			return remove(list, commandID), nil
		})
	} else {
		err = editLocal(id, func(list []store.Command) []store.Command {
			var out []store.Command
			for _, c := range list {
				if c.ID != commandID {
					out = append(out, c)
				}
			}
			return out
		})
	}
	if err == nil {
		emit("projectfile:changed", id)
	}
	return err
}

// MoveCommand moves a command between the user's state (toShared false)
// and the project file (toShared true), replacing one with the same id
// there. It adds the command to the other side first and removes it from
// its side after; if that fails, the other side is put back as it was, so a
// failed move leaves things as they were.
func (s *ProjectService) MoveCommand(id, commandID string, toShared bool) error {
	commandsMu.Lock()
	defer commandsMu.Unlock()
	p, ok := store.FindProject(id)
	if !ok {
		return errors.New("project not found")
	}
	shared := projectfile.Load(p.Path).File.Commands
	var cmd *projectfile.Command
	if toShared {
		for _, c := range p.Commands {
			if c.ID == commandID {
				c := projectfile.Command(c)
				cmd = &c
			}
		}
	} else {
		for _, c := range shared {
			if c.ID == commandID {
				c := c
				cmd = &c
			}
		}
	}
	if cmd == nil {
		return fmt.Errorf("no command %s to move", commandID)
	}
	defer emit("projectfile:changed", id)
	withoutLocal := func(list []store.Command) []store.Command {
		var out []store.Command
		for _, c := range list {
			if c.ID != commandID {
				out = append(out, c)
			}
		}
		return out
	}

	if toShared {
		// The shared command it replaces, if any, to put back on failure.
		var replaced *projectfile.Command
		for _, c := range shared {
			if c.ID == commandID {
				c := c
				replaced = &c
			}
		}
		if err := projectfile.EditCommands(p.Path, func(list []projectfile.Command) ([]projectfile.Command, error) {
			return upsert(list, *cmd), nil
		}); err != nil {
			return err
		}
		if err := editLocal(id, withoutLocal); err != nil {
			undo := projectfile.EditCommands(p.Path, func(list []projectfile.Command) ([]projectfile.Command, error) {
				if replaced != nil {
					return upsert(list, *replaced), nil
				}
				return remove(list, commandID), nil
			})
			return errors.Join(err, wrapUndo(undo))
		}
		return nil
	}

	before := append([]store.Command(nil), p.Commands...)
	if err := editLocal(id, func(list []store.Command) []store.Command {
		return append(withoutLocal(list), store.Command(*cmd))
	}); err != nil {
		return err
	}
	if err := projectfile.EditCommands(p.Path, func(list []projectfile.Command) ([]projectfile.Command, error) {
		return remove(list, commandID), nil
	}); err != nil {
		undo := editLocal(id, func([]store.Command) []store.Command { return before })
		return errors.Join(err, wrapUndo(undo))
	}
	return nil
}

// wrapUndo says that putting things back after a failed move failed too.
func wrapUndo(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("and it could not be undone: %w", err)
}

// RunCommand runs one of the project's commands in a terminal in its
// folder. Shared commands run like the rest of the project's code (composer
// scripts, artisan): adding a project to Larvyn means trusting it.
func (s *ProjectService) RunCommand(id, commandID string) (terminal.SessionInfo, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return terminal.SessionInfo{}, errors.New("project not found")
	}
	for _, c := range commandsOf(p) {
		if c.ID == commandID {
			return terminal.Sessions.Start(terminal.SessionSpec{ProjectID: p.ID, Kind: "run", Key: "command:" + c.ID,
				Title: c.Title, Cwd: p.Path, Command: c.Run})
		}
	}
	return terminal.SessionInfo{}, fmt.Errorf("no command %s", commandID)
}

func editLocal(id string, fn func([]store.Command) []store.Command) error {
	return store.Update(func(d *store.State) {
		for i := range d.Projects {
			if d.Projects[i].ID == id {
				d.Projects[i].Commands = fn(d.Projects[i].Commands)
			}
		}
	})
}

func upsert(list []projectfile.Command, c projectfile.Command) []projectfile.Command {
	for i := range list {
		if list[i].ID == c.ID {
			list[i] = c
			return list
		}
	}
	return append(list, c)
}

func remove(list []projectfile.Command, id string) []projectfile.Command {
	var out []projectfile.Command
	for _, c := range list {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return out
}

// slug makes a command id from its title: "Fresh DB + seed" → "fresh-db-seed".
func slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimSuffix(b.String(), "-")
	if len(s) > 48 {
		s = strings.TrimSuffix(s[:48], "-")
	}
	if s == "" {
		s = "command"
	}
	return s
}

// freeID is base, or base-2, base-3… when a command already has it.
func freeID(base string, p store.Project) string {
	taken := map[string]bool{}
	for _, c := range commandsOf(p) {
		taken[c.ID] = true
	}
	id := base
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

var watchOnce sync.Once

// ServiceStartup watches the projects' files, so a git pull or a branch
// switch shows up without a reload.
func (s *ProjectService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	watchOnce.Do(func() { go watchFiles(ctx) })
	return nil
}

func watchFiles(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		for _, p := range store.Projects() {
			if projectfile.Changed(p.Path) {
				emit("projectfile:changed", p.ID)
			}
		}
	}
}
