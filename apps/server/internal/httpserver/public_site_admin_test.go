package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ZenNotes/zennotes/apps/server/internal/config"
	"github.com/ZenNotes/zennotes/apps/server/internal/publicsite"
	"github.com/ZenNotes/zennotes/apps/server/internal/vault"
)

// newPublicSiteAdmin returns the server, the admin base URL (under /admin when
// the public site is on, since the public site then owns "/") and a logged-in
// cookie jar.
func newPublicSiteAdmin(t *testing.T, on bool) (*Server, string, http.CookieJar) {
	t.Helper()
	cfg := config.Config{
		VaultPath:  t.TempDir(),
		Bind:       "127.0.0.1:0",
		AuthToken:  "secret-token",
		PublicSite: on,
	}
	if on {
		cfg.BasePath = "/admin"
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
	base := srv.URL + cfg.BasePath
	return s, base, loginAtBase(t, base, "secret-token")
}

func loginAtBase(t *testing.T, base, token string) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"token": token})
	resp, err := client.Post(base+"/api/session/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status: %d", resp.StatusCode)
	}
	return jar
}

func psDo(t *testing.T, base string, jar http.CookieJar, method string, body any) (int, map[string]any) {
	t.Helper()
	// A PUT must name the version it was based on; tests that don't care
	// about staleness get the current one filled in.
	if m, ok := body.(map[string]any); ok && method == "PUT" && jar != nil {
		if _, set := m["version"]; !set {
			_, cur := psDo(t, base, jar, "GET", nil)
			with := map[string]any{"version": cur["version"]}
			for k, v := range m {
				with[k] = v
			}
			body = with
		}
	}
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, base+"/api/public-site", rdr)
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
		resp, err := http.Get(srv + "/api/capabilities")
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
	if !s.loadPublicIndex().policy.IsPublic("a/x.md") {
		t.Fatal("policy should see rule in vault A")
	}
	if _, err := s.switchVaultRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	_, got := psDo(t, srv, jar, "GET", nil)
	if r, _ := got["rules"].(map[string]any); len(r) != 0 {
		t.Fatalf("rules after switch: %v", got["rules"])
	}
	if s.loadPublicIndex().policy.IsPublic("a/x.md") {
		t.Fatal("policy must be empty for vault B")
	}
}

// M4: homeNotePublic uses the same predicate as the public "/": a drawing (or
// a note whose URL is shadowed by a route) is never a served home page.
func TestPublicSiteAdminHomeNotePublicMatchesPublicRoot(t *testing.T) {
	s, srv, jar := newPublicSiteAdmin(t, true)
	root := s.currentVault().Root()
	for rel, body := range map[string]string{"Blog/d.excalidraw.md": "drawing", "admin/x.md": "x", "Blog/ok.md": "ok"} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rules := map[string]string{"Blog": "public", "admin": "public"}
	for home, want := range map[string]bool{"Blog/d.excalidraw.md": false, "admin/x.md": false, "Blog/ok.md": true} {
		_, got := psDo(t, srv, jar, "PUT", map[string]any{"homeNote": home, "rules": rules})
		if got["homeNotePublic"] != want {
			t.Errorf("%s: homeNotePublic = %v, want %v", home, got["homeNotePublic"], want)
		}
	}
}

func TestPublicSiteAdminRejectsStalePut(t *testing.T) {
	_, srv, jar := newPublicSiteAdmin(t, true)
	_, first := psDo(t, srv, jar, "GET", nil)
	v1, _ := first["version"].(string)
	if v1 == "" {
		t.Fatalf("GET has no version: %+v", first)
	}
	code, saved := psDo(t, srv, jar, "PUT", map[string]any{"version": v1, "rules": map[string]string{"Blog": "public"}})
	if code != http.StatusOK || saved["version"] == v1 {
		t.Fatalf("first PUT: %d %+v", code, saved)
	}
	// A second tab still holding v1 must not overwrite the newer rules.
	code, _ = psDo(t, srv, jar, "PUT", map[string]any{"version": v1, "rules": map[string]string{}})
	if code != http.StatusConflict {
		t.Fatalf("stale PUT: %d, want 409", code)
	}
	code, _ = psDo(t, srv, jar, "PUT", map[string]any{"version": "", "rules": map[string]string{}})
	if code != http.StatusConflict {
		t.Fatalf("PUT without version: %d, want 409", code)
	}
	_, now := psDo(t, srv, jar, "GET", nil)
	if rules, _ := now["rules"].(map[string]any); rules["Blog"] != "public" {
		t.Fatalf("rules changed by rejected PUTs: %+v", now)
	}
}

// A rename carries a hidden mark; a settings tab opened before it must not
// drop that mark with its next save.
func TestPublicSiteAdminStaleTabCannotDropCarriedHiddenMark(t *testing.T) {
	s, srv, jar := newPublicSiteAdmin(t, true)
	root := s.currentVault().Root()
	if err := s.publicRules.Put(root, publicsite.SiteRules{Rules: map[string]publicsite.Mark{"Blog": publicsite.MarkPublic, "Blog/Drafts": publicsite.MarkHidden}}); err != nil {
		t.Fatal(err)
	}
	_, tab := psDo(t, srv, jar, "GET", nil)
	if err := s.publicRules.RewriteHidden(root, "Blog/Drafts", "Blog/Later", false); err != nil {
		t.Fatal(err)
	}
	code, _ := psDo(t, srv, jar, "PUT", map[string]any{"version": tab["version"], "rules": map[string]string{"Blog": "public", "Blog/Drafts": "hidden", "Other": "public"}})
	if code != http.StatusConflict {
		t.Fatalf("stale PUT after rename: %d, want 409", code)
	}
	if got := s.publicRules.Get(root).Rules["Blog/Later"]; got != publicsite.MarkHidden {
		t.Fatalf("carried hidden mark lost: %q", got)
	}
}
