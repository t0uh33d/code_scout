# Code Scout

Self-hosted logging and network inspection for Flutter apps.

Add one package to your app. Every log call and HTTP request gets captured, printed to your
console, stored on the device, and synced to a dashboard you run yourself. From there you can
search it, filter by tag, replay a session, or watch a device live while QA reproduces a bug.

Code Scout is not a crash reporter. Crashlytics tells you the app crashed. Code Scout shows you
what it was doing for the five minutes before. Most teams run both.

| | |
|---|---|
| **Dashboard** (this repo) | Go 1.24, Postgres, Templ + HTMX + Tailwind |
| **Flutter SDK** | [`code_scout`](https://pub.dev/packages/code_scout), [`code_scout_dio`](https://pub.dev/packages/code_scout_dio), [`code_scout_http`](https://pub.dev/packages/code_scout_http) |
| **SDK source** | [code_scout_flutter](https://github.com/t0uh33d/code_scout_flutter) |

> **Status:** actively being built toward 1.0. The log viewer, session timeline, network detail,
> search, live tail, export and retention all work today. The project overview, sessions list,
> error grouping, project settings and live device streaming are still to come. See
> [What works today](#what-works-today).

---

## How it fits together

```
Flutter app                              Your server
┌────────────────────────┐              ┌──────────────────────────┐
│ CodeScout.instance.i() │              │                          │
│ Dio / http interceptor │              │  POST /api/logs/dump     │
│          ↓             │  ──batched── │          ↓               │
│ SQLite (on device)     │   tar.gz     │  Postgres                │
│          ↓             │   upload     │          ↓               │
│ Sync worker            │              │  Dashboard + live tail   │
└────────────────────────┘              └──────────────────────────┘
```

Logs go to SQLite on the device first, so nothing is lost when the network is flaky. A
background worker batches them, compresses them in an isolate, and uploads. If an upload fails,
the batch rolls back and gets retried.

---

## Running the dashboard

Clone the repo and start it. Nothing else to install.

```bash
git clone https://github.com/t0uh33d/code_scout.git
cd code_scout
docker compose up
```

That brings up Code Scout and a Postgres database. Tables are created on first start. Open
<http://localhost:24275>.

**Change the passwords in `docker-compose.yml` before you put this anywhere public.**

### Using your own database

If you already run Postgres, whether that is RDS, Cloud SQL or your own server, delete the
`db` service from `docker-compose.yml` and point the app at yours:

```bash
docker run -p 24275:24275 \
  -e CS_DB_HOST=your-db.example.com \
  -e CS_DB_USER=code_scout \
  -e CS_DB_PASSWORD=secret \
  -e CS_DB_NAME=code_scout \
  -e CS_DB_SSLMODE=require \
  t0uh33d/code_scout:latest
```

### Configuration

Everything is set with environment variables. You can also put the same keys in
`/etc/code-scout.conf` as TOML, without the `CS_` prefix. Environment variables win.

| Variable | Default | |
|---|---|---|
| `CS_DB_HOST` | | required |
| `CS_DB_PORT` | `5432` | |
| `CS_DB_USER` | | required |
| `CS_DB_PASSWORD` | | |
| `CS_DB_NAME` | | required |
| `CS_DB_SSLMODE` | `disable` | `require`, `verify-ca` or `verify-full`. Managed databases usually need at least `require` |
| `CS_HOST` | `0.0.0.0` | |
| `CS_PORT` | `24275` | |
| `CS_PUBLIC_BASE_URL` | | the URL people reach this instance on, if it sits behind a proxy |
| `CS_MAX_OPEN_CONNS` | `25` | keep below your database's connection limit |
| `CS_MAX_IDLE_CONNS` | `5` | |
| `CS_CONN_MAX_LIFETIME_MINUTES` | `30` | |

The server waits for the database on startup, retrying with backoff, so it is safe to start
both at once. `GET /healthz` returns 200 when it is ready and 503 when the database is
unreachable, which is what the container healthcheck uses.

### Create your account and a project

The first visit shows a registration form. There is no default login, and the first account you
create becomes the owner. After that, the same page becomes a login form.

Then click **Add new project**. The project ID and secret key appear once, right after you
create it. Copy them straight away, because there is currently no way to see the secret again.

---

## Adding the SDK to your app

```bash
flutter pub add code_scout
flutter pub add code_scout_dio    # if you use Dio
flutter pub add code_scout_http   # if you use package:http
```

Initialise it once, early:

```dart
await CodeScout.instance.init(
  freshContextFetcher: () => context,
  configuration: CodeScoutConfiguration(
    logging: LoggingBehavior(
      minimumLevel: LogLevel.all,
    ),
    projectCredentials: ProjectCredentials(
      link: 'http://localhost:24275/',
      projectID: 'your-project-id',
      projectSecret: 'your-secret-key',
    ),
    sync: LogSyncBehavior(
      maxBatchSize: 100,
      syncInterval: Duration(seconds: 30),
    ),
  ),
);
```

Then log:

```dart
final scout = CodeScout.instance;

scout.d('Cart restored from cache');
scout.i('Checkout started', tags: {'analytics', 'checkout'});
scout.e('Payment failed', error: e, stackTrace: st);
```

And capture network calls:

```dart
// Dio
dio.interceptors.add(CodeScoutDioInterceptor());

// package:http
final client = CodeScoutHttpClient(client: http.Client());
```

`projectCredentials` is optional. Leave it out and Code Scout works as a local logging framework,
giving you console output and on-device history with nothing leaving the device. Add the
credentials when you want the dashboard.

---

## Searching

The search box takes a small query language. You can combine these with free text.

| | |
|---|---|
| `level:error` | one level |
| `tag:checkout` | logs carrying a tag |
| `session:4f2a81b0` | one app session |
| `request:7d19c204` | one network call, all phases |
| `"gateway timeout"` | free-text match on the message |

---

## What works today

**Working:** log ingestion and storage, the log viewer with search, level badges, inline
expansion and infinite scroll, live tail over SSE, session timeline, network request detail,
CSV and JSON export, nightly retention, user accounts and sessions, project creation.

**Not yet:** project overview, sessions list, error grouping, project settings and secret
rotation, live device streaming, redaction of sensitive headers.

That last one matters if you are considering this for production. Request and response headers
are stored exactly as captured, including `Authorization`. Redaction by default is planned
before 1.0. Until then, be deliberate about what you point it at.

---

## Development

```bash
make dev-setup   # first time: creates .env and the local database
make dev         # dev server with hot reload (templ watch + air)
make test        # unit tests
make test-all    # unit + integration tests (creates a scratch database)
make build       # build a linux/amd64 binary into ./bin

docker build -t code_scout .    # build the image
```

Some tests need a real Postgres, because they cover unique indexes and
`ON CONFLICT` behaviour that a mock cannot exercise. They skip unless
`CS_TEST_DB` is set, which `make test-all` handles for you.

Running `make dev` needs Go 1.24+, a local Postgres, `air` and `templ`. Run `make dev-setup`
once first to create `.env` and the database.

The dashboard is written in [Templ](https://templ.guide). Edit the `.templ` files, never the
generated `_templ.go` files. `make run` regenerates them for you, and a hand-edited generated
file gets silently overwritten on the next build.

The server uses a hexagonal layout:

| Package | Holds |
|---|---|
| `internal/domain` | entities and error codes, no framework code |
| `internal/ports` | the interfaces everything else depends on |
| `internal/services` | business logic |
| `internal/adapters/db` | GORM models, mappers and repositories |
| `server/handlers` | HTTP handlers |
| `view` | Templ templates |

Handlers depend on the port interfaces rather than concrete types, and everything is wired
explicitly in `main.go`. There is no global database handle.

`DESIGN.md` documents the design system. Read it before changing anything visual.

---

## Contributing

Issues and pull requests are welcome. The most useful things right now are the project settings
screen, the project overview, and anything that makes the first fifteen minutes easier.
Open an issue before starting something large, so we can check it fits the direction.

---

## License

MIT. See [LICENSE](LICENSE).
