# ZenNotes (self-hosted)

<p align="center">
  <img src="apps/web/public/icon-512.png" alt="ZenNotes icon" width="128">
</p>

A self-hosted fork of [ZenNotes](https://github.com/ZenNotes/zennotes): a keyboard-first Markdown notes app that keeps your notes as plain `.md` files on disk. It runs as a browser app backed by a Go server, and can also publish chosen folders as a read-only public site.

Features: Vim-friendly editing, edit/split/preview modes, tasks, tags, archive/trash, daily and weekly notes, KaTeX math, Mermaid and other diagrams, Excalidraw drawings, CSV databases, workflows, and vault-wide search.

## Run it with Docker

Prebuilt images are published to GHCR on every push to `main`:

```bash
docker run -d --name zennotes \
  -p 127.0.0.1:7878:7878 \
  -v "$PWD/vault:/workspace" -v "$PWD/data:/data" \
  -e ZENNOTES_BIND=0.0.0.0:7878 \
  -e ZENNOTES_CONFIG_PATH=/data/server.json \
  -e ZENNOTES_DEFAULT_VAULT_PATH=/workspace \
  -e ZENNOTES_BROWSE_ROOTS=/workspace \
  -e ZENNOTES_AUTH_TOKEN="$(openssl rand -hex 32)" \
  ghcr.io/joseiedo/zennotes:main
```

Or build and run locally with `make up` (generates an auth token in `./data/auth-token`, mounts `./vault`). Then open http://localhost:7878 and sign in with the token.

The container runs read-only, without capabilities, and requires an auth token unless you explicitly set `ZENNOTES_ALLOW_INSECURE_NOAUTH=1`. Keep it behind a reverse proxy or VPN.

## Public site

Set `ZENNOTES_PUBLIC_SITE=1` to serve a read-only site of the notes you choose at `/`, Obsidian Publish style. The full app moves to `/admin`, where **Settings → Vault → Public site** lets you mark folders and notes Public / Hidden / Inherit and pick a home note.

| Variable | Purpose |
| --- | --- |
| `ZENNOTES_PUBLIC_SITE=1` | Enable the public site; admin moves under `ZENNOTES_BASE_PATH` (default `/admin`) |
| `ZENNOTES_PUBLIC_URL` | Public origin, e.g. `https://notes.example.com`, for canonical/OG links and `sitemap.xml` |
| `ZENNOTES_TRUSTED_PROXIES` | Your reverse proxy's IP/CIDR, so per-visitor rate limits work |
| `ZENNOTES_ADMIN_PROXY_AUTH=1` | Only when running without a token and your proxy authenticates `/admin` |

Nothing is public until you mark it. Frontmatter, `%%…%%` and `<!-- -->` comments, trash and dot-folders are never published; private and missing pages return the same 404. Renames made outside ZenNotes aren't tracked — check the panel's warnings afterwards.

## Develop

Requires Node.js 22+, npm and Go 1.25+.

```bash
npm ci
npm run dev          # Go server + Vite web client together (same as `make dev`)
npm run build:prod   # typecheck, tests, web bundles and the server binary
```

Code layout and where things belong: [docs/architecture.md](docs/architecture.md).

## License

MIT
