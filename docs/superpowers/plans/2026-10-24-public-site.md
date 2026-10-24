# Public Site Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Self-hosted ZenNotes serves a read-only public site at `/` for owner-chosen folders, while the full app moves to `/admin`.

**Architecture:** Go server gets a `publicsite` package (rules, visibility, sanitizing, asset-ref resolution) and a second chi router mounted at `/` beside the existing inner router (mounted at the base path, default `/admin`). The web workspace gets a second, separate Vite build (`public.html` → `dist-public`, base `/_zn/static/`) that reuses app-core's `renderMarkdown` and diagram/math enhancers. The admin app gets a "Public site" settings section backed by two protected API endpoints.

**Tech Stack:** Go 1.25 + chi v5 (no new Go deps), React 18 + Vite + vitest, TypeScript.

**Spec:** `docs/superpowers/specs/2026-10-24-public-site-design.md`

## Global Constraints

- Nothing private leaks: every public handler decides visibility only through `publicsite.Policy.IsPublic` / `IsPublicNote`.
- Fail closed: missing/corrupt rules file, unknown path, renamed folder, handler error → treated as private / 404.
- Private, missing and malformed public requests return a **byte-identical** 404 per route kind (page 404 shell; API 404 JSON `{"error":"not found"}`), same headers.
- Public site is opt-in: `ZENNOTES_PUBLIC_SITE=1`. Off → server behaves exactly as today (existing tests stay green untouched).
- On: base path defaults to `/admin`; explicit non-root `ZENNOTES_BASE_PATH` respected; never `/`.
- On + no auth token: refuse to start unless `ZENNOTES_ADMIN_PROXY_AUTH=1`.
- Public URL base for canonical/OG/sitemap: `ZENNOTES_PUBLIC_URL` (e.g. `https://notes.joseiedo.com`).
- Reserved public prefix: `/_zn/` (API `/_zn/api/*`, bundle `/_zn/static/*`).
- Always private: notes with `Folder == trash`, any path segment starting with `.`, `.excalidraw` files as pages, comment sidecars, paths escaping the vault.
- Excalidraw: never a page/tree/search/sitemap entry; only served as an asset embedded by a public note.
- Rules file: `<dir of server.json>/public-site.json`, keyed by absolute vault root.
- Public responses: `Cache-Control: no-cache`. Public handlers never read or set the session cookie.

## Review Focus

1. Owner's browser still has the old root-scope service worker (`/sw.js`, cache-first shell) → visiting `/` must not show a cached admin shell. Public router serves a kill-switch `/sw.js` (Task 5 test).
2. Note bodies with `%%…%%` / `<!-- -->` / `---` frontmatter **inside fenced code blocks** → kept verbatim; only real comments/frontmatter stripped (Task 3 test).
3. Path tricks on public endpoints: `..`, URL-encoded `%2e%2e`, absolute paths, backslashes, case variants (`blog/Post` vs `Blog/Post`) → 404 unless the exact public path (Task 5 leak matrix).
4. A public note's asset href that resolves via basename fallback to a file in a hidden/dot folder → still served only if the resolved file isn't under a dot segment or trash; a href not literally present as a link/embed in the sanitized body → 404 (Task 3 + Task 5 tests).
5. Vault switched at runtime (`switchVaultRoot`) → rules of the old vault must not apply; new vault with no rules → nothing public (Task 2 test).

---

### Task 1: Config, startup guard, cookie path

**Files:**
- Modify: `apps/server/internal/config/config.go` (Config struct, `Load`)
- Modify: `apps/server/internal/config/config_test.go`
- Modify: `apps/server/cmd/zennotes-server/main.go` (call validation after `config.Load()`)
- Modify: `apps/server/internal/httpserver/security.go:470-483` (`sessionCookie`)
- Test: `apps/server/internal/httpserver/basepath_test.go`

**Interfaces:**
- Produces: `Config.PublicSite bool`, `Config.PublicURL string`, `Config.AdminProxyAuth bool`; `config.PublicSitePath() string` (`filepath.Join(filepath.Dir(configFilePath()), "public-site.json")`); `config.ValidatePublicSite(cfg Config) error`.

- [ ] **Step 1: Failing config tests** in `config_test.go`:
  - `TestPublicSiteDefaultsBasePathToAdmin`: env `ZENNOTES_PUBLIC_SITE=1`, no base path → `cfg.BasePath == "/admin"`, `cfg.PublicSite == true`.
  - `TestPublicSiteKeepsExplicitBasePath`: plus `ZENNOTES_BASE_PATH=/zen` → `"/zen"`.
  - `TestPublicSiteOffKeepsRoot`: no env → `BasePath == ""`, `PublicSite == false`.
  - `TestValidatePublicSiteRefusesNoAuth`: `{PublicSite:true, BasePath:"/admin", AuthToken:""}` → error whose message contains `ZENNOTES_AUTH_TOKEN` and `ZENNOTES_ADMIN_PROXY_AUTH`; with `AdminProxyAuth:true` → nil; with `AuthToken:"x"` → nil; `{PublicSite:false}` → nil.
  - `TestValidatePublicSiteRefusesRootBasePath`: `{PublicSite:true, AuthToken:"x", BasePath:""}` → error.
  - `TestLoadPublicURL`: `ZENNOTES_PUBLIC_URL=https://notes.example.com/` → `"https://notes.example.com"` (trailing slash trimmed).
- [ ] **Step 2:** `cd apps/server && go test ./internal/config/ -run 'PublicSite|PublicURL'` → FAIL (undefined fields).
- [ ] **Step 3:** Implement. Env names: `ZENNOTES_PUBLIC_SITE`, `ZENNOTES_PUBLIC_URL`, `ZENNOTES_ADMIN_PROXY_AUTH` (via existing `envEnabled`). Base-path default applied after `NormalizeBasePath`. In `main.go`, `log.Fatal` on `ValidatePublicSite` error, before the existing no-token check.
- [ ] **Step 4:** Re-run → PASS.
- [ ] **Step 5: Failing cookie test** `TestBasePathSessionCookiePath` in `basepath_test.go`: server with `BasePath:"/admin"`, `AuthToken:"tok"` (not insecure); POST `/admin/api/session/login` with the token (match the body shape `sessionLogin` expects) → `Set-Cookie` has `Path=/admin/api`; then GET `/admin/api/vault` with that cookie → 200. Root deployment still yields `Path=/api`.
- [ ] **Step 6:** Run → FAIL. Set `Path: s.currentConfig().BasePath + "/api"`. Run `go test ./...` → PASS.
- [ ] **Step 7: Commit** `fix(server): session cookie honours base path; feat(config): public site settings`.

### Task 2: Rules store and visibility policy

**Files:**
- Create: `apps/server/internal/publicsite/rules.go`, `apps/server/internal/publicsite/policy.go`
- Test: `apps/server/internal/publicsite/rules_test.go`, `policy_test.go`

**Interfaces:**
- Produces:
  - `type Mark string` with `MarkPublic Mark = "public"`, `MarkHidden Mark = "hidden"`.
  - `type SiteRules struct { HomeNote string \`json:"homeNote"\`; Rules map[string]Mark \`json:"rules"\` }` — keys are vault-relative POSIX paths of folders or files, no leading/trailing slash.
  - `type Store struct` with `NewStore(path string) *Store`, `(s *Store) Get(vaultRoot string) SiteRules` (never errors: unreadable/corrupt/missing → empty rules), `(s *Store) Put(vaultRoot string, r SiteRules) error` (atomic write, mode 0600; rejects keys that are empty, absolute, contain `..`, or contain `\`, and marks other than public/hidden). File shape: `{"vaults": {"<abs root>": SiteRules}}`.
  - `type Policy struct` built by `NewPolicy(r SiteRules) Policy`; `(p Policy) IsPublic(rel string) bool` (nearest-mark-wins over the path and its ancestors; no mark → false; any segment starting with `.` → false; `..`/absolute/backslash → false); `(p Policy) IsPublicNote(m vault.NoteMeta) bool` = `m.Folder != vault.FolderTrash && strings.HasSuffix(m.Path, ".md") && p.IsPublic(m.Path)`.

- [ ] **Step 1: Failing policy table test** `TestPolicyIsPublic` with rules `{"Blog":public, "Blog/Drafts":hidden, "Blog/Drafts/Ready":public, "Notes/one.md":public}`:
  `Blog/a.md`→true, `Blog/Drafts/x.md`→false, `Blog/Drafts/Ready/y.md`→true, `Notes/one.md`→true, `Notes/two.md`→false, `Other/z.md`→false, `Blog/.secret/a.md`→false, `../Blog/a.md`→false, `/Blog/a.md`→false, `Blog\\a.md`→false, `blog/a.md`→false (case-sensitive).
  `TestPolicyIsPublicNote`: trash-folder meta under `Blog/` → false; `Blog/d.excalidraw` → false.
- [ ] **Step 2: Failing store tests** `TestStoreMissingFileIsEmpty`, `TestStoreCorruptFileIsEmpty` (write `{not json`), `TestStoreRoundTripPerVault` (Put for `/v1`, Get `/v2` → empty), `TestStoreRejectsBadKeys` (`../x`, `/abs`, `a\\b`, `""`, mark `"maybe"` → error, file unchanged).
- [ ] **Step 3:** `go test ./internal/publicsite/` → FAIL.
- [ ] **Step 4:** Implement. Store guards with a mutex; atomic write via temp file + rename in the same dir.
- [ ] **Step 5:** Run → PASS.
- [ ] **Step 6: Commit** `feat(publicsite): rules store and visibility policy`.

### Task 3: Public content helpers (sanitize, asset refs)

**Files:**
- Create: `apps/server/internal/publicsite/content.go`
- Test: `apps/server/internal/publicsite/content_test.go`

**Interfaces:**
- Produces:
  - `SanitizeBody(body string) string` — removes a leading YAML frontmatter block, `<!-- … -->` comments, and `%% … %%` comments (inline and multi-line), leaving fenced code blocks (``` and ~~~) and inline code spans untouched.
  - `FrontmatterDescription(body string) string` — `description:` scalar from leading frontmatter, else `""`.
  - `ExtractAssetRefs(sanitized string) []string` — hrefs from `![[x]]`, `[[x]]` (only when x has a file extension other than `.md`), `![alt](x)`, `[text](x)`, `<img src="x">`; strips `|alias`/`#fragment`/`?query` from wikilink targets and `"title"` from markdown links; skips code; skips scheme URLs (`https:` etc.), `#…`, `//…`. `.excalidraw` targets count **only** from `![[…]]` embeds (a plain link to a drawing is not a ref, so it 404s per spec).
  - `ResolveAssetRef(notePath, href string, assetPaths []string) (string, bool)` — Go port of `resolveAssetVaultRelativePath` (`packages/app-core/src/lib/local-assets.ts:106` onward): URL-decode, note-dir-relative join, then vault-root-relative, then the same basename fallback; returns false when the result escapes the vault, contains a `.`-prefixed segment, or isn't in `assetPaths`.

- [ ] **Step 1: Failing tests:**
  - `TestSanitizeStripsFrontmatterAndComments`: input `"---\ntitle: x\nsecret: y\n---\nHello %%hidden%% world <!-- c --> end\n%%\nmulti\n%%\n"` → contains `"Hello"`, `"world"`, `"end"`; not contains `secret`, `hidden`, `multi`, `<!--`.
  - `TestSanitizeKeepsCodeBlocks`: fenced block containing `%%keep%%` and `<!-- keep -->` and a `---` line → all preserved; inline `` `%%keep2%%` `` preserved.
  - `TestFrontmatterDescription`: `description: Hi there` → `"Hi there"`; none → `""`.
  - `TestExtractAssetRefs`: body with `![[img.png|300]]`, `![a](sub/pic.jpg "t")`, `[pdf](docs/a.pdf)`, `[[Other note]]`, `[web](https://x.y/a.png)`, `<img src="b.gif">`, `![[draw.excalidraw]]`, `[[link.excalidraw]]`, and `` `![[nope.png]]` `` → exactly `img.png`, `sub/pic.jpg`, `docs/a.pdf`, `b.gif`, `draw.excalidraw` (order-insensitive).
  - `TestResolveAssetRef`: assets `["attachments/img.png","Blog/sub/pic.jpg",".private/x.png","Blog/b.gif"]`, note `Blog/post.md`: `img.png`→`attachments/img.png` (basename), `sub/pic.jpg`→`Blog/sub/pic.jpg`, `b.gif`→`Blog/b.gif`, `x.png`→false (dot folder), `../../etc/passwd`→false, `missing.png`→false.
- [ ] **Step 2:** Run → FAIL.
- [ ] **Step 3:** Implement (regex/scanner; no new deps). Mirror the TS basename tie-breaking.
- [ ] **Step 4:** Run → PASS.
- [ ] **Step 5: Commit** `feat(publicsite): body sanitizing and asset reference resolution`.

### Task 4: Admin API + capability

**Files:**
- Create: `apps/server/internal/httpserver/public_site_admin.go`
- Modify: `apps/server/internal/httpserver/server.go` (`Server` gets `publicRules *publicsite.Store`, set in `New` when `cfg.PublicSite`; register routes in `registerProtectedRoutes`; capability)
- Test: `apps/server/internal/httpserver/public_site_admin_test.go`

**Interfaces:**
- Consumes: Task 2 `Store`, `Policy`.
- Produces (protected, under `<base>/api`):
  - `GET /public-site` → `{ "homeNote": string, "rules": {path: mark}, "missingPaths": [string], "homeNotePublic": bool }`. `missingPaths` = rule keys matching no note/asset path and no directory prefix of one.
  - `PUT /public-site` body `{homeNote, rules}` → saves for current vault root, returns the GET shape; 400 on Store validation error.
  - Capability `"supportsPublicSite": cfg.PublicSite` in `capabilities`.
  - Server helper `(s *Server) currentPolicy() publicsite.Policy` (Get for `currentVault().Root()`; empty policy when public site off).

- [ ] **Step 1: Failing tests:** `TestPublicSiteAdminRequiresAuth` (no cookie → 401), `TestPublicSiteAdminRoundTrip` (PUT then GET equal; `missingPaths` lists `Gone`; `homeNotePublic` false when home note not under a public rule), `TestPublicSiteAdminRejectsBadKey` (`../x` → 400), `TestCapabilitiesReportPublicSite` (true when on, false when off), `TestPublicSiteRulesFollowVaultSwitch` (rules saved for vault A; `switchVaultRoot` to B → GET returns empty rules).
- [ ] **Step 2:** Run `go test ./internal/httpserver/ -run PublicSite` → FAIL.
- [ ] **Step 3:** Implement. Store path from `config.PublicSitePath()`; tests inject a temp path (add `publicRulesPath` override on `New` options or set the field directly in tests).
- [ ] **Step 4:** Run → PASS.
- [ ] **Step 5: Commit** `feat(server): public site admin API`.

### Task 5: Public router — site, note, asset, page shell, 404s

**Files:**
- Create: `apps/server/internal/httpserver/public_router.go`, `apps/server/internal/httpserver/public_shell.go`
- Modify: `apps/server/internal/httpserver/server.go` (`Router()`: when `cfg.PublicSite`, `outer.Mount(basePath, inner)` **and** `outer.Mount("/", s.publicRouter())`; `New` gains a `publicStatic fs.FS` param or field)
- Modify: `apps/server/web/embed.go` (add `//go:embed all:dist-public` + `PublicDist() (fs.FS, error)`), `apps/server/cmd/zennotes-server/main.go` (pass it)
- Modify: `docs/how-to/self-host-with-docker.md` (new section: enabling the public site, the three env vars, reverse proxy, the no-auth refusal, rules file location)
- Test: `apps/server/internal/httpserver/public_router_test.go`

**Interfaces:**
- Consumes: Tasks 2–4.
- Produces (all public, no auth, `Cache-Control: no-cache`):
  - `GET /_zn/api/site` → `{ "homeNote": string|null, "notes": [{ "path", "title", "updatedAt", "wikilinks": [string] }] }` — public notes only (`IsPublicNote`); `homeNote` null unless itself public.
  - `GET /_zn/api/note?path=<rel>` → `{ "path", "title", "updatedAt", "body": SanitizeBody(raw) }` or API 404.
  - `GET /_zn/api/asset?note=<rel>&href=<href>` → file bytes iff note public ∧ `href ∈ ExtractAssetRefs(SanitizeBody(body))` ∧ `ResolveAssetRef` ok ∧ resolved path not trash; else API 404. Content-Type by extension, `X-Content-Type-Options: nosniff`, SVG served with `Content-Security-Policy: sandbox`.
  - `GET /_zn/static/*` → files from `PublicDist()` only.
  - `GET /sw.js` → kill-switch worker (`self.addEventListener('install',()=>self.skipWaiting()); self.addEventListener('activate',e=>e.waitUntil(self.registration.unregister().then(()=>self.clients.matchAll()).then(cs=>cs.forEach(c=>c.navigate(c.url)))))`), `Content-Type: text/javascript`.
  - `GET /` and `GET /*` (anything else) → page shell (`public.html` from `PublicDist()`) with 200 when the path maps to a public note (URL path = encoded vault path without `.md`; `/` = home) or a 404 shell otherwise. Unknown `/_zn/api/*` → API 404.
  - `publicNotFoundPage(w)` / `publicNotFoundAPI(w)` — the only 404 writers.
  - `renderShell(note *PublicPage) []byte` in `public_shell.go` — injects into `public.html`'s `<head>`: `<title>`, `meta description`, `og:title`, `og:description`, `og:url`, `og:type=article`, `twitter:card=summary`, `link rel=canonical` (absolute from `PublicURL`, omitted if unset), and into `<div id="zn-seo">` the sanitized body as escaped `<p>` paragraphs (split on blank lines). Description = `FrontmatterDescription(raw)` else first 160 chars of sanitized body, whitespace-collapsed. All values HTML-escaped. 404 shell: title "Not found", no meta.

- [ ] **Step 1: Failing leak-matrix test** `TestPublicLeakMatrix` using a fixture vault: `Blog/a.md` (public, embeds `![[img.png]]`, links `[[Private note]]`, frontmatter `secret: S1`, `%%S2%%`), `Blog/Drafts/d.md` (hidden, body `S3`), `Private note.md` (unruled, body `S4`), trash note under `Blog` (body `S5`), `Blog/.hidden/h.md` (`S6`), `attachments/img.png`, `attachments/private.png`, `Blog/draw.excalidraw`; rules `{Blog: public, Blog/Drafts: hidden}`, home `Blog/a.md`. Assert:
  - every response body from `/_zn/api/site`, `/_zn/api/note?path=Blog/a.md`, `/`, `/Blog/a`, `/sitemap.xml` contains none of `S1..S6`, `Drafts`, `d.md`, `private.png`, `draw.excalidraw`, `.hidden` (note `Private note` appears only as link text inside `Blog/a`'s body — allowed).
  - page 404 bodies+status+headers (excluding `Date`) for `/Blog/Drafts/d`, `/Private%20note`, `/Blog/.hidden/h`, `/does/not/exist`, `/Blog/%2e%2e/Private%20note`, `/blog/a` are identical; same for API 404 on `note?path=` with `Blog/Drafts/d.md`, `Private note.md`, `../x.md`, `Blog/draw.excalidraw`, missing.
  - `asset?note=Blog/a.md&href=img.png` → 200 PNG bytes; `href=private.png` → API 404; `note=Private note.md&href=img.png` → API 404.
- [ ] **Step 2: Failing routing tests:** `TestPublicSiteMovesAdminUnderBasePath` — with public site on: `/api/vault`, `/vault`, `/api/session`, `/watch`, `/index.html`, `/manifest.webmanifest` return the public 404 (never admin JSON/SPA); `/admin/api/healthz` → 200; `/admin/` → admin SPA shell. `TestPublicSiteOffUnchanged` — off: `/api/healthz` 200 at root (existing behaviour). `TestPublicServiceWorkerKillSwitch` — `/sw.js` body contains `unregister`. `TestPublicHomeFallback` — home note hidden → `/` returns 200 shell with no note title in `<title>`. `TestPublicShellSEO` — `/Blog/a` has `<title>` with note title, `og:url` = `https://notes.example.com/Blog/a`, `zn-seo` contains body text, and HTML-escapes a title containing `<script>`.
- [ ] **Step 3:** Run → FAIL.
- [ ] **Step 4:** Implement. Public router uses a fresh chi router with `securityHeadersMiddleware` (CSP as today plus `img-src 'self' data: blob:`), no CORS middleware, no `requireAuth`. Path→note: decode once, append `.md`, exact match against public note paths (no filesystem lookup for misses). Provide a `fstest.MapFS` public bundle in tests.
- [ ] **Step 5:** Run `go test ./...` → PASS.
- [ ] **Step 6: Commit** `feat(server): public site router`.

### Task 6: Public search, sitemap, robots

**Files:**
- Modify: `apps/server/internal/httpserver/public_router.go`
- Test: `apps/server/internal/httpserver/public_router_test.go`

**Interfaces:**
- Produces:
  - `GET /_zn/api/search?q=` → `[{ "path", "title", "lineText" }]` max 50; runs `SearchText` and keeps matches whose note passes `IsPublicNote` **and** whose `lineText` is re-derived from the sanitized body (drop the match if the matching line isn't present after sanitizing). Rate-limited with `newAttemptLimiter(time.Minute, 60)` keyed by `clientAddressKey`; over limit → 429. Empty `q` → `[]`.
  - `GET /sitemap.xml` → `<urlset>` of absolute public note URLs (`PublicURL` + encoded path), `lastmod` from `updatedAt`; home as `/`. Without `PublicURL`: 404.
  - `GET /robots.txt` → `User-agent: *\nDisallow: <basePath>/\nDisallow: /_zn/api/\nSitemap: <PublicURL>/sitemap.xml` (Sitemap line omitted without `PublicURL`).

- [ ] **Step 1: Failing tests** with the Task 5 fixture: `TestPublicSearchOnlyPublic` (`q=S` matches nothing private; `q=Hello` finds `Blog/a.md`); `TestPublicSearchDropsSanitizedLines` (`q=S2` → `[]`); `TestPublicSearchRateLimited` (61st call → 429); `TestSitemapOnlyPublic`; `TestRobotsDisallowsAdmin`.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement. **Step 4:** Run → PASS.
- [ ] **Step 5: Commit** `feat(server): public search, sitemap, robots`.

### Task 7: Public bundle build pipeline

**Files:**
- Create: `apps/web/public.html` (root `<div id="zn-root">`, `<div id="zn-seo"></div>`, script `/src/public/main.tsx`), `apps/web/vite.public.config.ts`, `apps/web/src/public/main.tsx` (renders "ZenNotes" placeholder)
- Modify: `apps/web/package.json` (`build` → also `vite build -c vite.public.config.ts`; same for `build:nocheck`), `tooling/scripts/sync-web-dist.mjs` and `tooling/scripts/prepare-server-web-dist.mjs` (also copy `apps/web/dist-public` → `apps/server/web/dist-public`, same lock), `Dockerfile:45` (also copy `dist-public`), `Makefile` `clean` (remove both new dirs), root/app `.gitignore` if `dist` is listed there
- Test: build commands below

**Interfaces:**
- Produces: `apps/web/dist-public/public.html` + `assets/*`, built with `base: '/_zn/static/'`, same `resolve.alias`/`dedupe` as `vite.config.ts` (import shared pieces instead of copying), `outDir: 'dist-public'`, `rollupOptions.input: public.html`. No service worker, no manifest.

- [ ] **Step 1:** `npm run build --workspace @zennotes/web` → both `apps/web/dist/index.html` and `apps/web/dist-public/public.html` exist; `grep -L "src/main" apps/web/dist-public/assets/*.js` — the public bundle must not import the admin entry (`grep -r "registerProtectedRoutes\|installBridge" apps/web/dist-public` → no matches).
- [ ] **Step 2:** `npm run sync-web --workspace @zennotes/server && cd apps/server && go build ./...` → succeeds; `go test ./...` → PASS.
- [ ] **Step 3: Commit** `build(web): separate public site bundle`.

### Task 8: Public app — tree, note view, links, assets, 404, home

**Files:**
- Create in `apps/web/src/public/`: `api.ts`, `tree.ts`, `links.ts`, `PublicApp.tsx`, `Sidebar.tsx`, `NoteView.tsx`, `NotFound.tsx`, `public.css`
- Test: `apps/web/src/public/tree.test.ts`, `links.test.ts` (vitest; add `test`/`test:run` scripts to `apps/web/package.json` if absent, mirroring app-core's)

**Interfaces:**
- Consumes: Task 5 API shapes; app-core `renderMarkdown` (`@renderer/lib/markdown`), `resolveWikilinkTarget` / `wikilinkHeadingAnchor` (`@renderer/lib/wikilinks`), `expandEmbeds` (`@renderer/lib/transclusion`), `renderDiagrams` (`@renderer/lib/diagram-renderers`), `renderMermaidSvg`, reading-view CSS (`@renderer/styles/*` as `Preview` uses), `LazyExcalidrawPreview` for `[data-excalidraw-embed]` hosts.
- Produces:
  - `api.ts`: `fetchSite(): Promise<PublicSite>`, `fetchNote(path): Promise<PublicNote | null>` (null on 404), `assetUrl(notePath, href): string` (`/_zn/api/asset?note=…&href=…`), `searchNotes(q): Promise<PublicSearchHit[]>`.
  - `tree.ts`: `buildTree(notes: PublicNoteMeta[]): TreeNode[]` — folders from path segments, sorted folders-first then by title; `noteUrl(path): string` (encode each segment, drop `.md`); `pathFromUrl(pathname): string | null`.
  - `links.ts`: `rewriteRenderedLinks(root: HTMLElement, ctx: { notePath: string; notes: PublicNoteMeta[] })` — `a.wikilink[data-wikilink]` → `href = noteUrl(resolved)` (+ `#heading` anchor) or `/_zn/missing` when unresolved, keeping text (plain links to `.excalidraw` files also → `/_zn/missing`); `img`/media/`[data-local-asset-href]` and excalidraw hosts → `assetUrl(notePath, originalHref)`. `markUnresolvedEmbeds(markdown, notes): string` — replaces `![[target]]` that is not an asset (per `classifyLocalAssetHref` being null or `'file'` with no extension) and doesn't resolve to a public note with `<div class="zn-embed-missing">Not found</div>`.
  - `PublicApp`: client router on `history.pushState`; layout = sidebar (tree + search box) + note pane + "Linked from" (via app-core `backlinksForNote` over site notes); `/` → home note or "Nothing here yet"; unresolved → `NotFound`. On mount, clear `#zn-seo`. Theme follows `prefers-color-scheme` using ZenNotes default light/dark tokens.

- [ ] **Step 1: Failing tests:** `buildTree` nests `Blog/a.md`, `Projects/zennotes/x.md` with only those folders; `noteUrl('Blog/My Post.md') === '/Blog/My%20Post'`; `pathFromUrl('/Blog/My%20Post') === 'Blog/My Post.md'`; `pathFromUrl('/') === null`; `markUnresolvedEmbeds('![[Private]] ![[img.png]] ![[Blog/a]]', [Blog/a.md])` keeps `img.png` and `Blog/a`, replaces `Private`; `rewriteRenderedLinks` (jsdom) sets private wikilink href to `/_zn/missing` and keeps text, public one to `/Blog/a`, image src to `/_zn/api/asset?note=Blog%2Fa.md&href=img.png`.
- [ ] **Step 2:** Run `npx vitest run apps/web/src/public` → FAIL.
- [ ] **Step 3:** Implement. Transclusion: `expandEmbeds` with `resolve` against public notes and `loadNote` = `fetchNote(...).body`.
- [ ] **Step 4:** Run tests → PASS; `npm run build --workspace @zennotes/web` → succeeds.
- [ ] **Step 5: Manual check:** `ZENNOTES_PUBLIC_SITE=1 ZENNOTES_AUTH_TOKEN=dev ZENNOTES_PUBLIC_URL=http://127.0.0.1:7878 npm run dev:server` after sync; mark a folder public via `curl -X PUT` on `/admin/api/public-site` with the session; open `/` — tree, note render (math, code, mermaid, callout, image, excalidraw embed), private link → 404 page, browser back/forward.
- [ ] **Step 6: Commit** `feat(web): public site reader`.

### Task 9: Public search and backlinks UI

**Files:**
- Create: `apps/web/src/public/SearchBox.tsx`, `apps/web/src/public/Backlinks.tsx`
- Modify: `apps/web/src/public/PublicApp.tsx`
- Test: `apps/web/src/public/backlinks.test.ts`

**Interfaces:**
- Consumes: `searchNotes`, `backlinksForNote`.
- Produces: `publicBacklinks(notePath, notes): PublicNoteMeta[]` (thin wrapper over `backlinksForNote`, excludes self). Search box debounced 200 ms, shows title + `lineText`, Enter opens first hit; 429 → "Too many searches, try again in a minute".

- [ ] **Step 1: Failing test:** `publicBacklinks('Blog/a.md', [b links [[a]], c links [[Blog/a]], a links [[a]]])` → `[b, c]`.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement. **Step 4:** Run → PASS; build succeeds.
- [ ] **Step 5: Commit** `feat(web): public search and backlinks`.

### Task 10: Admin "Public site" panel

**Files:**
- Create: `packages/shared-domain/src/public-site-rules.ts` (+ `.test.ts`), `packages/app-core/src/components/PublicSiteSettings.tsx` (+ `PublicSiteSettings.test.ts` for pure helpers)
- Modify: `packages/bridge-contract/src/bridge.ts` (capability `supportsPublicSite?: boolean`; optional methods), `packages/bridge-contract/src/ipc.ts:748` area (same capability), `apps/web/src/bridge/http-bridge.ts` (implement methods; derive `supportsPublicSite` from `lastServerCapabilities` like `supportsWorkflows` at `:1374`), `packages/app-core/src/components/SettingsModal.tsx` (new `Section` in the `vault` category, rendered only when `getCapabilities().supportsPublicSite === true`)

**Interfaces:**
- Produces:
  - shared-domain: `type PublicMark = 'public' | 'hidden'`; `type PublicSiteConfig = { homeNote: string; rules: Record<string, PublicMark>; missingPaths: string[]; homeNotePublic: boolean }`; `effectiveVisibility(path: string, rules: Record<string, PublicMark>): { visible: boolean; source: 'explicit' | 'inherited' | 'default' }` — same semantics as Go `Policy.IsPublic` (nearest mark wins, dot segments never visible).
  - bridge: `getPublicSiteConfig?(): Promise<PublicSiteConfig>`, `setPublicSiteConfig?(input: { homeNote: string; rules: Record<string, PublicMark> }): Promise<PublicSiteConfig>` → `GET`/`PUT` `<base>/api/public-site`.
  - UI: tree of vault folders/notes from the store's existing note list (excluding trash and `.excalidraw`), each row with a 3-way control **Public / Hidden / Inherit** and an effective-state indicator distinguishing explicit vs inherited; home-note picker listing only currently visible notes; "Open public site" link (`/`); warnings for `!homeNotePublic` and each `missingPaths` entry with a "Remove rule" action. Saves on change.

- [ ] **Step 1: Failing tests:** `effectiveVisibility` table identical to Task 2's (same inputs → same booleans), plus `source` values (`Blog/a.md` → inherited, `Blog` → explicit, `Other/z.md` → default).
- [ ] **Step 2:** `npx vitest run packages/shared-domain/src/public-site-rules.test.ts` → FAIL. **Step 3:** Implement. **Step 4:** PASS.
- [ ] **Step 5:** Implement bridge + panel; `npm run typecheck` → PASS; `npm run test:run` → PASS.
- [ ] **Step 6: Manual check:** in `/admin`, Settings → Vault → Public site: tick `Blog`, hide `Blog/Drafts`, pick home; reload `/` in a private window — matches.
- [ ] **Step 7: Commit** `feat(app): public site settings panel`.
