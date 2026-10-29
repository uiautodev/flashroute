package main

import (
	"log"

	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/server"
	"github.com/uiautodev/flashroute/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	st, err := store.New(store.DefaultDir())
	if err != nil {
		log.Fatalf("load store: %v", err)
	}
	srv := server.New(st, gateway.New(st), server.Options{
		Desktop: true,
		Version: version,
		Commit:  commit,
		DataDir: st.Dir(),
	})

	app := application.New(application.Options{
		Name:        "FlashRoute",
		Description: "AI model router and chat console",
		Assets: application.AssetOptions{
			Handler: srv.Handler(),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "FlashRoute",
		Width:            1280,
		Height:           840,
		MinWidth:         920,
		MinHeight:        620,
		BackgroundColour: application.NewRGB(7, 11, 16),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatalf("run desktop app: %v", err)
	}
}
