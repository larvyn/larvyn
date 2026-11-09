// Package main only wires Larvyn together: it registers the services from
// internal/ with Wails, connects them through their hooks (services don't
// import each other, except terminal, which they use to open terminals) and
// opens the window.
package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"larvyn/internal/agents"
	"larvyn/internal/cli"
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
	"github.com/wailsapp/wails/v3/pkg/events"

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

func main() {
	// `larvyn context` and the rest: the CLI, not the app (internal/cli).
	if cli.IsCommand(os.Args[1:]) {
		os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
	}
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

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
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
			Theme: windowTheme(),
		},
		BackgroundColour: windowBackground(),
		URL:              "/",
		// Files dropped on a terminal are typed in as their paths.
		EnableFileDrop: true,
	})
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		terminal.HandleDrop(e.Context().DroppedFiles(), e.Context().DropTargetDetails())
	})

	stopAPI := startAPI()
	err := app.Run()
	stopAPI()
	if err != nil {
		log.Fatal(err)
	}
}

// windowTheme is the Windows title bar theme for the chosen app theme.
func windowTheme() application.Theme {
	switch store.CurrentSettings().Theme {
	case "light":
		return application.Light
	case "dark":
		return application.Dark
	}
	return application.SystemDefault
}

// windowBackground is what the window shows before the page paints: the
// page's own background for the chosen theme. Following the system it stays
// dark, which the page repaints at once if the system is light.
func windowBackground() application.RGBA {
	if store.CurrentSettings().Theme == "light" {
		return application.NewRGB(246, 246, 248)
	}
	return application.NewRGB(9, 9, 11)
}
