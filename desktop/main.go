package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/uiautodev/flashroute/internal/gateway"
	"github.com/uiautodev/flashroute/internal/server"
	"github.com/uiautodev/flashroute/internal/store"
	"github.com/uiautodev/flashroute/internal/update"
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
	updater := update.New(update.Options{DataDir: st.Dir(), Desktop: true})
	srv := server.New(st, gateway.New(st), server.Options{
		Desktop:         true,
		Version:         version,
		Commit:          commit,
		DataDir:         st.Dir(),
		GatewayEndpoint: gatewayEndpoint,
		Updater:         updater,
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

	var languageMu sync.Mutex
	language := initialTrayLanguage()
	currentLanguage := func() string {
		languageMu.Lock()
		defer languageMu.Unlock()
		return language
	}
	setLanguage := func(value string) {
		languageMu.Lock()
		language = value
		languageMu.Unlock()
	}
	updateInProgress := atomic.Bool{}
	checkUpdates := func(lang string) {
		text := trayText(lang)
		info, err := updater.Check(context.Background(), version)
		if err != nil {
			app.Dialog.Error().SetTitle(text.checkUpdates).SetMessage(err.Error()).Show()
			return
		}
		if !info.Available {
			message := text.upToDate
			if info.Dev {
				message = text.devBuild
			}
			app.Dialog.Info().SetTitle(text.checkUpdates).SetMessage(message).Show()
			return
		}
		question := app.Dialog.Question().
			SetTitle(text.updateAvailable).
			SetMessage(fmt.Sprintf(text.updateMessage, info.Latest))
		question.AddButton(text.later).SetAsDefault()
		question.AddButton(text.install).OnClick(func() {
			if !updateInProgress.CompareAndSwap(false, true) {
				return
			}
			go func() {
				defer updateInProgress.Store(false)
				app.Dialog.Info().
					SetTitle(text.installing).
					SetMessage(fmt.Sprintf(text.installingMessage, info.Latest)).
					Show()
				if _, err := updater.Apply(context.Background(), info); err != nil {
					app.Dialog.Error().SetTitle(text.checkUpdates).SetMessage(err.Error()).Show()
					return
				}
				if err := updater.Restart(); err != nil {
					app.Dialog.Error().SetTitle(text.checkUpdates).SetMessage(err.Error()).Show()
				}
			}()
		})
		question.Show()
	}

	buildMenu := func() *application.Menu {
		text := trayText(currentLanguage())
		menu := app.NewMenu()
		menu.Add(text.open).OnClick(func(*application.Context) {
			showWindow()
		})
		menu.Add(text.checkUpdates).OnClick(func(*application.Context) {
			if updateInProgress.Load() {
				return
			}
			go checkUpdates(currentLanguage())
		})
		menu.AddSeparator()
		menu.Add(text.quit).OnClick(func(*application.Context) {
			quitting.Store(true)
			app.Quit()
		})
		return menu
	}
	tray.SetMenu(buildMenu())
	app.Event.On("flashroute:language", func(event *application.CustomEvent) {
		next, _ := event.Data.(string)
		if (next == "zh" || next == "en") && next != currentLanguage() {
			setLanguage(next)
			tray.SetMenu(buildMenu())
		}
	})

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

type trayLabels struct {
	open              string
	checkUpdates      string
	quit              string
	upToDate          string
	devBuild          string
	updateAvailable   string
	updateMessage     string
	installing        string
	installingMessage string
	later             string
	install           string
}

func initialTrayLanguage() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if strings.HasPrefix(strings.ToLower(os.Getenv(key)), "zh") {
			return "zh"
		}
	}
	return "en"
}

func trayText(language string) trayLabels {
	if language == "zh" {
		return trayLabels{
			open: "打开 FlashRoute", checkUpdates: "检查更新…", quit: "退出 FlashRoute",
			upToDate: "当前已是最新版本。", devBuild: "当前是开发版本，无法检查更新。",
			updateAvailable: "发现新版本", updateMessage: "FlashRoute %s 已发布，是否安装并重启？",
			installing: "正在安装更新", installingMessage: "正在安装 FlashRoute %s，应用会自动重启。",
			later: "稍后", install: "安装并重启",
		}
	}
	return trayLabels{
		open: "Open FlashRoute", checkUpdates: "Check for Updates…", quit: "Quit FlashRoute",
		upToDate: "FlashRoute is up to date.", devBuild: "Development builds cannot check for updates.",
		updateAvailable: "Update available", updateMessage: "FlashRoute %s is available. Install and restart now?",
		installing: "Installing update", installingMessage: "Installing FlashRoute %s. The app will restart automatically.",
		later: "Later", install: "Install and Restart",
	}
}
