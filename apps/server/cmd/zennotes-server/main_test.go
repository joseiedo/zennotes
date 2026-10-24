package main

import (
	"strings"
	"testing"

	"github.com/ZenNotes/zennotes/apps/server/internal/config"
)

func TestBindIsLoopbackTreatsEmptyHostAsNonLoopback(t *testing.T) {
	cases := []struct {
		bind string
		want bool
	}{
		{":7878", false},
		{"0.0.0.0:7878", false},
		{"[::]:7878", false},
		{"127.0.0.1:7878", true},
		{"[::1]:7878", true},
		{"localhost:7878", true},
	}

	for _, tc := range cases {
		if got := bindIsLoopback(tc.bind); got != tc.want {
			t.Fatalf("bindIsLoopback(%q) = %v, want %v", tc.bind, got, tc.want)
		}
	}
}

func TestStartupAuthError(t *testing.T) {
	const remote, local = "0.0.0.0:7878", "127.0.0.1:7878"
	cases := []struct {
		name    string
		cfg     config.Config
		wantErr string
	}{
		{"proxy auth public non-loopback no token", config.Config{PublicSite: true, AdminProxyAuth: true, BasePath: "/admin", Bind: remote}, ""},
		{"no flags non-loopback no token", config.Config{Bind: remote}, "ZENNOTES_AUTH_TOKEN"},
		{"no flags loopback no token", config.Config{Bind: local}, ""},
		{"insecure noauth non-loopback", config.Config{AllowInsecureNoAuth: true, Bind: remote}, ""},
		{"public no token no proxy flag", config.Config{PublicSite: true, BasePath: "/admin", Bind: local}, "ZENNOTES_ADMIN_PROXY_AUTH"},
		{"proxy flag without public site still refused", config.Config{AdminProxyAuth: true, Bind: remote}, "ZENNOTES_AUTH_TOKEN"},
		{"token ok", config.Config{AuthToken: "x", Bind: remote}, ""},
	}
	for _, tc := range cases {
		err := startupAuthError(tc.cfg)
		if tc.wantErr == "" && err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
		if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s: error %v, want containing %q", tc.name, err, tc.wantErr)
		}
	}
}
