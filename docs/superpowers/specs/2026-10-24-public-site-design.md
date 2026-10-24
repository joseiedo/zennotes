# Public site for self-hosted ZenNotes — design

Date: 2026-10-24
Status: draft, awaiting approval

## Intent

The self-hosted ZenNotes server should also act as a public, read-only website
for a chosen part of the vault, in the style of Obsidian Publish. The owner runs it
at their own domain (e.g. `notes.joseiedo.com`). Anyone on the internet can browse
the public notes there, while the full app stays private behind the existing login.

Today the self-hosted build has no publishing: the cloud "Publish note" feature is
disabled in the web bridge (`supportsCloudSync: false`) and stubbed with
`notImplemented`. This feature is separate from cloud publishing and does not touch it.

**Overriding rule: nothing private leaks.** Every decision below is judged by
whether it can expose a private note, its title, its contents, its existence or a
private file. When in doubt, show less.

## Goals

1. Admin app moves to `/admin`. Every existing route (SPA, `/api/*`, websocket,
   static assets, manifest, the legacy root-level API) is served only under
   `/admin`.
2. Public site at `/`: a sidebar tree of public notes, the note rendered like
   ZenNotes' reading view, a home note, search, backlinks, and link previews/SEO.
3. In the admin app, a tree selector over the vault to choose what's public:
   tick folders (everything inside them is included, current and future) and hide
   specific subfolders or notes inside a ticked folder.
4. Live: a saved edit is visible to visitors on their next page load. There is no
   publish step and no snapshots.

## Non-goals

- Cloud publishing, desktop app, mobile app. This is the self-hosted Go server
  plus the web build only.
- Editing, comments, accounts, analytics or visitor logins on the public site.
- Live push updates to open visitor tabs (no websocket on the public side).
- Snapshot/draft workflow, per-note publish buttons and frontmatter `publish:` flags.
- Custom public themes. The public site uses the reading-view look.
- Multiple sites per vault, or a public site served on a separate domain from admin.

## Anti-goals (failure even if it "works")

- Any response outside `/admin` that reveals a private note's body, title, excerpt,
  tags, path, existence, comments, or a private file's bytes or name.
- A 404 for a private note that differs in any way (status, body, headers,
  redirect) from a 404 for a path that never existed.
- Shipping the admin bundle, editor code or admin API surface to public visitors.
- Fail-open behaviour: missing/corrupt config, a renamed folder, or a server error
  must never make *more* content public.

## Decisions

### Routing

- Reuse the existing `ZENNOTES_BASE_PATH` mechanism
  (`apps/server/internal/httpserver/server.go` `Router()`, `serveIndexHTML` hint
  injection; web reads `window.__ZN_BASE_PATH__`). With the public site enabled the
  admin base path is `/admin`. The inner router (API, legacy root API, SPA) is
  mounted only there.
- A **separate public router** is mounted at `/` for everything outside `/admin`.
  It shares no handlers with the admin router and never reads the session cookie.
  It serves only:
  - the public page shell for `/` and note URLs,
  - the public bundle's static files,
  - a small public JSON API (tree, note, asset, search, backlinks),
  - `sitemap.xml` and `robots.txt` (the latter disallows `/admin`).
- Enabling: an opt-in server setting (env, e.g. `ZENNOTES_PUBLIC_SITE=1`). When it
  is on, the base path must be non-root and defaults to `/admin` (an explicitly
  configured non-root `ZENNOTES_BASE_PATH` is respected, *my call*); the server
  refuses to start if it would be `/`. When it is off, the server behaves exactly
  as today.
- **No-auth guard (owner decision):** the server refuses to start with the public
  site enabled while admin auth is off (`ZENNOTES_ALLOW_INSECURE_NOAUTH`), since
  that would expose the whole admin app at `/admin` on the internet. The only way
  past it is an explicit second setting that says `/admin` is protected by an
  upstream proxy login (e.g. Authelia). Its name is the builder's call, and the
  docs must spell out the risk. The startup error message names both settings.
- **Known issue to fix:** the session cookie is set with a hard-coded
  `Path: "/api"` (`security.go` `sessionCookie`). Under a base path the browser
  calls `/admin/api/...`, so the cookie may never be sent back. Set the cookie
  path to `<basePath>/api` and add a base-path login test.

### Public URLs

- Note URL = vault-relative path without `.md`, each segment URL-encoded, e.g.
  `/Blog/My%20Post`. *(builder's call: a slug scheme is fine if stable and
  collision-free, as long as private paths are never consulted to build it.)*
- Public API and bundle live under a reserved prefix (e.g. `/_zn/...`) that can't
  collide with a note path. Notes whose path starts with that prefix are simply
  unreachable.

### What is public (the rules)

- Rules are stored as a map of vault path → `public` | `hidden`. A node with no
  rule inherits from its nearest ancestor with one. No rule anywhere means
  **private**. The nearest mark wins, so a ticked folder inside a hidden folder is
  public again.
- Always private, regardless of rules: the trash folder, dot-folders/files (incl.
  ZenNotes' own metadata dirs), comment sidecars, and anything outside the vault
  root.
- Rules live in the **server's data dir** next to its config (e.g.
  `public-site.json`), not in the vault: `{ homeNote, rules }`. A missing or
  unreadable file means nothing is public.
- Rename/move: rules are keyed by path, so a renamed public folder becomes private
  (fail-closed). The admin panel lists rules whose path no longer exists so the
  owner can fix or remove them. *(builder may additionally rewrite rules on renames
  done through the app. Optional.)*
- The vault can be switched at runtime (`switchVaultRoot`). Rules are per vault
  root. Switching to a vault with no rules means nothing is public.
- One central check (`isPublic(path)`) is the only way any public handler decides
  visibility. Everything else calls it.

### Assets (images, attachments, Excalidraw files)

- A file is served publicly **only while at least one public note embeds or links
  to it**. Its own folder location doesn't matter. Requests for any other file get
  the standard 404.
- **Excalidraw drawings are assets only (owner decision).** Although the admin app
  lists `.excalidraw` files as notes, the public site never shows them as pages or
  tree entries, even inside a ticked folder, and they're not in the sitemap or
  search. They appear only as embedded previews inside public notes, under the
  asset rule above. A plain link to a drawing from a public note leads to the
  standard 404. No standalone drawing viewer ships in the public bundle.

### Rendering and content

- The public site is a **second entry point in the web build** (its own HTML +
  bundle). It reuses app-core's `renderMarkdown()` (`packages/app-core/src/lib/markdown.ts`),
  the reading-view CSS/theme and the post-render enhancers (KaTeX, highlighting,
  Mermaid, Excalidraw previews, callouts, tables, task lists) where they work
  without the admin bridge. It talks only to the public API, never to `window.zen`
  admin methods.
- The server **strips frontmatter, HTML comments and `%%…%%` comments** from note
  bodies before sending them publicly, because these never show in the reading view
  but would otherwise reach the browser.
- Wikilinks and embeds: the server returns, with each note, a resolution map for its
  links: target → public URL, or "not found". The map is computed only against
  public notes.
  - Link to public note → normal link.
  - Link to private/hidden/missing note → link kept with its text, pointing at a URL
    that returns the standard 404.
  - `![[note]]` transclusion → rendered only if the target is public; otherwise a
    generic "not found" block with no title or excerpt.

### Home page

- The owner picks a home note in the admin panel (only public notes are
  selectable). `/` renders it.
- If the home note is unset or not public (hidden later, renamed, deleted), `/`
  shows the tree and a plain "nothing here yet" page. The admin panel shows a
  warning.

### Sidebar tree

- Mirrors the real vault folder structure, pruned to public content. Ancestor
  folders of a public node appear (by name) only so the public node has a place in
  the tree. Their private siblings never appear.
- Note titles shown come only from public notes.

### Search

- Server-side, over public notes only (filter vault search results through
  `isPublic`, or search a public-only set). Snippets come only from public notes.
  No counts or hints about hidden results. Rate-limit the endpoint.

### Backlinks

- "Linked from" under each note, built only from public notes' wikilinks. Private
  linkers are never listed or counted.

### Link previews / SEO

- For each public note URL, the server sends a page shell already containing:
  `<title>`, meta description (frontmatter `description` if set, else the start of
  the stripped body), Open Graph/Twitter tags, a canonical URL, and the note's text
  rendered as basic HTML so crawlers see real content. The client render then takes
  over. *(builder's call: which Go markdown renderer, and whether an OG image is
  picked from the note's first public image.)*
- `sitemap.xml` lists only public notes. `robots.txt` disallows `/admin`.
- Private and unknown URLs get the standard 404 shell: same status, body and
  headers.
- The external base URL for canonical/OG/sitemap links comes from config (e.g.
  `ZENNOTES_PUBLIC_URL=https://notes.joseiedo.com`).

### Admin: "Public site" panel

- In the admin app (Settings, unless a better home is obvious), shown only when
  the server reports the public site enabled (a server-driven capability, like
  `supportsWorkflows`).
- A vault tree with a per-node control showing and setting **Public / Hidden /
  Inherit**. Inherited state is visibly distinct from explicit state, so the
  effective visibility of every node is obvious at a glance.
- Home note picker, a link to open the public site, and warnings: home note not
  public, rules pointing at missing paths.
- Backed by protected admin API endpoints under `/admin/api/...` (read/write the
  rules file).

### Freshness

- Live. Public responses reflect the vault at request time, using the vault's
  existing caches and invalidation. Public responses must not be cached long by
  browsers or proxies (`no-cache` or a short max-age), so hiding a note takes
  effect promptly.

### Security headers

- Public responses get the existing security headers middleware, with CSP adjusted
  as the public bundle needs. No CORS for the public API. Public handlers never
  set or read the session cookie.

## Testing expectations

Leak-safety is the core and needs tests, not just review:

- A fixture vault with public, hidden, nested re-public, unruled, trash, dot-folder
  and asset cases. For every public endpoint and page route, assert that private
  items produce a 404 byte-identical to a nonexistent path, and that no response
  body contains any private title, path segment, excerpt or private asset name
  (except link text written in a public note's own body).
- With the public site enabled, nothing outside `/admin` serves admin API, SPA or
  assets. Legacy root API paths return the public 404.
- Missing/corrupt rules file → nothing public.
- Login works under `/admin` (cookie path fix).
- Startup refuses public site + no-auth unless the upstream-proxy setting is set.
- Excalidraw files in a ticked folder: absent from tree/sitemap/search; served
  only when embedded by a public note.

## Left to the builder

- Exact URL encoding/slug scheme (within the constraints above).
- Go markdown renderer for the SEO body; OG image choice.
- How the public bundle is split from the admin bundle in Vite, and which
  reading-view enhancers it can reuse as-is versus via a thin public adapter.
- Exact public API shapes and the reserved prefix name.
- Search implementation (filter existing `SearchText` vs. a separate index).
- Admin panel placement and visual details, within the behaviour above.
- Optional: rewriting rules when folders are renamed through the app.
