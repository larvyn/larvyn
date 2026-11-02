package main

import (
	"embed"
	"log"
	"path/filepath"
	"strings"

	"larvyn/internal/agents"
	"larvyn/internal/files"
	"larvyn/internal/git"
	"larvyn/internal/laravel"
	"larvyn/internal/proc"
	"larvyn/internal/project"
	"larvyn/internal/stack"
	"larvyn/internal/store"
	"larvyn/internal/system"
	"larvyn/internal/terminal"
	"larvyn/internal/updater"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

// appIcon is the window icon (taskbar, Alt+Tab) on Linux and Windows.
//
//go:embed build/appicon.png
var appIcon []byte

// init connects the services through hooks, so internal packages never import
// each other. Tests in this package get the same wiring.
func init() {
	// Removing a worktree stops the terminals running in it.
	git.CloseSessionsIn = func(dir string) {
		for _, s := range terminals.List() {
			if strings.EqualFold(filepath.Clean(s.Cwd), filepath.Clean(dir)) {
				_ = terminals.Close(s.ID)
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
	proc.StartShellEnvImport()
	app := application.New(application.Options{
		Name:        "Larvyn",
		Description: "The Laravel workbench: projects, services, terminals, agents and data in one window",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(&project.ProjectService{}),
			application.NewService(terminals),
			application.NewService(&stack.StackService{}),
			application.NewService(&laravel.LaravelService{}),
			application.NewService(&git.GitService{}),
			application.NewService(&agents.AgentService{}),
			application.NewService(&system.SystemService{}),
			application.NewService(&files.FileService{}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
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
