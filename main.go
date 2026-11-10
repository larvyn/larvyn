// Package main only starts Larvyn: it connects the services through their
// hooks (internal/wiring; services don't import each other, except terminal,
// which they use to open terminals), registers them with Wails, starts the
// agents' API (internal/agentapi) and opens the window.
package main

import (
	"embed"
	"log"
	"os"

	"larvyn/internal/agentapi"
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
	"larvyn/internal/wiring"

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

func main() {
	// The services' hooks, before anything else runs.
	wiring.Connect()
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

	stopAPI := agentapi.Start(version)
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
