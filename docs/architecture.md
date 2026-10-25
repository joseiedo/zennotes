# Architecture

This document describes the repo layout and what each major app/package is responsible for.

ZenNotes runs as a self-hosted web app: a browser frontend (`apps/web`) backed by a Go server (`apps/server`). The product UI lives in shared packages so the shells stay thin.

## Top-level layout

```text
apps/
  web/       Vite shell, HTTP bridge, and the public-site reader bundle
  server/    Go server: API, auth, vault access, public site, embedded web bundles
packages/
  app-core/        Shared React application and renderer logic
  bridge-contract/ Typed runtime contract between UI and host
  shared-domain/   Shared types and note/task/view models
  shared-ui/       Reusable UI primitives
tooling/
  scripts/         Build, dev-stack and dist-sync helpers
docs/
```

## apps/web

`apps/web` is the browser frontend shell.

Responsibilities:

- Vite development/build pipeline
- browser bootstrapping
- HTTP bridge that implements the `window.zen` API shape for the shared UI
- the public-site reader (`public.html`, `src/public/`), built separately into `dist-public/` so no admin code ships to visitors

Important point:

The web app does not reimplement the product UI. It mounts the shared UI from `packages/app-core`.

Important scripts:

- `npm run dev:web`
- `npm run build --workspace @zennotes/web` (builds both the admin and the public bundle)

## apps/server

`apps/server` is the Go backend.

Responsibilities:

- HTTP API
- WebSocket watch stream
- vault access on the server host
- directory browsing and vault selection
- auth/session flow
- security headers and CORS/origin checks
- serving the embedded web bundles
- the opt-in public read-only site (`ZENNOTES_PUBLIC_SITE=1`): admin under the base path, public router at `/`

Important scripts:

- `npm run dev:server`
- `npm run build --workspace @zennotes/server`

## packages/app-core

`packages/app-core` is the source of truth for user-facing product behavior.

Responsibilities:

- React app shell
- Zustand store
- editor panes
- preview
- command palette
- sidebar
- settings modal (including the Public site panel)
- tags/tasks/archive/trash views
- quick notes
- folder icons and sidebar customization

## packages/bridge-contract

`packages/bridge-contract` defines the runtime contract between the shared UI and the host.

It describes:

- app info and capability flags for unsupported platform features
- note, folder, asset and search operations
- session operations
- watchers and events

The UI depends on this interface instead of depending directly on raw fetch calls.

## packages/shared-domain

`packages/shared-domain` contains shared types and models that are not renderer-specific.

Examples:

- tasks
- workflows
- public-site visibility rules (kept in step with the Go policy)
- shared domain types used across the UI and server contract

## packages/shared-ui

`packages/shared-ui` is intentionally small today.

It exists as the place for UI primitives that are reusable without dragging in the whole app.

## tooling/scripts

`tooling/scripts` contains repo-level helpers used by workspaces and CI.

Examples:

- server build helpers
- web stack orchestration
- dist syncing/preparation scripts (copying `apps/web/dist` and `dist-public` into the server's embed directory)
- Windows-safe Go/Node wrapper scripts

## What should go where?

As a rule:

- shared user-facing behavior -> `packages/app-core`
- runtime contract -> `packages/bridge-contract`
- browser-only bootstrapping and the public reader -> `apps/web`
- server-side vault/network/security behavior -> `apps/server`
