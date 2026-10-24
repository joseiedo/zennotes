# Self-Host with Docker

This guide is for running ZenNotes in a browser against a vault stored on your own machine, home server, or remote server.

It uses the current supported self-hosted model:

- browser frontend
- Go server
- host-mounted vault
- Docker as the main deployment path

## What Docker is doing

Docker is not the owner of your notes.

The intended model is:

- you create a vault directory on the host
- Docker mounts that directory into the ZenNotes container
- the server reads and writes files in that mounted host directory
- the browser app talks to the server

So the vault remains a normal folder on the host filesystem.

## Requirements

You need:

- Docker
- Docker Compose
- a host directory for your vault

## 1. Create a host vault

Example:

```bash
mkdir -p "$HOME/Notes/ZenNotesVault"
```

You can also point ZenNotes at an existing vault instead of a new one.

## 2. Start the self-hosted stack

From the repo root:

```bash
CONTENT_ROOT="$HOME/Notes/ZenNotesVault" make up
```

This starts the self-hosted browser version with Docker.

Important details:

- the host vault is mounted into the container
- ZenNotes serves that host directory instead of storing notes in container-only storage
- Docker is the main supported path for browser/self-hosted use

## 3. Open the app

Open:

- [http://localhost:7878](http://localhost:7878)

## 4. Authenticate

Secure self-hosted mode generates a bootstrap auth token and stores it under:

- `data/auth-token`

Read the token:

```bash
cat data/auth-token
```

Paste that token into the browser when ZenNotes asks for it.

After login, the browser uses a session cookie, so you should not need to keep re-entering the token on refresh.

## 5. Connect the vault

If the server does not already have a vault selected, the empty-state screen will show:

- `Connect to server vault`

Click it and choose the mounted vault directory.

If you started with:

```bash
CONTENT_ROOT="$HOME/Notes/ZenNotesVault" make up
```

then the selected server-side vault path should correspond to that mounted directory.

## 6. Confirm that the host owns the files

Create or edit a note in the browser.

Then inspect the host directory directly:

```bash
find "$HOME/Notes/ZenNotesVault" -maxdepth 3 -type f | sort
```

You should see the note files on the host, not hidden away in a container-only filesystem.

## 7. Stop the stack

```bash
make down
```

## Useful commands

Start:

```bash
CONTENT_ROOT="$HOME/Notes/ZenNotesVault" make up
```

Stop:

```bash
make down
```

Logs:

```bash
make logs
```

Rebuild:

```bash
CONTENT_ROOT="$HOME/Notes/ZenNotesVault" make rebuild
```

## Permissions

If the server exits on startup with a permission error like:

```text
vault init: mkdir /workspace/inbox: permission denied
```

…the container can't write to your mounted vault. ZenNotes runs as a **non-root user** inside the container (UID `65532` in the published image), and Docker bind mounts preserve the host's ownership — so the mounted directory has to be writable by the UID the container runs as. A directory you own as your normal user is *not* writable by UID `65532`, which is why it fails even on folders you "have access to".

**With `make up` / Docker Compose:** this is handled for you. The stack runs the container as your own user (`$(id -u):$(id -g)`) and creates the vault and `data` directories as you, so there's nothing to do.

**Running the image directly with `docker run`:** pass `--user` so the container runs as you, and make sure both mounted directories (the vault and `data`) are owned by that user:

```bash
mkdir -p ./vault ./data

docker run --rm \
  --user "$(id -u):$(id -g)" \
  -p 127.0.0.1:7878:7878 \
  -v "$PWD/vault:/workspace" \
  -v "$PWD/data:/data" \
  -e ZENNOTES_AUTH_TOKEN="$(openssl rand -hex 32)" \
  adibhanna/zennotes
```

Alternatively, instead of `--user`, give the image's default UID write access to the host directories:

```bash
sudo chown -R 65532:65532 ./vault ./data
```

Either way works — the only requirement is that the UID the container runs as can write to the mounted host directories.

## Security notes

The current self-hosted model is designed around:

- single-user use first
- private network, reverse proxy, or VPN access
- a host-mounted vault

Important points:

- Docker defaults are intended to be safer than a wide-open dev setup
- the browser app logs in with a bootstrap token and then uses a session cookie
- the server restricts vault browsing based on configured browse roots
- vault notes are written with `0600` and dirs with `0700` by default
- asset uploads default to a 50 MiB cap and note writes to 10 MiB

If you expose ZenNotes beyond your LAN, the recommended model is:

- put it behind a reverse proxy
- terminate TLS there
- treat direct public exposure as unsupported-by-default
- set `ZENNOTES_BEHIND_TLS=1` so cookies get the `Secure` flag and the
  server emits HSTS
- set `ZENNOTES_TRUSTED_PROXIES` (CIDR list) so the server only honours
  `X-Forwarded-*` headers from your reverse proxy

## Useful environment variables

The container reads these on startup. Set them in `docker-compose.yml`
or via the orchestrator of your choice.

- `ZENNOTES_AUTH_TOKEN` — bootstrap token. Required for non-loopback binds.
- `ZENNOTES_AUTH_TOKEN_FILE` — read the token from a file instead of an env
  var (the Docker/Kubernetes `*_FILE` secrets convention), so the value never
  lives in `.env` or `docker-compose.yml`. Details:
  - It is used **only when `ZENNOTES_AUTH_TOKEN` is unset** — a set
    `ZENNOTES_AUTH_TOKEN` always wins.
  - The path must **exist and be readable by the container's user**, and the
    file's contents are **trimmed** of surrounding whitespace/newlines.
  - If the file is missing, unreadable, or empty, the server logs a clear
    `ZENNOTES_AUTH_TOKEN_FILE … could not be read` (or `… is empty`) line and
    then refuses to start on a non-loopback bind — check `docker logs`.
  - A bare `ZENNOTES_AUTH_TOKEN_FILE=${ZENNOTES_AUTH_TOKEN_FILE}` in Compose
    resolves to an **empty** value (and is ignored) unless that variable is set
    on the host — point it at the mounted secret path directly instead:

    ```yaml
    services:
      zennotes:
        image: adibhanna/zennotes
        environment:
          ZENNOTES_AUTH_TOKEN_FILE: /run/secrets/zennotes_auth_token
        secrets:
          - zennotes_auth_token
    secrets:
      zennotes_auth_token:
        file: ./secrets/zennotes_auth_token.txt
    ```
- `ZENNOTES_BEHIND_TLS=1` — declare that a TLS-terminating proxy is in
  front. Enables `Secure` cookies and `Strict-Transport-Security`.
- `ZENNOTES_TRUSTED_PROXIES` — comma-separated CIDR list. Required if
  the proxy is on a different IP than loopback (e.g. on a Docker bridge
  network or a separate host).
- `ZENNOTES_ALLOWED_ORIGINS` — comma-separated origins permitted to use
  the API from a browser or WebView. Misses are logged once per origin,
  and the effective policy is printed in the startup banner (`cors:`).
  Values are matched verbatim, so opaque origins count: `null` and
  `file://` are valid entries, as are custom schemes like
  `capacitor://localhost`. `*` allows any origin, but then the session
  cookie is withheld cross-origin (a wildcard plus credentials would let
  any site a user visits drive their session) — token clients still work.
  The **desktop app does not need this**: it talks to the server from its
  main process, which sends no `Origin`, so CORS never applies to it.
- `ZENNOTES_BROWSE_ROOTS` — directories the server may consider as
  vault candidates. Anything outside is rejected.
- `ZENNOTES_MAX_NOTE_BYTES` / `ZENNOTES_MAX_ASSET_BYTES` — per-request
  byte caps for `/api/notes/write` and `/api/assets/upload`. Defaults
  10 MiB and 50 MiB.
- `ZENNOTES_VAULT_FILE_MODE` / `ZENNOTES_VAULT_DIR_MODE` — octal mode
  for new files / directories. Defaults `0600` and `0700`.
- `ZENNOTES_BASE_PATH` — mount the API and static bundle under a
  subpath instead of the domain root. Use this when deploying behind a
  reverse proxy that routes by path (e.g. `example.com/zennotes/`).
  See [Reverse-proxy with a path prefix](#reverse-proxy-with-a-path-prefix).
- `ZENNOTES_DISABLE_WATCHER=1` — turn off the inotify file watcher. The
  vault is still fully served; only live updates (auto-refresh when files
  change on disk) stop. Set this where inotify is restricted or unstable —
  notably **unprivileged LXC containers**, where inotify on a bind-mount can
  wedge the process and lock the volume (see Common problems below).
- `ZENNOTES_PERSIST_SESSIONS=1` — **opt-in**: keep browser logins across
  restarts. By default the server holds sessions in memory, so restarting the
  container (or the host) invalidates every login and the browser re-prompts for
  the token — even though the token itself is unchanged. With this on, sessions
  are saved to `sessions.json` beside your host config (on the `/data` volume)
  and reloaded on startup, so you stay logged in. It writes session tokens to
  disk (mode `0600`, alongside the auth token that already lives there); leave it
  off if you'd rather sessions never touch disk.

## Reverse-proxy with a path prefix

If you want to host ZenNotes alongside other apps under a single
domain, set `ZENNOTES_BASE_PATH=/zennotes` (any leading-slash path
works). The server then expects every request to start with that
prefix; the bundled web client reads the prefix from a `<meta>` tag
the server injects into the SPA shell, so API + WebSocket calls
target `/zennotes/api/...` and `/zennotes/api/watch`.

Example Nginx fragment that forwards `/zennotes/` to the container:

```nginx
location /zennotes/ {
    proxy_pass         http://127.0.0.1:7878/zennotes/;
    proxy_http_version 1.1;
    proxy_set_header   Host              $host;
    proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header   X-Forwarded-Proto $scheme;
    proxy_set_header   Upgrade           $http_upgrade;
    proxy_set_header   Connection        "upgrade";
}
```

Notes:

- Keep the trailing slash on both sides of `proxy_pass` so the prefix
  is preserved, not stripped.
- The path is always rooted (must start with `/`); a trailing slash
  is ignored. `ZENNOTES_BASE_PATH=zennotes/` is treated the same as
  `/zennotes`.
- An empty `ZENNOTES_BASE_PATH` (or `/`) means "serve at root" — the
  default behaviour for plain Docker installs.

For a deeper walkthrough of the security choices and a full env-var
list, see:

- [Secure Self-Hosting](./secure-self-hosting.md)
- [At-Rest Encryption](./at-rest-encryption.md)
- [Security Reference](../reference/security-reference.md)

## Publish a public read-only site

ZenNotes can also serve a read-only website of the notes you choose, at the
domain root, while the full app moves under an admin path. It is off by
default; nothing changes unless you turn it on.

Environment variables:

- `ZENNOTES_PUBLIC_SITE=1` — turn the public site on. The site is served at
  `/` and the admin app (UI, API, WebSocket) moves under `ZENNOTES_BASE_PATH`,
  which defaults to `/admin` when the public site is on. An explicit non-root
  `ZENNOTES_BASE_PATH` is respected; the admin app is never served at `/`.
- `ZENNOTES_PUBLIC_URL` — the public origin, e.g. `https://notes.example.com`
  (no trailing slash). Used for canonical links, social-preview (`og:url`)
  tags and `/sitemap.xml`. Without it those tags are omitted, `/sitemap.xml`
  returns 404 and `robots.txt` names no sitemap.
- `ZENNOTES_ADMIN_PROXY_AUTH=1` — declare that your reverse proxy
  authenticates the admin path. Only needed when you run without
  `ZENNOTES_AUTH_TOKEN`.
- `ZENNOTES_TRUSTED_PROXIES` — set this to your proxy's address (or CIDR)
  whenever ZenNotes runs behind a reverse proxy. Public search is rate-limited
  per client; without it every visitor looks like the proxy and shares one
  limit.

The admin path is used only while the public site is on: the `/admin` default
is never written to `server.json`, so turning the public site off brings the
admin app back to `/` (an explicit `ZENNOTES_BASE_PATH` is kept).

**No-auth refusal.** With the public site on, the server refuses to start
unless the admin app is protected: set `ZENNOTES_AUTH_TOKEN` (or
`ZENNOTES_AUTH_TOKEN_FILE`), or set `ZENNOTES_ADMIN_PROXY_AUTH=1` when a proxy
in front already authenticates `/admin`. This keeps a public deployment from
exposing an open admin API.

**What is public.** Nothing, until you mark it: choose public and hidden
folders/notes and an optional home note in the admin app. The rules are stored
in `public-site.json` next to the host config (`server.json`, i.e. on the
`/data` volume in Docker), keyed by the vault's absolute path. A missing or
unreadable rules file means nothing is public. Trashed notes, dot-folders,
inline comments (`%%…%%` and `<!-- … -->` in a note's text) and frontmatter
are never published, and comment sidecar files (the threads you add in the
app) are never served at all; Excalidraw drawings and attachments are only
served when a public note embeds them. Private, missing and malformed URLs all
return the same 404. A note whose URL would collide with a fixed route (the
admin path, `/sitemap.xml`, `/robots.txt`, `/sw.js`, `/_zn/…`) is left out of
the site.

**Renames.** Rules are keyed by path. Renaming, moving or duplicating a note,
folder or attachment *in ZenNotes* carries its Hidden marks along (a duplicate
of a hidden item is hidden too), so a hidden draft inside a public folder stays
hidden under its new name. Public marks are not carried: a renamed public
folder becomes private until you mark it again. Renames made **outside**
ZenNotes (Obsidian, a sync tool, the shell) can't be tracked — after them,
check the public site panel for "matches nothing in the vault" warnings and
re-mark what moved, especially hidden items inside public folders.

The public site reserves `/_zn/` (`/_zn/api/*` and `/_zn/static/*`) and
serves a `/sw.js` that unregisters any service worker an earlier root-mounted
install left behind, so old browser tabs switch to the public site cleanly.

**Proxy-auth mode.** With `ZENNOTES_ADMIN_PROXY_AUTH=1` the proxy is the
only thing protecting the admin app, so:

- the ZenNotes port must not be reachable except through the proxy — bind it
  to `127.0.0.1` (`ZENNOTES_BIND=127.0.0.1:7878`) or don't publish it from the
  container;
- the proxy must protect both `/admin` and everything under `/admin/` (see the
  two `location` blocks below);
- because browsers send proxy credentials automatically, the server refuses
  (403) state-changing requests and WebSocket upgrades under the admin path
  that come from another site (a foreign `Origin`, or `Sec-Fetch-Site:
  cross-site`). A `*` in `ZENNOTES_ALLOWED_ORIGINS` does not lift this; list
  extra origins explicitly. Token mode is unaffected.

Example Nginx config, protecting the admin path at the proxy (when you rely on
`ZENNOTES_ADMIN_PROXY_AUTH=1`; with a token you can drop `auth_basic`). Set
`ZENNOTES_TRUSTED_PROXIES=127.0.0.1` so rate limits see real clients:

```nginx
server {
    server_name notes.example.com;

    # The bare admin path (redirects to /admin/) — protect it too.
    location = /admin {
        auth_basic           "ZenNotes admin";
        auth_basic_user_file /etc/nginx/zennotes.htpasswd;
        proxy_pass         http://127.0.0.1:7878;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
    }

    location /admin/ {
        auth_basic           "ZenNotes admin";
        auth_basic_user_file /etc/nginx/zennotes.htpasswd;
        proxy_pass         http://127.0.0.1:7878;
        proxy_http_version 1.1;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
        proxy_set_header   Upgrade           $http_upgrade;
        proxy_set_header   Connection        "upgrade";
    }

    location / {
        proxy_pass       http://127.0.0.1:7878;
        proxy_set_header Host              $host;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Open the admin app at `https://notes.example.com/admin/`.

## Common problems

### I have to re-enter the token after every restart

By default the server keeps browser sessions **in memory**, so restarting the container (or the host) forgets every login — your browser's saved cookie is no longer recognized and you're re-prompted for the token, even though the token itself hasn't changed.

Set **`ZENNOTES_PERSIST_SESSIONS=1`** to keep sessions across restarts: they're saved to `sessions.json` on the `/data` volume (mode `0600`) and reloaded on startup. It's opt-in — see the environment-variables list above for the on-disk trade-off; leave it off if you'd rather sessions never touch disk.

### The browser opens, but `Connect to server vault` does nothing

In the normal self-hosted path, Docker is the primary way to run browser plus server together.

If you are instead running the web dev server directly, you need both:

```bash
npm run dev:web
npm run dev:server
```

Without the Go server, the browser UI has nothing to call for `/api/*`.

### The vault path looks wrong inside Docker

That usually means you are looking at the wrong path layer.

The important rule is:

- the host path is the source of truth for your files
- the app is serving that mounted directory

If you create a note and the file appears in the host vault, the setup is working as intended.

### The vault directory looks empty, but the app shows notes

Check the vault model. By default, ZenNotes may still place primary notes in `inbox/`.

So your notes may be under:

- `<vault>/inbox/`

not directly in the vault root.

If you want a flatter layout, change:

- `Settings -> Vault -> Primary notes location -> Vault root`

### The container hangs and won't stop (unprivileged LXC)

If the web page loads but the container ignores `docker stop`/`docker kill`
(and even `kill -9`), and the bind-mounted volume on the host is locked, the
culprit is almost always the inotify file watcher on a restricted host —
typically an **unprivileged LXC container**, where inotify on a bind-mounted
directory can put the process into an unkillable state.

Run with the watcher off:

- `ZENNOTES_DISABLE_WATCHER=1`

The vault is still fully served; you only lose live auto-refresh when files
change on disk (reload the page to pick up external edits). On startup the
server now also logs a warning instead of failing silently if it can only
watch part of the vault.

## Related docs

- [Connect Desktop to a Remote ZenNotes Server](./connect-desktop-to-remote-server.md)
- [Vault and Folder Model](../reference/vault-and-folder-model.md)
- [How ZenNotes Works](../explanation/how-zennotes-works.md)
