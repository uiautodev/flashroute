package main

import (
	"log"

	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/server"
	"github.com/uiautodev/flashroute/internal/store"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
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

	err = wails.Run(&options.App{
		Title:     "FlashRoute",
		Width:     1280,
		Height:    840,
		MinWidth:  920,
		MinHeight: 620,
		AssetServer: &assetserver.Options{
			Handler: srv.Handler(),
		},
		BackgroundColour: &options.RGBA{R: 7, G: 11, B: 16, A: 1},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarDefault(),
			About: &mac.AboutInfo{
				Title:   "FlashRoute",
				Message: "AI model router and chat console",
			},
		},
	})
	if err != nil {
		log.Fatalf("run desktop app: %v", err)
	}
}
