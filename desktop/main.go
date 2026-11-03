package main

import (
	_ "embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"sync/atomic"

	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/server"
	"github.com/uiautodev/flashroute/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed build/trayicon.png
var trayIcon []byte

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
			ActivationPolicy: application.ActivationPolicyAccessory,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "FlashRoute",
		Width:            1280,
		Height:           840,
		MinWidth:         920,
		MinHeight:        620,
		BackgroundColour: application.NewRGB(7, 11, 16),
		URL:              "/",
	})

	var quitting atomic.Bool
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if quitting.Load() {
			return
		}
		window.Hide()
		event.Cancel()
	})

	showWindow := func() {
		window.Show()
		window.Restore()
		window.Focus()
	}

	tray := app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayIcon)
	} else {
		tray.SetIcon(trayIcon)
	}
	tray.SetTooltip("FlashRoute")
	tray.OnClick(showWindow)
	tray.OnRightClick(func() {
		tray.ShowMenu()
	})
	menu := app.NewMenu()
	menu.Add("Open FlashRoute").OnClick(func(*application.Context) {
		showWindow()
	})
	menu.AddSeparator()
	menu.Add("Quit FlashRoute").OnClick(func(*application.Context) {
		quitting.Store(true)
		app.Quit()
	})
	tray.SetMenu(menu)

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		showWindow()
	})

	if runtime.GOOS == "darwin" {
		app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
			showWindow()
		})
	}

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
