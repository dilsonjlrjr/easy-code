// Command easycode-desktop is the local client of Easy Code: a read-only desktop app (Wails 3, Go plus the
// system WebView) to follow waves live and receive native notifications (US-45, US-46, ADR-20). Every
// action opens the web screen in the browser.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"easycode-desktop/internal/app"
	"easycode-desktop/internal/config"
	"easycode-desktop/ui"
)

// version is set at build time (-ldflags "-X main.version=..."), sent in User-Agent and compared with the
// server version (cliente-local.md section 6).
var version = "dev"

// The product icon everywhere the system shows one (generated from project/brand, `make -C project icons`).
var (
	//go:embed build/appicon.png
	appIcon []byte
	//go:embed build/linux/icons/hicolor/256x256/apps/easycode-desktop.png
	notificationIcon []byte
	//go:embed build/tray.png
	trayIcon []byte
	//go:embed build/tray-template.png
	trayTemplateIcon []byte
)

// appID names the app for the system: macOS bundle, GTK/Wayland app id (matches the Linux desktop entry
// br.easycode.desktop.desktop) and single instance.
const appID = "br.easycode.desktop"

func main() {
	if len(os.Args) > 1 {
		// The app has no command line (no control commands and no --json mode, PD-07).
		fmt.Fprintln(os.Stderr, "easycode-desktop: this app has no command line options")
		os.Exit(2)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	dir, err := config.DefaultDir()
	if err != nil {
		logger.Error("no user configuration folder", "error", err)
		os.Exit(1)
	}
	prefs := config.NewStore(dir, nil)
	service, host := app.New(app.Deps{Version: version, Prefs: prefs, Logger: logger})
	notifier := notifications.New()

	wails := application.New(application.Options{
		Name:        "Easy Code",
		Description: "Easy Code — acompanhamento de ondas e notificações",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(notifier),
			application.NewService(service),
			application.NewService(&lifecycle{host: host}),
		},
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(ui.Assets())},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: appID,
		},
		Mac:   application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		Linux: application.LinuxOptions{ApplicationID: appID, ProgramName: "easycode-desktop"},
	})

	window := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:  "main",
		Title: "Easy Code",
		URL:   "/",
		// RNF-19: usable from 800×600.
		Width: 1024, Height: 720, MinWidth: 800, MinHeight: 600,
	})
	// Closing the window keeps the app in the tray, so notifications keep coming (US-46 CA-3).
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})

	labels := trayLabels(loadLocale(prefs))
	menu := application.NewMenu()
	menu.Add(labels.open).OnClick(func(*application.Context) { window.Show(); window.Focus() })
	menu.AddSeparator()
	menu.Add(labels.quit).OnClick(func(*application.Context) { wails.Quit() })
	tray := wails.SystemTray.New()
	// macOS tints the template icon for the menu bar; Linux and Windows show the colored one on any bar.
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayTemplateIcon)
	} else {
		tray.SetIcon(trayIcon).SetDarkModeIcon(trayIcon)
	}
	tray.SetMenu(menu)
	tray.SetTooltip("Easy Code")
	tray.OnClick(func() { window.Show(); window.Focus() })

	host.SetPlatform(&platform{app: wails, notifier: notifier, window: window, icon: notificationIconPath(logger)})
	notifier.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error != nil {
			logger.Warn("notification response unreadable", "error", result.Error)
			return
		}
		host.NotificationClicked(result.Response.ID)
	})

	if err := wails.Run(); err != nil {
		logger.Error("app stopped", "error", err)
		os.Exit(1)
	}
}

// lifecycle starts and stops the service with Wails; its only methods are the service hooks, which are not
// bound to the interface.
type lifecycle struct{ host *app.Host }

func (l *lifecycle) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	return l.host.Startup(ctx)
}

func (l *lifecycle) ServiceShutdown() error {
	l.host.Shutdown()
	return nil
}

// platform adapts Wails to app.Platform.
type platform struct {
	app      *application.App
	notifier *notifications.NotificationService
	window   *application.WebviewWindow
	// icon is the PNG file of the product icon for the notifications on Linux and Windows; empty if it
	// could not be written.
	icon string
}

func (p *platform) Emit(name string, data any) { p.app.Event.Emit(name, data) }

func (p *platform) OpenURL(address string) error { return p.app.Browser.OpenURL(address) }

func (p *platform) Notify(id, title, body string, silent bool) error {
	options := notifications.NotificationOptions{ID: id, Title: title, Body: body}
	options.Attachments = notificationAttachments(runtime.GOOS, p.icon)
	if silent {
		options.Sound = &notifications.NotificationSound{Silent: true}
	}
	return p.notifier.SendNotification(options)
}

func (p *platform) Show() {
	p.window.Show()
	p.window.Focus()
}

func loadLocale(prefs *config.Store) string {
	loaded, err := prefs.Load()
	if err != nil {
		return config.LocalePtBR
	}
	return loaded.Locale
}

type labels struct{ open, quit string }

// trayLabels are the tray menu texts (pt-BR by default, RNF-21).
func trayLabels(locale string) labels {
	if locale == config.LocaleEn {
		return labels{open: "Open Easy Code", quit: "Quit"}
	}
	return labels{open: "Abrir o Easy Code", quit: "Sair do app"}
}

// notificationAttachments puts the product icon on the notification where the system does not take it from
// the app: Linux sends it as the image of the notification (the image-path hint) and Windows as the app
// logo of the toast. macOS shows the icon of the app bundle by itself; an attachment there would be a
// second image.
func notificationAttachments(goos, icon string) []notifications.NotificationAttachment {
	if icon == "" {
		return nil
	}
	switch goos {
	case "linux":
		return []notifications.NotificationAttachment{{Path: icon}}
	case "windows":
		return []notifications.NotificationAttachment{{Path: icon, Type: "appLogoOverride"}}
	}
	return nil
}

// notificationIconPath writes the notification icon to the user cache folder, since the notification
// daemons read it from a file. Without it the notification still goes, with the system icon.
func notificationIconPath(logger *slog.Logger) string {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return ""
	}
	dir, err := os.UserCacheDir()
	if err == nil {
		dir = filepath.Join(dir, "easycode")
		err = os.MkdirAll(dir, 0o700)
	}
	if err != nil {
		logger.Warn("notification icon not written", "error", err)
		return ""
	}
	path := filepath.Join(dir, "notification-icon.png")
	if err := os.WriteFile(path, notificationIcon, 0o600); err != nil {
		logger.Warn("notification icon not written", "error", err)
		return ""
	}
	return path
}
