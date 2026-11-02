package main

import (
	"embed"
	"log"
	"path/filepath"
	"strings"

	"larvyn/internal/git"
	"larvyn/internal/proc"
	"larvyn/internal/stack"
	"larvyn/internal/store"
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
	terminal.ResolveProgram = resolveProgram
	terminal.AgentRelaunch = agentRelaunchArgv
	terminal.ResolveResumeID = func(key, cwd string, pid int, startedAt int64, claimed map[string]bool) (string, bool) {
		if key == "codex" {
			return codexResumeID(cwd, pid, startedAt, claimed)
		}
		return "", false
	}
	stack.Inspect = func(p store.Project) stack.Facts {
		info := inspect(p)
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
			application.NewService(&ProjectService{}),
			application.NewService(terminals),
			application.NewService(&stack.StackService{}),
			application.NewService(&LaravelService{}),
			application.NewService(&git.GitService{}),
			application.NewService(&AgentService{}),
			application.NewService(&SystemService{}),
			application.NewService(&FileService{}),
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
