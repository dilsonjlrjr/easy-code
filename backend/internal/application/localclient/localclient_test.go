package localclient_test

import (
	"strings"
	"testing"

	"easycode/internal/application/localclient"
)

func TestDeviceKeepsPrintableTextOnly(t *testing.T) {
	if got := localclient.Device("easycode-desktop/1.0.0 (linux; amd64)\r\n\x00"); got != "easycode-desktop/1.0.0 (linux; amd64)" {
		t.Fatalf("device: %q", got)
	}
	if got := localclient.Device("  "); got != "unknown" {
		t.Fatalf("empty device: %q", got)
	}
	if got := localclient.Device(strings.Repeat("x", 500)); len(got) != 120 {
		t.Fatalf("long device: %d", len(got))
	}
}

func TestConfigurationIsBuiltFromThePublicIssuer(t *testing.T) {
	c := localclient.NewService(nil, nil, nil, nil, nil, nil, "https://sso.example/auth/realms/easycode/", "easycode-api").Configuration()
	if c.Issuer != "https://sso.example/auth/realms/easycode" || c.ClientID != "easycode-desktop" || c.Audience != "easycode-api" ||
		c.DeviceAuthorizationEndpoint != c.Issuer+"/protocol/openid-connect/auth/device" || c.RevocationEndpoint != c.Issuer+"/protocol/openid-connect/revoke" ||
		c.CodeValiditySeconds != 600 || len(c.Scopes) != 2 || c.Scopes[1] != "easycode-read" {
		t.Fatalf("configuration: %+v", c)
	}
}
