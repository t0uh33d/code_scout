# Code Scout — Product & Release Plan

**Last updated:** 2026-07-30
**Status of this document:** canonical. Supersedes the untracked `DEVELOPMENT_PLAN.md`
and `TODOS.md` at the workspace root, both of which describe `main` and are stale.

---

## What Code Scout is

A free, open-source logging and network inspection tool for Flutter apps. Two parts:

1. **A Flutter SDK** (`code_scout` + `code_scout_dio` / `code_scout_http`) that captures
   logs, network calls and errors, stores them locally in SQLite, and syncs them to your
   server in compressed batches.
2. **A self-hosted web dashboard** (single Go binary) to browse, search, filter and live-tail
   everything the SDK collected.

**Positioning.** Not a crash reporter — it complements one.
*Crashlytics tells you the app crashed; Code Scout shows you what it was doing for the
five minutes before.* App-level tracing, network inspection, session timelines, QA
workflows, on infrastructure you own. Teams should expect to run both.

**Target user.** Flutter devs debugging during development; QA reproducing bugs on real
devices; small teams wanting self-hosted visibility without Datadog pricing.

---

## Architecture decisions (settled — don't relitigate)

| Decision | Choice | Rationale |
|---|---|---|
| Primary datastore | **MySQL** (RDS / Aurora / Cloud SQL all work) | Scale is a stated product requirement; managed MySQL everywhere |
| Server-side SQLite | **No.** Quickstart is `docker compose up` (app + MySQL) | Keeps one storage adapter to maintain; still one-command onboarding. `ports.LogRepository` seam stays open if this is ever revisited |
| Live device streaming | **In scope.** Flutter → WebSocket → server → existing SSE broker → browser | Core to the QA workflow and to the maintainer's own projects |
| Real-time transport | **SSE** browser-side (built), **WebSocket** device-side (to build) | SSE is simpler for server→browser; WS gives framing for device→server. Supersedes the old plan's "WS for both" |
| Config surface | **Boot config** (file + env): DB, TLS, pool, port. **Runtime settings** (DB-backed + UI): retention, quotas, SDK sampling, secrets | The settings UI reads from the DB, so DB creds can't live behind it; also keeps infra config unreachable from a compromised dashboard session |
| Extensibility | Batteries included, swappable via ports | Hexagonal ports already exist. Build only the adapters people are using |
| Horizontal scale | Stateless replicas + Redis-backed `EventPublisher` when `REDIS_URL` is set | Only the in-memory broker pins us to one pod. Build the seam, not the fleet |
| Scale strategy | **SDK-side restraint first** (sampling, flight-recorder, backoff), infra second | Crashlytics' lesson: don't ship the firehose. See "Scaling" below |

---

## Current state

### Flutter SDK — `code_scout` 1.1.1, companions 1.0.1 (fixed, **not yet published**)

**Working:**
- 10 log levels; shorthand `.v() .d() .i() .w() .e() .f()`, plus `.log()` and awaitable `.logMessage()`
- Tags (set-based, wildcard filtering) and arbitrary key-value metadata
- Stack trace capture and parsing, 10-frame limit
- Session tracking — UUID per app launch, correlates all logs from one run
- SQLite persistence (WAL, `idx_logs_sync_status`, sync_status tracking)
- Periodic batch sync: mark syncing → compress in isolate → multipart upload → delete on
  success / rollback on failure; auto-stop after 5 consecutive failures
- tar.gz compression in a background isolate (no UI jank)
- ANSI colour-coded console output
- Network interception via companions: Dio interceptor and `http` BaseClient wrapper,
  3-phase (request → response → error) correlated by `request_id`, 2-min stale eviction
- Draggable floating overlay (Pim) with bottom-sheet menu
- Credential validation (`GET /api/validate`) before sync starts
- Zero third-party HTTP deps in core (`dart:io` only)

**Fixed 2026-07-30, awaiting publish:**
- Capture before `init()` no longer throws `LateInitializationError` into app traffic
- `code_scout_http` no longer returns a drained response stream (was breaking *every*
  request through the wrapper in 1.0.0)
- All capture paths in both interceptors are defensively wrapped — telemetry can never
  fail the request it observes
- Dio `onResponse` no longer throws on a null status code
- First real regression tests (`code_scout_http/test/`)

### Server — branch `feat/complete-dashboard` (pushed, **not yet merged to main**)

**Working:**
- Hexagonal architecture; explicit DI in `main.go`; no global DB
- Project CRUD — create/delete, 32-char secrets, list with pagination + search
- Two auth models: SDK headers (`X-Project-ID`/`X-Project-Secret`) for ingest;
  `cs_session` cookie (bcrypt, 30-day, first-run register) for the dashboard
- Log ingestion `POST /api/logs/dump` — tar.gz decompress, batch insert, SSE publish
- Log query layer — filtering, keyset pagination, stats aggregation
- Search DSL (`level:` `tag:` `session:` `request:` + free text) with parser tests
- **Log viewer** — search, level badges, inline row expansion, infinite scroll, tag pills
- **SSE live tail** — in-memory broker (50 subs/project, 100-deep buffers), with tests
- **Session timeline** and **network request detail** (tabbed request/response/error)
- **Export** — streaming CSV and JSON
- **Retention** — nightly cron, soft-delete past window then purge past grace period
- 24h activity sparkline on project cards
- Templ + HTMX + Tailwind; design tokens synced to the Figma "final" designs
- Static assets embedded in the binary; build metadata via ldflags
- Graceful shutdown, CORS, panic recovery, request-scoped structured logging

**Security hardening, 2026-07-30:**
- `POST`/`DELETE /api/project` now require a login session — they previously fell through
  the auth middleware with **no credential check at all**
- `Authenticate` middleware has no allowlist/fall-through: everything behind it requires
  valid SDK credentials
- Project secrets from `crypto/rand` (were clock-seeded `math/rand`, i.e. predictable)
- Upload caps on ingest: 50MB body, 256MB decompressed (gzip-bomb guard)
- DB DSN (with password) no longer logged at startup

### Website — `codescout-website` (Astro + Starlight)

- Landing page + 8 documentation pages, deployed to Cloudflare
- **The docs are factually wrong** — they document an `init()` signature and method
  signatures that don't exist, describe the interceptors as part of the core package, and
  link to the wrong GitHub org. Fixing this is a v1.0 blocker.
- Design direction for a PocketBase-inspired restyle captured in `DESIGN_NOTES.md` there.

---

## Release plan

### v1.0 — "Ship what exists" (blockers only)

The goal is not new features. It's making what's built discoverable, installable, and
honest. Ordered by urgency.

1. **Publish SDK 1.1.1 / 1.0.1 to pub.dev.** `code_scout_http` 1.0.0 is live and broken
   for every user. Highest-urgency item in this document. *(needs maintainer's pub credentials)*
2. **Close PR #1** (`feature/db-concurrent-insert`) — declined; superseded by the dashboard
   branch, which rewrote the same ingest files. Close it with a short note so the repo has
   no stale open PR at launch.
3. **Merge `feat/complete-dashboard` → `main`.** `main` is a year stale and is what every
   visitor sees.
4. **LICENSE + README.** The site advertises MIT; the server repo has no LICENSE file and
   GitHub shows no README. Front-door credibility.
5. **Docs rewrite** — correct every SDK signature, endpoint, and the GitHub org. Content
   only; restyle is v1.1.
6. **Idempotent ingest** — respect the client-supplied log UUID with a unique index.
   Today a retried batch silently duplicates every row. Prerequisite for live streaming
   (a log can arrive streamed *and* in the next batch).
7. **Composite indexes** — `(project_id, deleted_at, time_stamp)`,
   `(project_id, session_id, time_stamp)`, `(project_id, request_id)`. Without these the
   viewer full-scans per page. Cheapest large win available.
8. **Deployment story** — `docker-compose.yml` (app + MySQL), env-var config overriding
   TOML, `mysql_tls` option, connection-pool limits, startup retry with backoff. These
   four config gaps are also exactly what RDS/Aurora/Cloud SQL need.
9. **Project settings screen** — view project ID, reveal/copy secret, rotate secret,
   rename, delete. **Today there is no way to retrieve your secret after creation.**
10. **Single-tenancy, stated.** All logged-in users see all projects. There's no
    self-registration after the first user, so this is *effectively* single-operator —
    document that explicitly rather than implying team support.
11. **Payload discipline** — `metadata` is a 64KB TEXT column and `sql_mode=''` means MySQL
    silently truncates oversized payloads into invalid JSON. Move to MEDIUMTEXT, cap
    capture size SDK-side, enable strict mode.

### v1.1 — Core screens (the PRD's priority order)

12. **Project sidebar + overview dashboard** — stat cards (logs, errors 24h, network calls,
    sessions), recent errors, activity chart. PRD build-priority #1; the entry point to
    every project. Reuses the existing stats query and sparkline.
13. **Sessions list** — session ID, first/last timestamp, log count, error count → links
    into the existing timeline. Pure aggregate over existing data.
14. **Errors view** — error/fatal grouped by message with occurrence counts, sessions
    affected, first/last seen. Aggregate over existing data.
15. **Runtime settings, DB-backed** — a `settings` table + typed accessors, surfaced in the
    settings screen: retention window, purge grace, upload caps, per-project quotas.
    Replaces today's hardcoded `30, 7` and the constants added during hardening.
16. **Website restyle** per `DESIGN_NOTES.md` (PocketBase-inspired), now that content is correct.
17. **Design-system reconciliation** — mark the stale token appendix in the workspace
    `docs/DESIGN.md` as superseded by `code_scout/DESIGN.md`; they currently disagree on the
    log-level palette.

### v1.2 — Live streaming (the differentiator)

18. **Server WS endpoint** `WS /ws/device` — SDK-header auth, feeds straight into the
    existing `ports.EventPublisher`. Connected-device registry falls out of it.
19. **Flutter WS client** — replace the dead raw-TCP handshake in `CSxInterfaceController`
    with `dart:io` `WebSocket.connect` (framing for free, keeps zero-third-party-deps).
    Retire the unused protocol constants and wire up `RealTimeConfig.enableLiveStreaming`.
20. **Live Devices screen** — connected devices, status, session, connected-since, "Watch".
21. **Live Log Stream screen** — auto-scroll with pause/resume, level and type filters,
    highlight-search, clear, export buffer, "persist logs" toggle, multi-viewer count.
22. **Dual mode** — batch sync and live streaming running simultaneously without
    duplication (depends on item 6).

### v1.3 — Trust & SDK maturity

23. **Redaction by default** — `Authorization` and `Cookie` headers redacted unless opted
    in; configurable redaction keys; body-capture opt-out and size caps. Currently full
    headers and bodies, including bearer tokens, are stored in plaintext. **Treat this as a
    blocker before promoting Code Scout for team use.**
24. **SDK scaling controls** — session sampling (remote-configurable %, e.g. 1% in prod /
    100% on QA devices), flight-recorder mode (keep verbose logs local, upload only around
    an error with preceding context), and server backoff (429 + `Retry-After` honoured).
    This is the actual answer to "what if a million-user app installs this" — see below.
25. **Implement or remove `captureDeviceInfo` / `captureAppContext`** — both are public,
    default `true`, and read by nothing. Implementing unlocks device/OS/app-version fields
    that the Live Devices and Sessions screens want.
26. **Real test coverage** — server: services, repos, handlers, middleware (currently only
    `pkg/sse` and `pkg/search` have tests). SDK: level filtering, the sync mark/rollback
    pipeline, tar.gz round-trip, interceptor correlation. Also delete the `flutter create`
    boilerplate test in `example/` that asserts on a counter that isn't there.
27. **CI** — build, vet, test, analyze across all three repos, plus a PR template. No
    `.github/` exists anywhere today, so nothing gates an incoming contribution.

### v2.0 — Scale-out (build when a real deployment demands it)

28. **Redis-backed `EventPublisher`** — when `REDIS_URL` is set, publish through Redis
    pub/sub instead of process memory. This is the *only* thing pinning the server to one
    pod; after it, k8s is `Deployment` + N replicas + HPA + managed MySQL. Small change,
    large capability.
29. **Helm chart / k8s manifests**, and a documented horizontal-scaling story in the README.
30. **Queue-buffered ingest** — endpoint appends to NATS JetStream or Redis Streams; worker
    pods drain into the DB. Absorbs spikes.
31. **ClickHouse adapter for the `logs` table**, MySQL retaining users/projects/secrets. The
    standard split every event-heavy OSS tool converges on (Sentry, PostHog, SigNoz, Uptrace).
32. **Load harness** (k6 or a Go bench firing synthetic tar.gz batches) so "scalable" is a
    measured number. Belongs in CI.

**Non-negotiable:** single-node mode must never stop being the default. Loki's lesson —
the small install and the huge install should be the same binary with different config.

### Later / explicitly deferred

- **Alert rules & notifications** (threshold evaluation on the existing stats aggregation +
  cron, webhook/email delivery). Real feature, post-v1 — don't let it compete with the
  core screens.
- **Multi-user project ownership** — `project_users` join table, queries scoped by user.
  Needed for genuine team deployments; migration assigns existing projects to the first user.
- Crash-reporting integration (catch `FlutterError` / `runZonedGuarded` as fatal logs so the
  session timeline shows the crash *in context* — the honest complement to Crashlytics)
- Performance metrics (request durations, response sizes), dashboard favourites backend,
  full-text search indexing, IAM database auth for RDS

---

## Scaling: what happens at a million users

Rough arithmetic: 1M installs / 100K DAU / ~350 rows per session ≈ **35M rows/day,
~400 rows/sec average, 40–70GB/day**. No single-node deployment serves that — and it
shouldn't have to, because that's not where the industry solves this problem.

**Crashlytics' approach is client-side restraint, not server muscle.** Breadcrumbs live in
a small on-device ring buffer (~64KB) and upload *only attached to a crash* — the rare
event. Steady-state per-device data rate is near zero, and server storage scales with
unique problems, not users.

Applied here, in impact order:

1. **Session sampling** (v1.3) — 1% of production sessions cuts 35M rows/day to ~350K. QA
   devices stay at 100%. Single highest-leverage feature in this document.
2. **Flight-recorder mode** (v1.3) — verbose logs stay local; upload on error with preceding
   context. Also a genuinely better *product*: "when something breaks you get the whole story."
3. **Server backoff / kill-switch** (v1.3) — flow control instead of failure handling.
4. **Indexes, idempotency, quotas, payload caps** (v1.0) — the durability floor.
5. **Redis broker + replicas** (v2.0) — boring horizontal scaling.
6. **Queue + ClickHouse** (v2.0) — only on real demand.

With 1–3 in place, "a million users" stops being an infrastructure problem for the large
majority of deployments.

---

## Known debt (tracked, not blocking)

- `DeleteProject` aborts if a project has no secret row, making such projects undeletable
  (`project_service.go` — `GetSecret`'s `ErrRecordNotFound` is returned before the nil guard)
- `logs.time_stamp` is stored in server-local time (`loc=Local` in the DSN) rather than UTC,
  despite the SDK sending UTC. Self-consistent, but a TZ change reinterprets history
- `is_network_call` / `call_phase` are stored but never surfaced in the main log list — a
  request, its response and its error render as three visually identical rows
- `domain.CustomBool` accepts only JSON numbers; a third-party client sending
  `"is_network_call": true` 500s the whole batch
- SDK emits `level: "all"` / `"off"`, which the DB accepts but the search parser rejects (400)
- `NetworkErrorData` omits the `{'network'}` tag its request/response siblings both set, so
  `tag:network` hides errors
- Existing deployed DBs need a manual `ALTER` for the old `call_phase` CHECK constraint
  (GORM auto-migrate won't alter it); `datetime(3)` does apply automatically
- Existing project secrets were minted with the weak RNG — regenerate once secret rotation
  ships (v1.0 item 9)
- Workspace-root product docs (`docs/`, `DEVELOPMENT_PLAN.md`, `TODOS.md`) are untracked by
  any git repo. Adopt the good ones into this repo; delete the stale ones.
