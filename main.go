package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "Larvyn",
		Description: "The Laravel workbench: projects, services, terminals, agents and data in one window",
		Services: []application.Service{
			application.NewService(&ProjectService{}),
			application.NewService(terminals),
			application.NewService(&StackService{}),
			application.NewService(&LaravelService{}),
			application.NewService(&GitService{}),
			application.NewService(&AgentService{}),
			application.NewService(&SystemService{}),
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
