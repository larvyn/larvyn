// Package main only wires Larvyn together: it registers the services from
// internal/ with Wails, connects them through their hooks (services don't
// import each other, except terminal, which they use to open terminals) and
// opens the window.
package main

import (
	"embed"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"larvyn/internal/agents"
	"larvyn/internal/files"
	"larvyn/internal/git"
	"larvyn/internal/laravel"
	"larvyn/internal/plugins"
	"larvyn/internal/proc"
	"larvyn/internal/project"
	"larvyn/internal/stack"
	"larvyn/internal/store"
	"larvyn/internal/system"
	"larvyn/internal/terminal"
	"larvyn/internal/updater"

	"github.com/wailsapp/wails/v3/pkg/application"

	// Linux: runs WebKitGTK without its sandbox where AppArmor blocks it.
	_ "larvyn/internal/platform"
)

//go:embed all:frontend/dist
var assets embed.FS

// appIcon is the window icon (taskbar, Alt+Tab) on Linux and Windows.
//
//go:embed build/appicon.png
var appIcon []byte

// init connects the services through hooks. Tests in this package get the
// same wiring.
func init() {
	// Removing a worktree stops the terminals running in it.
	git.CloseSessionsIn = func(dir string) {
		for _, s := range terminal.Sessions.List() {
			if strings.EqualFold(filepath.Clean(s.Cwd), filepath.Clean(dir)) {
				_ = terminal.Sessions.Close(s.ID)
			}
		}
	}
	terminal.AgentRelaunch = agents.RelaunchArgv
	terminal.ResolveResumeID = func(key, cwd string, pid int, startedAt int64, claimed map[string]bool) (string, bool) {
		if key == "codex" {
			return agents.CodexResumeID(cwd, pid, startedAt, claimed)
		}
		return "", false
	}
	agents.CreateWorktree = func(projectID, name, base string) (string, string, error) {
		wt, err := (&git.GitService{}).CreateWorktree(git.WorktreeRequest{ProjectID: projectID, Name: name, Base: base, CopyEnv: true})
		return wt.Path, wt.Branch, err
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
	stack.Inspect = func(p store.Project) stack.Facts {
		info := project.Inspect(p)
		f := stack.Facts{IsLaravel: info.IsLaravel, HasVendor: info.HasVendor, HasNodeModules: info.HasNodeModules, NpmScripts: info.NpmScripts}
		for _, pkg := range info.Packages {
			f.Packages = append(f.Packages, pkg.Name)
		}
		return f
	}
}

func main() {
	updater.Version = version
	updater.Cleanup()
	// Turn off plugins the marketplace revoked since the last start.
	go plugins.CheckRevoked()
	proc.StartShellEnvImport()
	app := application.New(application.Options{
		Name:        "Larvyn",
		Description: "The Laravel workbench: projects, services, terminals, agents and data in one window",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(&project.ProjectService{}),
			application.NewService(terminal.Sessions),
			application.NewService(&stack.StackService{}),
			application.NewService(&laravel.LaravelService{}),
			application.NewService(&git.GitService{}),
			application.NewService(&agents.AgentService{}),
			application.NewService(&system.SystemService{}),
			application.NewService(&files.FileService{}),
			application.NewService(&plugins.PluginService{}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
			// Plugin views are served from the plugins folder, and kept away
			// from Wails' runtime endpoint.
			Middleware: plugins.ViewMiddleware,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "Larvyn",
		Width:     1440,
		Height:    900,
		MinWidth:  960,
		MinHeight: 600,
		Frameless: true,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 44,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		Windows: application.WindowsWindow{
			Theme: application.Dark,
		},
		BackgroundColour: application.NewRGB(9, 9, 11),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
