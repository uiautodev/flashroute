package main

import (
	"fmt"
	"log"
	"net"
	"net/http"

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
	port := st.DesktopPort()
	ln, err := listenDesktop(port)
	if err != nil {
		log.Fatalf("listen for gateway: %v", err)
	}
	if actual := ln.Addr().(*net.TCPAddr).Port; actual != port {
		port = actual
		if err := st.SetDesktopPort(port); err != nil {
			log.Printf("save desktop port: %v", err)
		}
	}
	gatewayEndpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	srv := server.New(st, gateway.New(st), server.Options{
		Desktop:         true,
		Version:         version,
		Commit:          commit,
		DataDir:         st.Dir(),
		GatewayEndpoint: gatewayEndpoint,
	})
	go func() {
		log.Printf("desktop gateway: %s/v1", gatewayEndpoint)
		if err := http.Serve(ln, srv.Handler()); err != nil {
			log.Printf("desktop gateway stopped: %v", err)
		}
	}()

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

func listenDesktop(port int) (net.Listener, error) {
	var lastErr error
	for offset := 0; offset < 50; offset++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port+offset))
		if err == nil {
			return ln, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
