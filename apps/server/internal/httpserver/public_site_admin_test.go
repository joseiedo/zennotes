package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ZenNotes/zennotes/apps/server/internal/config"
	"github.com/ZenNotes/zennotes/apps/server/internal/publicsite"
	"github.com/ZenNotes/zennotes/apps/server/internal/vault"
)

func newPublicSiteAdmin(t *testing.T, on bool) (*Server, *httptest.Server, http.CookieJar) {
	t.Helper()
	cfg := config.Config{
		VaultPath:  t.TempDir(),
		Bind:       "127.0.0.1:0",
		AuthToken:  "secret-token",
		PublicSite: on,
	}
	v, err := vault.New(cfg.VaultPath, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := New(v, nil, nil, cfg)
	if on {
		s.publicRules = publicsite.NewStore(filepath.Join(t.TempDir(), "public-site.json"))
	}
	srv := httptest.NewServer(s.Router())
	t.Cleanup(srv.Close)
	return s, srv, loginAndJar(t, srv, "secret-token")
}

func psDo(t *testing.T, srv *httptest.Server, jar http.CookieJar, method string, body any) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, srv.URL+"/api/public-site", rdr)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	if jar != nil {
		client.Jar = jar
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestPublicSiteAdminRequiresAuth(t *testing.T) {
	_, srv, _ := newPublicSiteAdmin(t, true)
	if code, _ := psDo(t, srv, nil, "GET", nil); code != http.StatusUnauthorized {
		t.Fatalf("GET no cookie: %d", code)
	}
	if code, _ := psDo(t, srv, nil, "PUT", map[string]any{"rules": map[string]string{}}); code != http.StatusUnauthorized {
		t.Fatalf("PUT no cookie: %d", code)
	}
}

func TestPublicSiteAdminRoundTrip(t *testing.T) {
	s, srv, jar := newPublicSiteAdmin(t, true)
	if _, err := s.currentVault().CreateNote(vault.FolderInbox, "Hello", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.currentVault().WriteNote("inbox/Hello.md", "hi"); err != nil {
		t.Fatal(err)
	}
	put := map[string]any{
		"homeNote": "inbox/Hello.md",
		"rules":    map[string]string{"Gone": "public", "other": "hidden"},
	}
	code, putOut := psDo(t, srv, jar, "PUT", put)
	if code != 200 {
		t.Fatalf("PUT: %d %v", code, putOut)
	}
	code, got := psDo(t, srv, jar, "GET", nil)
	if code != 200 || !reflect.DeepEqual(putOut, got) {
		t.Fatalf("GET differs: %d %v vs %v", code, got, putOut)
	}
	if got["homeNote"] != "inbox/Hello.md" {
		t.Fatalf("homeNote: %v", got["homeNote"])
	}
	if got["homeNotePublic"] != false {
		t.Fatalf("homeNotePublic should be false: %v", got["homeNotePublic"])
	}
	missing, _ := got["missingPaths"].([]any)
	if len(missing) != 2 {
		t.Fatalf("missingPaths: %v", missing)
	}
	foundGone := false
	for _, m := range missing {
		if m == "Gone" {
			foundGone = true
		}
	}
	if !foundGone {
		t.Fatalf("Gone not in missingPaths: %v", missing)
	}

	// Making the home note's folder public flips homeNotePublic and clears it
	// from missing (directory prefix).
	put["rules"] = map[string]string{"inbox": "public"}
	_, got = psDo(t, srv, jar, "PUT", put)
	if got["homeNotePublic"] != true {
		t.Fatalf("homeNotePublic should be true: %v", got)
	}
	if m, _ := got["missingPaths"].([]any); len(m) != 0 {
		t.Fatalf("missingPaths: %v", m)
	}
}

func TestPublicSiteAdminRejectsBadKey(t *testing.T) {
	_, srv, jar := newPublicSiteAdmin(t, true)
	code, _ := psDo(t, srv, jar, "PUT", map[string]any{"rules": map[string]string{"../x": "public"}})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", code)
	}
}

func TestCapabilitiesReportPublicSite(t *testing.T) {
	for _, on := range []bool{true, false} {
		_, srv, _ := newPublicSiteAdmin(t, on)
		resp, err := http.Get(srv.URL + "/api/capabilities")
		if err != nil {
			t.Fatal(err)
		}
		var caps map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&caps)
		resp.Body.Close()
		if caps["supportsPublicSite"] != on {
			t.Fatalf("on=%v: got %v", on, caps["supportsPublicSite"])
		}
	}
}

func TestPublicSiteRulesFollowVaultSwitch(t *testing.T) {
	s, srv, jar := newPublicSiteAdmin(t, true)
	if code, out := psDo(t, srv, jar, "PUT", map[string]any{"rules": map[string]string{"a": "public"}}); code != 200 {
		t.Fatalf("PUT: %d %v", code, out)
	}
	if !s.currentPolicy().IsPublic("a/x.md") {
		t.Fatal("policy should see rule in vault A")
	}
	if _, err := s.switchVaultRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	_, got := psDo(t, srv, jar, "GET", nil)
	if r, _ := got["rules"].(map[string]any); len(r) != 0 {
		t.Fatalf("rules after switch: %v", got["rules"])
	}
	if s.currentPolicy().IsPublic("a/x.md") {
		t.Fatal("policy must be empty for vault B")
	}
}
