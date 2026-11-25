package main

import (
	"bytes"
	"testing"
)

// The product icon goes on the notification where the system does not take it from the app bundle.
func TestNotificationAttachmentsCarryTheProductIcon(t *testing.T) {
	if got := notificationAttachments("linux", "/c/icon.png"); len(got) != 1 || got[0].Path != "/c/icon.png" || got[0].Type != "" {
		t.Fatalf("linux: %+v", got)
	}
	if got := notificationAttachments("windows", `C:\c\icon.png`); len(got) != 1 || got[0].Type != "appLogoOverride" {
		t.Fatalf("windows: %+v", got)
	}
	if got := notificationAttachments("darwin", "/c/icon.png"); got != nil {
		t.Fatalf("macOS takes the bundle icon: %+v", got)
	}
	if got := notificationAttachments("linux", ""); got != nil {
		t.Fatalf("no file, no attachment: %+v", got)
	}
}

// The embedded icons are the generated PNGs, not empty files.
func TestIconsAreEmbedded(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n")
	for name, icon := range map[string][]byte{"app": appIcon, "notification": notificationIcon, "tray": trayIcon, "template": trayTemplateIcon} {
		if !bytes.HasPrefix(icon, png) {
			t.Fatalf("%s icon is not a PNG", name)
		}
	}
}
