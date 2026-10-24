package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ZenNotes/zennotes/apps/server/internal/config"
	"github.com/ZenNotes/zennotes/apps/server/internal/publicsite"
	"github.com/ZenNotes/zennotes/apps/server/internal/vault"
)

const testPublicURL = "https://notes.example.com"

var pngBytes = []byte("\x89PNG\r\n\x1a\nfake-image-bytes")

func testPublicBundle() fs.FS {
	return fstest.MapFS{
		"public.html":      &fstest.MapFile{Data: []byte(`<!doctype html><html><head><meta charset="utf-8"><title>ZenNotes</title></head><body><div id="zn-root"></div><div id="zn-seo"></div><script type="module" src="/_zn/static/assets/public.js"></script></body></html>`)},
		"assets/public.js": &fstest.MapFile{Data: []byte("console.log('public')")},
	}
}

func testAdminBundle() fs.FS {
	return fstest.MapFS{
		"index.html":           &fstest.MapFile{Data: []byte("<!doctype html><html><head></head><body>ADMIN-SPA-SHELL</body></html>")},
		"manifest.webmanifest": &fstest.MapFile{Data: []byte(`{"name":"admin"}`)},
	}
}

type publicFixture struct {
	s    *Server
	srv  *httptest.Server
	root string
}

// newPublicFixture writes files into a fresh vault (before vault.New, so the
// vault infers root-level notes), stores rules + home and starts the server
// with the public site on and the admin app under /admin.
func newPublicFixture(t *testing.T, files map[string]string, rules map[string]publicsite.Mark, home string) *publicFixture {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{
		VaultPath:  root,
		Bind:       "127.0.0.1:0",
		AuthToken:  "secret-token",
		PublicSite: true,
		BasePath:   "/admin",
		PublicURL:  testPublicURL,
	}
	v, err := vault.New(root, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := New(v, nil, testAdminBundle(), cfg)
	s.publicRules = publicsite.NewStore(filepath.Join(t.TempDir(), "public-site.json"))
	if err := s.publicRules.Put(v.Root(), publicsite.SiteRules{HomeNote: home, Rules: rules}); err != nil {
		t.Fatal(err)
	}
	s.SetPublicStatic(testPublicBundle())
	srv := httptest.NewServer(s.Router())
	t.Cleanup(srv.Close)
	return &publicFixture{s: s, srv: srv, root: root}
}

type capturedResponse struct {
	Status int
	Header http.Header
	Body   string
}

// fetchRaw sends rawPath verbatim (no client-side normalisation).
func fetchRaw(t *testing.T, srv *httptest.Server, rawPath string) capturedResponse {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.URL = &url.URL{Scheme: u.Scheme, Host: u.Host, Opaque: "//" + u.Host + rawPath}
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatalf("GET %s: %v", rawPath, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	h := resp.Header.Clone()
	h.Del("Date")
	return capturedResponse{Status: resp.StatusCode, Header: h, Body: string(body)}
}

func apiPath(endpoint string, q map[string]string) string {
	vals := url.Values{}
	for k, v := range q {
		vals.Set(k, v)
	}
	p := "/_zn/api/" + endpoint
	if len(vals) > 0 {
		p += "?" + vals.Encode()
	}
	return p
}

func leakFixtureFiles() map[string]string {
	return map[string]string{
		"Blog/a.md":               "---\nsecret: LEAK-S1\n---\n# Post A\n\nHello public world.\n\n![[img.png]]\n\nSee [[Private note]].\n\n%%LEAK-S2 [[Comment only link]]%%\n",
		"Blog/Drafts/d.md":        "LEAK-S3 draft body\n",
		"Private note.md":         "LEAK-S4 private body\n",
		"trash/Blog/t.md":         "LEAK-S5 trashed body\n",
		"Blog/.hidden/h.md":       "LEAK-S6 hidden body\n",
		"attachments/img.png":     string(pngBytes),
		"attachments/private.png": "PRIVATE-PNG",
		"Blog/draw.excalidraw":    `{"type":"excalidraw","elements":[]}`,
	}
}

func leakFixtureRules() map[string]publicsite.Mark {
	return map[string]publicsite.Mark{
		"Blog":        publicsite.MarkPublic,
		"Blog/Drafts": publicsite.MarkHidden,
		// The trashed copy sits under a public-marked path so only the
		// trash-folder rule keeps it private.
		"trash/Blog": publicsite.MarkPublic,
	}
}

func TestPublicLeakMatrix(t *testing.T) {
	f := newPublicFixture(t, leakFixtureFiles(), leakFixtureRules(), "Blog/a.md")

	// Sanity: the trashed note is listed, in the trash folder, under a
	// public-marked path, so only the trash rule keeps it private.
	notes, err := f.s.currentVault().ListNotes()
	if err != nil {
		t.Fatal(err)
	}
	foundTrash := false
	for _, n := range notes {
		if n.Path == "trash/Blog/t.md" && n.Folder == vault.FolderTrash {
			foundTrash = true
		}
	}
	if !foundTrash || !f.s.currentPolicy().IsPublic("trash/Blog/t.md") {
		t.Fatalf("fixture: trashed note not set up as intended (found=%v)", foundTrash)
	}

	forbidden := []string{"LEAK-S1", "LEAK-S2", "LEAK-S3", "LEAK-S4", "LEAK-S5", "LEAK-S6",
		"Drafts", "d.md", "private.png", "draw.excalidraw", ".hidden", "Comment only link"}
	for _, p := range []string{
		apiPath("site", nil),
		apiPath("note", map[string]string{"path": "Blog/a.md"}),
		"/",
		"/Blog/a",
		"/sitemap.xml",
	} {
		r := fetchRaw(t, f.srv, p)
		for _, bad := range forbidden {
			if strings.Contains(r.Body, bad) {
				t.Errorf("%s leaked %q:\n%s", p, bad, r.Body)
			}
		}
	}

	// Positive checks: the public content is actually there.
	site := fetchRaw(t, f.srv, apiPath("site", nil))
	if site.Status != 200 || !strings.Contains(site.Body, `"Blog/a.md"`) || !strings.Contains(site.Body, `"homeNote":"Blog/a.md"`) {
		t.Fatalf("site: %d %s", site.Status, site.Body)
	}
	var siteOut struct {
		Notes []struct {
			Path string `json:"path"`
		} `json:"notes"`
	}
	_ = json.Unmarshal([]byte(site.Body), &siteOut)
	if len(siteOut.Notes) != 1 {
		t.Fatalf("site notes = %+v, want only Blog/a.md", siteOut.Notes)
	}
	note := fetchRaw(t, f.srv, apiPath("note", map[string]string{"path": "Blog/a.md"}))
	if note.Status != 200 || !strings.Contains(note.Body, "Hello public world.") {
		t.Fatalf("note: %d %s", note.Status, note.Body)
	}
	if got := note.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("note Cache-Control = %q", got)
	}
	if page := fetchRaw(t, f.srv, "/Blog/a"); page.Status != 200 {
		t.Fatalf("/Blog/a status %d", page.Status)
	}

	// Page 404s are byte-identical.
	pageRef := fetchRaw(t, f.srv, "/does/not/exist")
	if pageRef.Status != 404 || !strings.Contains(pageRef.Body, "<title>Not found</title>") {
		t.Fatalf("page 404 ref: %d %s", pageRef.Status, pageRef.Body)
	}
	for _, p := range []string{
		"/Blog/Drafts/d", "/Private%20note", "/Blog/.hidden/h", "/Blog/%2e%2e/Private%20note",
		"/Blog/../Private%20note", "/blog/a", "/BLOG/A", "/Blog/a.md", "/Blog/a/", "/Blog%5Ca",
		"//Blog/a", "/trash/Blog/t", "/Blog/draw.excalidraw", "/Blog/draw", "/_zn/missing", "/_zn/",
		"/_zn/static/nope.js", "/_zn/static/../public.html", "/sitemap.xml", "/%2e%2e/etc/passwd",
	} {
		got := fetchRaw(t, f.srv, p)
		if !reflect.DeepEqual(got, pageRef) {
			t.Errorf("page 404 for %s differs:\n got %+v\nwant %+v", p, got, pageRef)
		}
	}

	// API 404s are byte-identical.
	apiRef := fetchRaw(t, f.srv, apiPath("note", map[string]string{"path": "missing.md"}))
	if apiRef.Status != 404 || strings.TrimSpace(apiRef.Body) != `{"error":"not found"}` {
		t.Fatalf("api 404 ref: %d %q", apiRef.Status, apiRef.Body)
	}
	for _, p := range []string{
		apiPath("note", map[string]string{"path": "Blog/Drafts/d.md"}),
		apiPath("note", map[string]string{"path": "Private note.md"}),
		apiPath("note", map[string]string{"path": "../x.md"}),
		apiPath("note", map[string]string{"path": "Blog/draw.excalidraw"}),
		apiPath("note", map[string]string{"path": "Blog/.hidden/h.md"}),
		apiPath("note", map[string]string{"path": "trash/Blog/t.md"}),
		apiPath("note", map[string]string{"path": "blog/a.md"}),
		apiPath("note", map[string]string{"path": "/Blog/a.md"}),
		apiPath("note", map[string]string{"path": "Blog\\a.md"}),
		apiPath("note", map[string]string{"path": "Blog/./a.md"}),
		apiPath("note", nil),
		apiPath("unknown", nil),
		"/_zn/api/",
		apiPath("asset", map[string]string{"note": "Blog/a.md", "href": "private.png"}),
		apiPath("asset", map[string]string{"note": "Private note.md", "href": "img.png"}),
		apiPath("asset", map[string]string{"note": "Blog/a.md"}),
	} {
		got := fetchRaw(t, f.srv, p)
		if !reflect.DeepEqual(got, apiRef) {
			t.Errorf("api 404 for %s differs:\n got %+v\nwant %+v", p, got, apiRef)
		}
	}

	// The embedded image is served.
	img := fetchRaw(t, f.srv, apiPath("asset", map[string]string{"note": "Blog/a.md", "href": "img.png"}))
	if img.Status != 200 || img.Body != string(pngBytes) || img.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("asset: %d %q %q", img.Status, img.Header.Get("Content-Type"), img.Body)
	}
	if img.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("asset missing nosniff")
	}
}

func TestPublicAssetRules(t *testing.T) {
	files := map[string]string{
		"Blog/a.md": "![[img.png]]\n\n![[old.png]]\n\n![[.secret/y.png]]\n\n![[pic.svg]]\n\n![[draw.excalidraw]]\n\n" +
			"%%![[private.png]]%%\n\n`![[code.png]]`\n\n[doc](/attachments/doc.pdf)\n",
		"attachments/img.png":     string(pngBytes),
		"attachments/private.png": "PRIVATE",
		"attachments/code.png":    "CODE",
		"attachments/doc.pdf":     "%PDF-1.4",
		"attachments/pic.svg":     `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
		"trash/old.png":           "TRASHED",
		"Blog/.secret/y.png":      "DOTTED",
		"Blog/draw.excalidraw":    `{"type":"excalidraw"}`,
	}
	f := newPublicFixture(t, files, map[string]publicsite.Mark{"Blog": publicsite.MarkPublic}, "")
	asset := func(href string) capturedResponse {
		return fetchRaw(t, f.srv, apiPath("asset", map[string]string{"note": "Blog/a.md", "href": href}))
	}
	apiRef := fetchRaw(t, f.srv, apiPath("note", map[string]string{"path": "missing.md"}))

	for _, href := range []string{
		"old.png",              // resolves into trash
		".secret/y.png",        // dot segment
		"private.png",          // only inside a comment
		"code.png",             // only inside code
		"attachments/img.png",  // not literally the href in the body
		"img.png?x=1",          // not literal
		"IMG.PNG",              // not literal
		"../attachments/x.pdf", // not present
	} {
		if got := asset(href); !reflect.DeepEqual(got, apiRef) {
			t.Errorf("asset %q should 404, got %d %q", href, got.Status, got.Body)
		}
	}

	if got := asset("img.png"); got.Status != 200 || got.Body != string(pngBytes) {
		t.Fatalf("img.png: %d", got.Status)
	}
	pdf := asset("/attachments/doc.pdf")
	if pdf.Status != 200 || pdf.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("pdf: %d %q", pdf.Status, pdf.Header.Get("Content-Type"))
	}
	svg := asset("pic.svg")
	if svg.Status != 200 || svg.Header.Get("Content-Security-Policy") != "sandbox" || !strings.HasPrefix(svg.Header.Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("svg: %d csp=%q ct=%q", svg.Status, svg.Header.Get("Content-Security-Policy"), svg.Header.Get("Content-Type"))
	}
	draw := asset("draw.excalidraw")
	if draw.Status != 200 || draw.Body != `{"type":"excalidraw"}` {
		t.Fatalf("excalidraw embed: %d %q", draw.Status, draw.Body)
	}
	// The drawing is never a page.
	if got := fetchRaw(t, f.srv, "/Blog/draw"); got.Status != 404 {
		t.Fatalf("drawing page: %d", got.Status)
	}
}

func TestPublicSiteMovesAdminUnderBasePath(t *testing.T) {
	f := newPublicFixture(t, leakFixtureFiles(), leakFixtureRules(), "Blog/a.md")
	pageRef := fetchRaw(t, f.srv, "/does/not/exist")
	for _, p := range []string{"/api/vault", "/vault", "/api/session", "/api/healthz", "/healthz", "/watch", "/api/watch", "/index.html", "/manifest.webmanifest", "/admin-x", "/notes"} {
		got := fetchRaw(t, f.srv, p)
		if !reflect.DeepEqual(got, pageRef) {
			t.Errorf("%s should be the public 404, got %d %q", p, got.Status, got.Body)
		}
		if strings.Contains(got.Body, "ADMIN-SPA-SHELL") {
			t.Errorf("%s served the admin shell", p)
		}
	}
	if got := fetchRaw(t, f.srv, "/admin/api/healthz"); got.Status != 200 {
		t.Fatalf("/admin/api/healthz: %d", got.Status)
	}
	admin := fetchRaw(t, f.srv, "/admin/")
	if admin.Status != 200 || !strings.Contains(admin.Body, "ADMIN-SPA-SHELL") {
		t.Fatalf("/admin/: %d %q", admin.Status, admin.Body)
	}
	if got := fetchRaw(t, f.srv, "/admin/api/vault"); got.Status != http.StatusUnauthorized {
		t.Fatalf("/admin/api/vault without auth: %d", got.Status)
	}
	// Public responses carry no CORS grants even for a matching Origin.
	req, _ := http.NewRequest(http.MethodGet, f.srv.URL+"/_zn/api/site", nil)
	req.Header.Set("Origin", f.srv.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "" || resp.Header.Get("Set-Cookie") != "" {
		t.Fatalf("public response carried CORS/cookie headers: %v", resp.Header)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "img-src 'self' data: blob:;") {
		t.Fatalf("public CSP img-src: %q", csp)
	}
}

func TestPublicSiteOffUnchanged(t *testing.T) {
	root := t.TempDir()
	srv, _ := newTestServer(t, config.Config{VaultPath: root, Bind: "127.0.0.1:0", AllowInsecureNoAuth: true})
	resp, err := http.Get(srv.URL + "/api/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("/api/healthz: %d", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/_zn/api/site")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatalf("public API must not exist with the public site off")
	}
}

func TestPublicServiceWorkerKillSwitch(t *testing.T) {
	f := newPublicFixture(t, leakFixtureFiles(), leakFixtureRules(), "")
	got := fetchRaw(t, f.srv, "/sw.js")
	if got.Status != 200 || !strings.Contains(got.Body, "unregister") || !strings.Contains(got.Body, "skipWaiting") {
		t.Fatalf("/sw.js: %d %q", got.Status, got.Body)
	}
	if ct := got.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("/sw.js content-type %q", ct)
	}
}

func TestPublicHomeFallback(t *testing.T) {
	files := leakFixtureFiles()
	files["Blog/Drafts/Hidden Home.md"] = "hidden home body"
	f := newPublicFixture(t, files, leakFixtureRules(), "Blog/Drafts/Hidden Home.md")
	got := fetchRaw(t, f.srv, "/")
	if got.Status != 200 {
		t.Fatalf("/: %d", got.Status)
	}
	if strings.Contains(got.Body, "Hidden Home") || strings.Contains(got.Body, "hidden home body") {
		t.Fatalf("hidden home leaked: %s", got.Body)
	}
	site := fetchRaw(t, f.srv, apiPath("site", nil))
	if !strings.Contains(site.Body, `"homeNote":null`) {
		t.Fatalf("site homeNote should be null: %s", site.Body)
	}
}

func TestPublicShellSEO(t *testing.T) {
	files := leakFixtureFiles()
	files["Blog/<script>x.md"] = "---\ndescription: \"Desc <b>bold</b>\"\n---\nFirst para <i>x</i>.\n\nSecond para.\n"
	f := newPublicFixture(t, files, leakFixtureRules(), "Blog/a.md")

	page := fetchRaw(t, f.srv, "/Blog/a")
	if page.Status != 200 {
		t.Fatalf("/Blog/a: %d", page.Status)
	}
	for _, want := range []string{
		"<title>a</title>",
		`<meta property="og:url" content="https://notes.example.com/Blog/a">`,
		`<link rel="canonical" href="https://notes.example.com/Blog/a">`,
		`<meta property="og:type" content="article">`,
		`<meta name="twitter:card" content="summary">`,
		"<p>Hello public world.</p>",
		`<meta name="description" content="# Post A Hello public world.`,
	} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("/Blog/a missing %q:\n%s", want, page.Body)
		}
	}
	if strings.Count(page.Body, "<title>") != 1 {
		t.Errorf("expected exactly one <title>: %s", page.Body)
	}
	seo := page.Body[strings.Index(page.Body, `<div id="zn-seo">`):]
	if !strings.Contains(seo, "Hello public world.") {
		t.Errorf("zn-seo missing body text")
	}

	x := fetchRaw(t, f.srv, "/Blog/%3Cscript%3Ex")
	if x.Status != 200 {
		t.Fatalf("script page: %d", x.Status)
	}
	if strings.Contains(x.Body, "<script>x") || strings.Contains(x.Body, "<b>bold") || strings.Contains(x.Body, "<i>x") {
		t.Fatalf("unescaped content:\n%s", x.Body)
	}
	for _, want := range []string{
		"<title>&lt;script&gt;x</title>",
		`content="Desc &lt;b&gt;bold&lt;/b&gt;"`,
		"<p>First para &lt;i&gt;x&lt;/i&gt;.</p><p>Second para.</p>",
		`https://notes.example.com/Blog/%3Cscript%3Ex"`,
	} {
		if !strings.Contains(x.Body, want) {
			t.Errorf("script page missing %q:\n%s", want, x.Body)
		}
	}

	home := fetchRaw(t, f.srv, "/")
	if !strings.Contains(home.Body, `<meta property="og:url" content="https://notes.example.com/">`) || !strings.Contains(home.Body, "<title>a</title>") {
		t.Fatalf("home shell:\n%s", home.Body)
	}

	// Static bundle files are served; the built-in shell is used without one.
	js := fetchRaw(t, f.srv, "/_zn/static/assets/public.js")
	if js.Status != 200 || js.Body != "console.log('public')" {
		t.Fatalf("static: %d %q", js.Status, js.Body)
	}
}

func TestPublicShellWithoutBundle(t *testing.T) {
	f := newPublicFixture(t, leakFixtureFiles(), leakFixtureRules(), "Blog/a.md")
	f.s.SetPublicStatic(nil)
	page := fetchRaw(t, f.srv, "/Blog/a")
	if page.Status != 200 || !strings.Contains(page.Body, `<div id="zn-root"></div>`) || !strings.Contains(page.Body, "<title>a</title>") {
		t.Fatalf("built-in shell: %d %s", page.Status, page.Body)
	}
	nf := fetchRaw(t, f.srv, "/nope")
	if nf.Status != 404 || !strings.Contains(nf.Body, "<title>Not found</title>") {
		t.Fatalf("built-in 404: %d %s", nf.Status, nf.Body)
	}
	if st := fetchRaw(t, f.srv, "/_zn/static/assets/public.js"); !reflect.DeepEqual(st, nf) {
		t.Fatalf("static without bundle should be the page 404: %d", st.Status)
	}
}

func TestPublicNonGetMethods(t *testing.T) {
	f := newPublicFixture(t, leakFixtureFiles(), leakFixtureRules(), "Blog/a.md")
	resp, err := http.Post(f.srv.URL+"/_zn/api/note?path=Blog/a.md", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 200 || strings.Contains(string(body), "Hello") {
		t.Fatalf("POST should not read notes: %d %s", resp.StatusCode, body)
	}
}
