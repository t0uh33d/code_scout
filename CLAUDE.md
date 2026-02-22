# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Code Scout is a centralized logging platform that collects, stores, and visualizes application logs from multiple projects. It provides a REST API for log ingestion (TAR.GZ format) and a web UI built with Templ + HTMX + Tailwind CSS.

## Tech Stack

- **Go 1.23** with Gorilla Mux (routing), GORM (ORM), Logrus (logging)
- **MySQL 5.7+** — configured via TOML at `/etc/code-scout.conf`
- **Templ** — HTML templating (`.templ` files generate `_templ.go` files)
- **Tailwind CSS 3.4** + **HTMX 2.0** for the frontend
- **air** for hot-reload during development

## Build & Development Commands

```bash
make run          # Start dev server (templ watch + air hot-reload)
make build        # Build Linux/AMD64 binary to ./bin/code_scout
make test         # Run all tests (unit + integration)
make templ        # Watch and regenerate templ files
make tailwind     # Watch and rebuild Tailwind CSS
make up host=X    # Build + deploy to remote host via SCP
```

Run a single test:
```bash
go test ./path/to/package -run TestName -tags=integration
```

Default dev server port: **24275**, templ proxy port: **9089**.

## Architecture (Hexagonal)

```
main.go                      → Entry point: wires dependencies, starts server
app/                         → App metadata (version constant)
conf/                        → TOML config loader (/etc/code-scout.conf)

internal/domain/             → Domain entities (Project, Log) & error codes/messages
internal/ports/              → Interfaces: repositories + services (contracts)
internal/services/           → Business logic (ProjectService, LogService)
internal/adapters/db/        → GORM persistence: connection, models, mappers, repos

server/                      → HTTP layer
  server.go                  → Server struct, Run(), graceful shutdown
  routes.go                  → Route registration + middleware chain
  handlers/                  → HTTP handlers (project, log, view, response helpers)
  middleware/                → Auth, CORS, logging, recovery, pagination

pkg/cslog/                   → Custom logging framework (Logger, RequestLog, hooks)
pkg/utils/                   → Shared utilities (GormBase, errors, HTTP helpers, pagination)

view/                        → Templ templates (.templ) + static assets (CSS, JS, images)
jobs/                        → Cron scheduler (robfig/cron)
```

**Request flow:** HTTP request → middleware chain (logger → close-conn → CORS → JSON content type → auth) → `server/handlers/` → `internal/services/` → `internal/adapters/db/` → MySQL

## Key Patterns

- **Hexagonal architecture**: Domain entities in `internal/domain/`, interfaces in `internal/ports/`, implementations in `internal/services/` and `internal/adapters/db/`.
- **Dependency injection**: No global DB variable. `*gorm.DB` is passed explicitly from `main.go` through constructors. Repositories and services accept interfaces.
- **Domain/GORM separation**: Domain entities have no ORM tags. GORM models in `internal/adapters/db/models.go` with mappers for conversion.
- **GormBase**: All DB models embed `pkg/utils.GormBase` which provides UUID primary keys (not auto-increment) and soft deletes.
- **API authentication**: Protected routes require `X-Project-ID` and `X-Project-Secret` headers validated against the `project_secrets` table.
- **Templ workflow**: Edit `.templ` files, never edit generated `_templ.go` files directly. Running `make run` handles regeneration automatically.
- **Build metadata**: Binary embeds build time, git branch, commit hash, and dirty file status via ldflags.
- **Server struct**: Encapsulates router, handlers, middleware, and graceful shutdown.

## API Routes

- **Public**: `GET /` (base layout), `GET /login`, `GET /static/*`
- **Protected** (`/api/*`): `POST /api/logs/dump`, `GET /api/validate`, `POST /api/project`, `DELETE /api/project/{project_id}`

## Database

Three tables: `projects`, `project_secrets`, `logs`. GORM auto-migrates on startup. Default connection: `root@localhost:3306/main_db`.
