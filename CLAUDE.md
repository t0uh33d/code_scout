# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Code Scout is a self-hosted logging and network inspection tool for Flutter apps. This repository is the dashboard: it receives batched uploads from the SDK (gzipped tar), stores them, and serves a web UI built with Templ + HTMX + Tailwind CSS. It also streams live sessions from a paired device.

The Flutter SDK lives in its own repository, [code_scout_flutter](https://github.com/getcodescout/code_scout_flutter).

## Tech Stack

- **Go 1.24** with Gorilla Mux (routing), GORM (ORM), Logrus (logging)
- **Postgres 16** — boot config via TOML at `/etc/code-scout.conf`; everything else is a database-backed instance setting edited in the UI
- **Templ** — HTML templating (`.templ` files generate `_templ.go` files)
- **Tailwind CSS 3.4** + **HTMX 2.0** for the frontend
- **gorilla/websocket** for the live device socket; watchers use SSE
- **air** for hot-reload during development

## Build & Development Commands

```bash
make dev-setup    # Write /etc/code-scout.conf, create the Postgres role and database
make run          # Start dev server (templ watch + air hot-reload) on :24275
make build        # Build Linux/AMD64 binary to ./bin/code_scout
make test-all     # Full Go suite including integration tests, against a throwaway database
make test-e2e     # Playwright against a real server on :24283
make test-e2e-headed  # The same, in a window you can watch
make test-sdk-e2e # The real Flutter SDK against a real server on :24284
make tailwind     # Watch and rebuild Tailwind CSS
```

`make templ` is a **watch** command and will not return. To regenerate once:
```bash
TEMPL_EXPERIMENT=rawgo templ generate
```

Run a single test:
```bash
go test ./path/to/package -run TestName
```

Default dev server port: **24275**, templ proxy port: **9089**.

## Architecture (Hexagonal)

```
main.go                      → Entry point: wires dependencies, starts server
app/                         → App metadata (version constant)
conf/                        → TOML config loader (/etc/code-scout.conf)

internal/domain/             → Domain entities (Project, Log, User) & error codes/messages
internal/ports/              → Interfaces: repositories + services (contracts)
internal/services/           → Business logic (ProjectService, LogService, AuthService)
internal/adapters/db/        → GORM persistence: connection, models, mappers, repos

server/                      → HTTP layer
  server.go                  → Server struct, Run(), graceful shutdown
  routes.go                  → Route registration + middleware chain
  handlers/                  → HTTP handlers (project, log, view, auth, response helpers)
  middleware/                → Auth, CORS, logging, recovery, pagination, session

pkg/cslog/                   → Custom logging framework (Logger, RequestLog, hooks)
pkg/utils/                   → Shared utilities (GormBase, errors, HTTP helpers, pagination)

view/                        → Templ templates (.templ) + static assets (CSS, JS, images)
jobs/                        → Cron scheduler (robfig/cron)
```

**Request flow:** HTTP request → middleware chain (logger → close-conn → CORS → JSON content type → auth) → `server/handlers/` → `internal/services/` → `internal/adapters/db/` → Postgres

`internal/live/` is the exception: live sessions are in-memory only and never touch the database.

**The live socket is request/reply as well as fan-out.** `Hub.Publish` sends one device's events to
many watchers; `Hub.Ask` sends one dashboard's question to the device and matches the answer back by
request id. That second direction is what the database browser rides on. Three orderings are
load-bearing and each has a test: the pending entry is registered *before* the frame is written, the
socket write happens *outside* the hub lock, and `endLocked` releases every waiter rather than
leaving them to time out.

**Every write to a device socket goes through `deviceConn`.** gorilla/websocket panics with
`concurrent write to websocket connection` on a second concurrent writer — it does not silently
interleave, whatever you may have read. The mutex is what keeps the ping ticker and a dashboard
query from colliding.

## Key Patterns

- **Hexagonal architecture**: Domain entities in `internal/domain/`, interfaces in `internal/ports/`, implementations in `internal/services/` and `internal/adapters/db/`.
- **Dependency injection**: No global DB variable. `*gorm.DB` is passed explicitly from `main.go` through constructors. Repositories and services accept interfaces.
- **Domain/GORM separation**: Domain entities have no ORM tags. GORM models in `internal/adapters/db/models.go` with mappers for conversion.
- **GormBase**: All DB models embed `pkg/utils.GormBase` which provides UUID primary keys (not auto-increment) and soft deletes.
- **API authentication**: Protected routes require `X-Project-ID` and `X-Project-Secret` headers validated against the `project_secrets` table.
- **Templ workflow**: Edit `.templ` files, never edit generated `_templ.go` files directly. Running `make run` handles regeneration automatically. Templ uses `TEMPL_EXPERIMENT=rawgo` flag.
- **Request-scoped logging**: `cslog.NewRequestCtrl(r)` creates a `RequestLog` from an HTTP request with `request_id` and `user_id` fields. Services/handlers embed `cslog.BaseCtrl` via `RequestCtrl` interface to carry logging context. Use `cslog.L(ctx)` to retrieve the logger from context.
- **Build metadata**: Binary embeds build time, git branch, commit hash, and dirty file status via ldflags.

## API Routes

- **Public**: `GET /login`, `GET /healthz`, `GET /static/*`
- **Auth API** (no session needed): `POST /api/auth/submit` (login or register), `POST /api/auth/logout`
- **Protected web pages** (require `cs_session` cookie): `GET /`, `/settings`, `/members`, and everything under `/project/{id}`
- **SDK routes** (`/api/*`, require `X-Project-ID` + `X-Project-Secret` headers): `POST /api/logs/dump`, `GET /api/validate`, `GET /api/live/socket` (WebSocket upgrade)

The live session's **database browser** hangs off `/project/{id}/live/{sid}/db`. Reading (`/db`,
`/db/rows`, `/db/cell`) needs project read, the same bar as watching a stream. Writing
(`POST /db/cell`) sits on the **manage** subrouter: it reaches into somebody's phone and changes
what is stored there, which belongs with rotating a secret rather than with reading logs. The app
must also have registered that database `writable`, so it is the second of two gates.

**Behind a reverse proxy**, the live features need `Upgrade`/`Connection` forwarded and
`proxy_buffering off`. A default nginx config breaks both with nothing in any log to say so — see
the README's Configuration section.

Everything project-scoped hangs off a `/project/{id}` subrouter behind `RequireProjectAccess`, so a handler cannot forget the check. A project the caller cannot see answers **404, never 403**.

## Database

Ten tables, auto-migrated on startup: `projects`, `project_secrets`, `project_members`, `project_favorites`, `users`, `user_sessions`, `instance_settings`, `project_usage_daily`, `sessions`, `logs`.

- `users` — `email` (unique, lower-cased before storing), `name`, `password_hash` (bcrypt), `role`, `must_change_password`. Email is the login identifier; there is no `username`.
- `user_sessions` — `user_id`, `token`, `expires_at` (30 days)
- `sessions` — one row per app launch, keyed on the client's own session id
- `instance_settings` — a single row: timezone, retention, upload cap, daily log cap. Read live, so a change applies without a restart.

Live sessions are deliberately **not** in this list. They exist only in memory in `internal/live/`.

## Design System
Always read `DESIGN.md` before making any visual or UI decisions.
All font choices, colors, spacing, and aesthetic direction are defined there.
Do not deviate without explicit user approval.
In QA mode, flag any code that doesn't match DESIGN.md.
