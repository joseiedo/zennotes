package httpserver

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/ZenNotes/zennotes/apps/server/internal/publicsite"
	"github.com/ZenNotes/zennotes/apps/server/internal/vault"
	"github.com/go-chi/chi/v5"
)

// The public site: an opt-in read-only website at "/". Every decision about
// what may leave the server goes through publicsite.Policy, and every request
// is mapped to content by exact match against the vault's own listing — a
// URL-derived path never reaches the filesystem or the policy on its own.
// Anything private, missing or malformed gets the same 404 per route kind.

const (
	publicAPIPrefix    = "/_zn/api/"
	publicStaticPrefix = "/_zn/static/"
	publicReserved     = "/_zn/"
)

// publicKillSwitchSW replaces any service worker an earlier root-mounted admin
// app registered at "/": it unregisters itself and reloads its clients.
const publicKillSwitchSW = `self.addEventListener('install',()=>self.skipWaiting()); self.addEventListener('activate',e=>e.waitUntil(self.registration.unregister().then(()=>self.clients.matchAll()).then(cs=>cs.forEach(c=>c.navigate(c.url)))))`

var publicNotFoundJSON = []byte(`{"error":"not found"}` + "\n")

func publicContentSecurityPolicy() string {
	return strings.Replace(contentSecurityPolicy(), "img-src 'self' data: blob: https:", "img-src 'self' data: blob:", 1)
}

// publicSecurityHeaders mirrors securityHeadersMiddleware with the public CSP.
func (s *Server) publicSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", publicContentSecurityPolicy())
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.effectiveScheme(r) == "https" {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		h.Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// publicRouter serves the public site. No CORS, no auth, no session cookie.
func (s *Server) publicRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(s.publicSecurityHeaders)
	r.HandleFunc("/", s.publicDispatch)
	r.HandleFunc("/*", s.publicDispatch)
	return r
}

// bufferedResponse collects a public response in memory so it can be built
// under publicGate and written to the (possibly slow) client after releasing it.
type bufferedResponse struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }
func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.code == 0 {
		b.code = http.StatusOK
	}
	return b.body.Write(p)
}
func (b *bufferedResponse) WriteHeader(code int) {
	if b.code == 0 {
		b.code = code
	}
}

func (s *Server) publicDispatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// Assets stream from an open file: they take the gate themselves only
	// while deciding visibility and opening the file.
	if r.URL.Path == publicAPIPrefix+"asset" {
		s.publicAssetAPI(w, r)
		return
	}
	// Everything else is small and built in memory under the read side of
	// publicGate, so a concurrent rename/move can never be observed half-done.
	buf := &bufferedResponse{header: w.Header()}
	s.publicGate.RLock()
	s.publicRoute(buf, r)
	s.publicGate.RUnlock()
	if buf.code == 0 {
		buf.code = http.StatusOK
	}
	w.WriteHeader(buf.code)
	if r.Method != http.MethodHead {
		_, _ = w.Write(buf.body.Bytes())
	}
}

func (s *Server) publicRoute(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/sitemap.xml":
		s.publicSitemap(w)
	case p == "/robots.txt":
		s.publicRobots(w)
	case p == "/sw.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write([]byte(publicKillSwitchSW))
	case strings.HasPrefix(p, publicAPIPrefix):
		switch strings.TrimPrefix(p, publicAPIPrefix) {
		case "site":
			s.publicSiteAPI(w, r)
		case "note":
			s.publicNoteAPI(w, r)
		case "asset":
			s.publicAssetAPI(w, r)
		case "search":
			s.publicSearchAPI(w, r)
		default:
			s.publicNotFoundAPI(w)
		}
	case strings.HasPrefix(p, publicStaticPrefix):
		s.publicStaticFile(w, strings.TrimPrefix(p, publicStaticPrefix))
	case strings.HasPrefix(p, publicReserved):
		s.publicNotFoundPage(w)
	default:
		s.publicPage(w, p)
	}
}

// publicNotFoundPage and publicNotFoundAPI are the only 404 writers. Their
// output depends on nothing about the request.
func (s *Server) publicNotFoundPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write(s.renderNotFoundShell())
}

func (s *Server) publicNotFoundAPI(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write(publicNotFoundJSON)
}

// publicIndex is one request's view of what is public.
type publicIndex struct {
	// v is the vault captured once for the whole request. Every read in the
	// request goes through it, so a concurrent vault switch can never pair
	// one vault's policy with another vault's content.
	v      *vault.Vault
	policy publicsite.Policy
	// pages are the public notes in ListNotes order, by canonical path.
	pages []vault.NoteMeta
	// home is the home note's path, "" unless it is a public page.
	home string
	// notes is the full listing (for excalidraw drawings embedded as assets).
	notes []vault.NoteMeta
}

// isPublicPage: a public markdown note that is not an Excalidraw drawing.
func isPublicPage(p publicsite.Policy, m vault.NoteMeta) bool {
	return p.IsPublicNote(m) && !strings.HasSuffix(strings.ToLower(m.Path), ".excalidraw.md")
}

// loadPublicIndex fails closed: any error yields an empty index.
func (s *Server) loadPublicIndex() publicIndex {
	idx := publicIndex{policy: publicsite.NewPolicy(publicsite.SiteRules{})}
	if s.publicRules == nil {
		return idx
	}
	v := s.currentVault()
	if v == nil {
		return idx
	}
	rules := s.publicRules.Get(v.Root())
	notes, err := v.ListNotes()
	if err != nil {
		return idx
	}
	idx.v = v
	idx.policy = publicsite.NewPolicy(rules)
	idx.notes = notes
	for _, n := range notes {
		if isPublicPage(idx.policy, n) {
			idx.pages = append(idx.pages, n)
			if rules.HomeNote != "" && n.Path == rules.HomeNote {
				idx.home = n.Path
			}
		}
	}
	return idx
}

// page finds a public page by exact canonical path.
func (idx publicIndex) page(rel string) (vault.NoteMeta, bool) {
	if rel == "" {
		return vault.NoteMeta{}, false
	}
	for _, n := range idx.pages {
		if n.Path == rel {
			return n, true
		}
	}
	return vault.NoteMeta{}, false
}

// readBody reads a page's raw body from the index's own vault; meta must come
// from idx.pages.
func (idx publicIndex) readBody(meta vault.NoteMeta) (string, bool) {
	if idx.v == nil {
		return "", false
	}
	content, err := idx.v.ReadNote(meta.Path)
	if err != nil || content.Path != meta.Path {
		return "", false
	}
	return content.Body, true
}

type publicSiteNote struct {
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	UpdatedAt int64    `json:"updatedAt"`
	Wikilinks []string `json:"wikilinks"`
}

type publicSitePayload struct {
	HomeNote *string          `json:"homeNote"`
	Notes    []publicSiteNote `json:"notes"`
}

func writePublicJSON(w http.ResponseWriter, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(data, '\n'))
}

func (s *Server) publicSiteAPI(w http.ResponseWriter, _ *http.Request) {
	idx := s.loadPublicIndex()
	out := publicSitePayload{Notes: []publicSiteNote{}}
	if idx.home != "" {
		home := idx.home
		out.HomeNote = &home
	}
	for _, n := range idx.pages {
		body, ok := idx.readBody(n)
		if !ok {
			continue
		}
		// Wikilinks come from the sanitized body: links inside comments or
		// frontmatter must not surface here (NoteMeta.Wikilinks uses the raw body).
		links := vault.ExtractWikilinks(publicsite.SanitizeBody(body))
		if links == nil {
			links = []string{}
		}
		out.Notes = append(out.Notes, publicSiteNote{
			Path:      n.Path,
			Title:     n.Title,
			UpdatedAt: n.UpdatedAt,
			Wikilinks: links,
		})
	}
	writePublicJSON(w, out)
}

type publicNotePayload struct {
	Path      string `json:"path"`
	Title     string `json:"title"`
	UpdatedAt int64  `json:"updatedAt"`
	Body      string `json:"body"`
}

func (s *Server) publicNoteAPI(w http.ResponseWriter, r *http.Request) {
	idx := s.loadPublicIndex()
	meta, ok := idx.page(r.URL.Query().Get("path"))
	if !ok {
		s.publicNotFoundAPI(w)
		return
	}
	body, ok := idx.readBody(meta)
	if !ok {
		s.publicNotFoundAPI(w)
		return
	}
	writePublicJSON(w, publicNotePayload{
		Path:      meta.Path,
		Title:     meta.Title,
		UpdatedAt: meta.UpdatedAt,
		Body:      publicsite.SanitizeBody(body),
	})
}

// isTrashPath reports whether rel lives in the vault's trash folder.
func isTrashPath(rel string, systemPaths map[string]string) bool {
	first := rel
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		first = rel[:i]
	}
	folder, ok := vault.SystemFolderForDirName(first, systemPaths)
	return ok && folder == vault.FolderTrash
}

func (s *Server) publicAssetAPI(w http.ResponseWriter, r *http.Request) {
	f, info, resolved, ok := s.openPublicAsset(r)
	if !ok {
		s.publicNotFoundAPI(w)
		return
	}
	defer f.Close()
	ext := strings.ToLower(path.Ext(resolved))
	h := w.Header()
	// Assets share the origin with /admin: never let one run as a document.
	h.Set("Content-Security-Policy", "sandbox")
	ctype := ""
	if publicInlineAssetExts[ext] {
		ctype = mime.TypeByExtension(ext)
	}
	if ctype == "" {
		ctype = "application/octet-stream"
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(resolved)}))
	}
	h.Set("Content-Type", ctype)
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// openPublicAsset decides whether the requested asset is public and opens it,
// all under the read side of publicGate. The caller closes the file.
func (s *Server) openPublicAsset(r *http.Request) (*os.File, os.FileInfo, string, bool) {
	s.publicGate.RLock()
	defer s.publicGate.RUnlock()
	q := r.URL.Query()
	href := q.Get("href")
	idx := s.loadPublicIndex()
	meta, ok := idx.page(q.Get("note"))
	if !ok || href == "" {
		return nil, nil, "", false
	}
	body, ok := idx.readBody(meta)
	if !ok {
		return nil, nil, "", false
	}
	listed := false
	for _, ref := range publicsite.ExtractAssetRefs(meta.Path, publicsite.SanitizeBody(body)) {
		if ref == href {
			listed = true
			break
		}
	}
	if !listed {
		return nil, nil, "", false
	}

	v := idx.v
	if v == nil {
		return nil, nil, "", false
	}
	settings, err := v.GetSettings()
	if err != nil {
		return nil, nil, "", false
	}
	assets, err := v.ListAssets()
	if err != nil {
		return nil, nil, "", false
	}
	candidates := make([]string, 0, len(assets))
	for _, a := range assets {
		if !isTrashPath(a.Path, settings.SystemFolderPaths) {
			candidates = append(candidates, a.Path)
		}
	}
	// Excalidraw drawings are listed as notes, not assets; a public note may
	// still embed one.
	for _, n := range idx.notes {
		l := strings.ToLower(n.Path)
		if n.Folder != vault.FolderTrash && (strings.HasSuffix(l, ".excalidraw") || strings.HasSuffix(l, ".excalidraw.md")) {
			candidates = append(candidates, n.Path)
		}
	}
	resolved, ok := publicsite.ResolveAssetRef(meta.Path, href, candidates)
	if !ok || isTrashPath(resolved, settings.SystemFolderPaths) {
		return nil, nil, "", false
	}
	abs, err := v.AssetAbsPath(resolved)
	if err != nil {
		return nil, nil, "", false
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, nil, "", false
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, "", false
	}
	return f, info, resolved, true
}

// publicInlineAssetExts are the asset types served inline (media and PDF).
// Everything else is a sandboxed octet-stream download.
var publicInlineAssetExts = map[string]bool{
	".apng": true, ".avif": true, ".gif": true, ".jpeg": true, ".jpg": true, ".png": true, ".svg": true, ".webp": true,
	".pdf": true,
	".aac": true, ".flac": true, ".m4a": true, ".mp3": true, ".ogg": true, ".wav": true,
	".m4v": true, ".mov": true, ".mp4": true, ".ogv": true, ".webm": true,
}

func (s *Server) publicStaticFile(w http.ResponseWriter, rel string) {
	s.mu.RLock()
	fsys := s.publicStatic
	s.mu.RUnlock()
	if fsys == nil || rel == "" || !fs.ValidPath(rel) {
		s.publicNotFoundPage(w)
		return
	}
	data, err := fs.ReadFile(fsys, rel)
	if err != nil {
		s.publicNotFoundPage(w)
		return
	}
	ctype := mime.TypeByExtension(strings.ToLower(path.Ext(rel)))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	_, _ = w.Write(data)
}

// publicPage serves the shell for "/" (home) or a public note's URL: the
// decoded request path plus ".md", matched exactly against public pages.
func (s *Server) publicPage(w http.ResponseWriter, urlPath string) {
	idx := s.loadPublicIndex()
	if urlPath == "/" {
		var page *PublicPage
		if meta, ok := idx.page(idx.home); ok {
			page = s.publicPageFor(idx, meta, "/")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(s.renderShell(page))
		return
	}
	rel := strings.TrimPrefix(urlPath, "/") + ".md"
	meta, ok := idx.page(rel)
	if !ok {
		s.publicNotFoundPage(w)
		return
	}
	page := s.publicPageFor(idx, meta, publicPageURL(meta.Path))
	if page == nil {
		s.publicNotFoundPage(w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(s.renderShell(page))
}

func (s *Server) publicPageFor(idx publicIndex, meta vault.NoteMeta, urlPath string) *PublicPage {
	raw, ok := idx.readBody(meta)
	if !ok {
		return nil
	}
	sanitized := publicsite.SanitizeBody(raw)
	return &PublicPage{
		Title:       meta.Title,
		Description: publicDescription(publicsite.FrontmatterDescription(raw), sanitized),
		Body:        sanitized,
		URLPath:     urlPath,
	}
}

const publicSearchMax = 50

var publicWSRe = regexp.MustCompile(`\s+`)

// publicLineKey mirrors the vault's search-line normalisation (collapse
// whitespace, trim, cap at 220 bytes) so a raw match can be compared with a
// line of the sanitized body.
func publicLineKey(line string) string {
	c := strings.TrimSpace(publicWSRe.ReplaceAllString(line, " "))
	if len(c) > 220 {
		c = c[:220]
	}
	return c
}

type publicSearchHit struct {
	Path     string `json:"path"`
	Title    string `json:"title"`
	LineText string `json:"lineText"`
}

// publicSearchAPI searches through the request's own vault, keeps only public
// pages, and only lines that survive sanitizing (never frontmatter, comments
// or other stripped text).
func (s *Server) publicSearchAPI(w http.ResponseWriter, r *http.Request) {
	if !s.publicSearchLimiter.allow(s.clientAddressKey(r)) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"too many requests"}` + "\n"))
		return
	}
	out := []publicSearchHit{}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writePublicJSON(w, out)
		return
	}
	idx := s.loadPublicIndex()
	if idx.v == nil {
		writePublicJSON(w, out)
		return
	}
	matches, err := idx.v.SearchText(q)
	if err != nil {
		writePublicJSON(w, out)
		return
	}
	needle := strings.ToLower(q)
	lines := map[string]map[string]bool{}
	for _, m := range matches {
		if len(out) >= publicSearchMax {
			break
		}
		meta, ok := idx.page(m.Path)
		if !ok {
			continue
		}
		set, seen := lines[meta.Path]
		if !seen {
			if body, ok := idx.readBody(meta); ok {
				set = map[string]bool{}
				for _, l := range strings.Split(publicsite.SanitizeBody(body), "\n") {
					set[publicLineKey(l)] = true
				}
			}
			lines[meta.Path] = set
		}
		if set == nil || m.LineText == "" || !set[m.LineText] || !strings.Contains(strings.ToLower(m.LineText), needle) {
			continue
		}
		out = append(out, publicSearchHit{Path: meta.Path, Title: meta.Title, LineText: m.LineText})
	}
	writePublicJSON(w, out)
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func (s *Server) publicSitemap(w http.ResponseWriter) {
	base := strings.TrimRight(s.currentConfig().PublicURL, "/")
	if base == "" {
		s.publicNotFoundPage(w)
		return
	}
	idx := s.loadPublicIndex()
	set := sitemapSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	if idx.home != "" {
		set.URLs = append(set.URLs, sitemapURL{Loc: base + "/"})
	}
	for _, n := range idx.pages {
		u := sitemapURL{Loc: base + publicPageURL(n.Path)}
		if n.UpdatedAt > 0 {
			u.LastMod = time.UnixMilli(n.UpdatedAt).UTC().Format("2006-01-02")
		}
		set.URLs = append(set.URLs, u)
	}
	data, err := xml.Marshal(set)
	if err != nil {
		s.publicNotFoundPage(w)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(data)
}

func (s *Server) publicRobots(w http.ResponseWriter) {
	cfg := s.currentConfig()
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	b.WriteString("Disallow: " + strings.TrimRight(cfg.BasePath, "/") + "/\n")
	b.WriteString("Disallow: /_zn/api/\n")
	if base := strings.TrimRight(cfg.PublicURL, "/"); base != "" {
		b.WriteString("Sitemap: " + base + "/sitemap.xml\n")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
