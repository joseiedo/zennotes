package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZenNotes/zennotes/apps/server/internal/config"
	"github.com/ZenNotes/zennotes/apps/server/internal/vault"
)

func newProxyAuthServer(t *testing.T, cfg config.Config) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	cfg.VaultPath = root
	cfg.DefaultVaultPath = root
	if cfg.Bind == "" {
		cfg.Bind = "192.0.2.10:7878"
	}
	v, err := vault.New(root, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(v, nil, nil, cfg).Router())
	t.Cleanup(srv.Close)
	return srv
}

func proxyReq(t *testing.T, method, url string, headers map[string]string) int {
	t.Helper()
	var body *strings.Reader
	if method == http.MethodPost {
		body = strings.NewReader(`{"folder":"inbox","subpath":"X"}`)
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestProxyAuthRejectsCrossSiteWrites(t *testing.T) {
	for _, public := range []bool{true, false} {
		cfg := config.Config{AdminProxyAuth: true, PublicSite: public, AllowedOrigins: []string{"*"}}
		base := ""
		if public {
			cfg.BasePath = "/admin"
			base = "/admin"
		}
		srv := newProxyAuthServer(t, cfg)
		url := srv.URL + base + "/api/folders/create"
		cases := []struct {
			name    string
			headers map[string]string
			want    int
		}{
			{"cross-site Origin, text/plain form", map[string]string{"Origin": "https://evil.example", "Content-Type": "text/plain"}, http.StatusForbidden},
			{"wildcard-allowed Origin still refused", map[string]string{"Origin": "https://other.example"}, http.StatusForbidden},
			{"opaque null Origin", map[string]string{"Origin": "null"}, http.StatusForbidden},
			{"Sec-Fetch-Site cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
			{"same-origin", map[string]string{"Origin": srv.URL, "Sec-Fetch-Site": "same-origin"}, http.StatusNoContent},
			{"no Origin (CLI / desktop)", nil, http.StatusNoContent},
		}
		for _, c := range cases {
			if got := proxyReq(t, http.MethodPost, url, c.headers); got != c.want {
				t.Errorf("public=%v %s: status %d, want %d", public, c.name, got, c.want)
			}
		}
		// Reads are unaffected.
		if got := proxyReq(t, http.MethodGet, srv.URL+base+"/api/capabilities", map[string]string{"Origin": "https://evil.example"}); got != http.StatusOK {
			t.Errorf("public=%v GET: %d", public, got)
		}
	}
}

func TestProxyAuthRejectsCrossSiteWebSocket(t *testing.T) {
	srv := newProxyAuthServer(t, config.Config{AdminProxyAuth: true, PublicSite: true, BasePath: "/admin", AllowedOrigins: []string{"*"}})
	ws := map[string]string{
		"Connection":            "Upgrade",
		"Upgrade":               "websocket",
		"Sec-WebSocket-Version": "13",
		"Sec-WebSocket-Key":     "dGhlIHNhbXBsZSBub25jZQ==",
	}
	evil := map[string]string{"Origin": "https://evil.example"}
	cross := map[string]string{"Origin": srv.URL, "Sec-Fetch-Site": "cross-site"}
	for name, extra := range map[string]map[string]string{"foreign origin": evil, "cross-site fetch": cross} {
		h := map[string]string{}
		for k, v := range ws {
			h[k] = v
		}
		for k, v := range extra {
			h[k] = v
		}
		if got := proxyReq(t, http.MethodGet, srv.URL+"/admin/api/watch", h); got != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, got)
		}
	}
}

// Token mode is unchanged: a bearer-token client may write from any origin.
func TestTokenModeCrossOriginWriteUnchanged(t *testing.T) {
	srv := newProxyAuthServer(t, config.Config{AuthToken: "secret-token", AllowedOrigins: []string{"*"}})
	got := proxyReq(t, http.MethodPost, srv.URL+"/api/folders/create", map[string]string{
		"Origin": "https://other.example", "Sec-Fetch-Site": "cross-site", "Authorization": "Bearer secret-token",
	})
	if got != http.StatusNoContent {
		t.Fatalf("status %d, want 204", got)
	}
}
