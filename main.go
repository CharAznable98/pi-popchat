package main

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"pi-popchat/internal/agent"
	"pi-popchat/internal/agent/pi"
	"pi-popchat/internal/core"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/icons/status-template.png
var statusIcon []byte

func main() {
	if display := os.Getenv("PI_POPCHAT_SCREEN_FIXTURE"); display != "" {
		runScreenFixture(display)
		return
	}
	notificationProbe := os.Getenv("PI_POPCHAT_CHECK_NOTIFICATIONS") == "1"
	clipboardProbe := os.Getenv("PI_POPCHAT_CHECK_CLIPBOARD") == "1"
	probeMode := os.Getenv("PI_POPCHAT_CHECK_DESKTOP") == "1" || notificationProbe || clipboardProbe
	var probeExit atomic.Int32
	var root string
	frontend, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	d := &Desktop{done: make(chan struct{}), mainWanted: true}
	app := application.New(application.Options{Name: "Pi Popchat", Description: "桌面 Agent 对话", Assets: application.AssetOptions{Handler: application.AssetFileServerFS(frontend)}, Services: []application.Service{application.NewService(d)}, Mac: application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false}, SingleInstance: &application.SingleInstanceOptions{UniqueID: "app.pi-popchat.desktop", OnSecondInstanceLaunch: func(_ application.SecondInstanceData) {
		if d.main != nil {
			d.showMain()
		}
	}}, ShouldQuit: d.shouldQuit, OnShutdown: func() {
		close(d.done)
		detachNotifications()
		if d.engine != nil {
			_ = d.engine.Close()
		}
		if probeMode && root != "" {
			_ = os.RemoveAll(root)
		}
	}})
	d.app = app
	root = os.Getenv("PI_POPCHAT_DATA_DIR")
	if probeMode {
		root, err = os.MkdirTemp("", "pi-popchat-native-")
		if err != nil {
			log.Fatal(err)
		}
		defer os.RemoveAll(root)
	}
	if root == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			log.Fatal(e)
		}
		root = filepath.Join(home, "Library", "Application Support", "Pi Popchat")
	}
	store, err := core.OpenStore(root)
	if err != nil {
		log.Fatal(err)
	}
	executable, _ := pi.Locate()
	var factory agent.Factory = pi.Factory{}
	if probeMode {
		factory = nativeProbeFactory{}
		executable = "probe-agent-no-external-process"
	}
	d.engine, err = core.New(store, factory, executable)
	if err != nil {
		log.Fatal(err)
	}
	d.main = app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "main", Title: "Pi Popchat", Hidden: true, Width: 1080, Height: 760, MinWidth: 720, MinHeight: 480, URL: "/?view=main"})
	d.panel = app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "panel", Title: "Pi Popchat", Width: 520, Height: 620, MinWidth: 380, MinHeight: 400, Hidden: true, AlwaysOnTop: true, HideOnEscape: false, HideOnFocusLost: false, URL: "/?view=panel", Mac: application.MacWindow{WindowClass: application.MacWindowClassPanel, PanelPreferences: application.MacPanelPreferences{FloatingPanel: true, NonActivating: true}, CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces | application.MacWindowCollectionBehaviorFullScreenAuxiliary | macWindowCanJoinAllApplications}})
	if probeMode && !notificationProbe {
		registerNativeProbeEvents(d)
	}
	// Own visibility instead of Wails' captured initial Hidden value on navigation.
	for name, w := range map[string]*application.WebviewWindow{"main": d.main, "panel": d.panel} {
		w.RegisterHook(events.Mac.WebViewDidFinishNavigation, func(e *application.WindowEvent) { e.Cancel(); d.applyVisibility(name) })
	}
	d.main.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); d.hideMain() })
	d.panel.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { e.Cancel(); d.hidePanel() })
	app.Event.OnApplicationEvent(events.Mac.ApplicationDidBecomeActive, func(_ *application.ApplicationEvent) { d.completePanelActivation() })
	app.Event.RegisterApplicationEventHook(events.Mac.ApplicationShouldHandleReopen, func(e *application.ApplicationEvent) {
		if probeMode {
			nativeProbeReopenCount.Add(1)
		}
		e.Cancel()
		select {
		case <-d.done:
			return
		default:
		}
		d.showMain()
	})
	d.engine.Changed = func() { app.Event.Emit("popchat:changed") }
	d.engine.Notify = d.notify
	appMenu := app.Menu.New()
	appMenu.AddRole(application.AppMenu)
	appMenu.AddRole(application.EditMenu)
	windowMenu := appMenu.AddSubmenu("窗口")
	windowMenu.Add("打开浮窗").OnClick(func(_ *application.Context) { d.showPanel() })
	windowMenu.Add("打开主窗口").OnClick(func(_ *application.Context) { d.showMain() })
	app.Menu.Set(appMenu)
	tray := app.SystemTray.New()
	tray.SetTemplateIcon(statusIcon)
	tray.SetTooltip("Pi Popchat")
	menu := app.Menu.New()
	menu.Add("打开主窗口").OnClick(func(_ *application.Context) { d.showMain() })
	menu.AddSeparator()
	menu.Add("退出 Pi Popchat").OnClick(func(_ *application.Context) { app.Quit() })
	tray.SetMenu(menu)
	if d.engine.CurrentID("main") == "" {
		_, _ = d.engine.NewSession("main", "")
	}
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(_ *application.ApplicationEvent) {
		if probeMode {
			if notificationProbe {
				setupNotifications(d.notificationSelected, d.setError)
			}
			go func() {
				if notificationProbe {
					probeExit.Store(int32(runNotificationProbe(d)))
				} else if clipboardProbe {
					probeExit.Store(int32(runClipboardProbe(d)))
				} else {
					probeExit.Store(int32(runDesktopProbe(d)))
				}
				d.app.Quit()
			}()
			return
		}
		if err := d.setShortcut(d.engine.Settings().Shortcut); err != nil {
			d.setError(err.Error())
		}
		setupNotifications(d.notificationSelected, d.setError)
		go func() {
			d.refreshEnvironment()
			if d.engine.Snapshot("main").Environment.Available {
				_ = d.engine.Prepare(d.engine.CurrentID("main"))
			}
		}()
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					d.engine.ReapIdle(15 * time.Minute)
				case <-d.done:
					return
				}
			}
		}()
	})
	if err = app.Run(); err != nil {
		log.Fatal(err)
	}
	if probeMode {
		os.RemoveAll(root)
		os.Exit(int(probeExit.Load()))
	}
}
