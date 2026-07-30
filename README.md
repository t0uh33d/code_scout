# Code Scout

Self-hosted logging and network inspection for Flutter apps.

Add one package to your app. Every log call and HTTP request gets captured, printed to your
console, stored on the device, and synced to a dashboard you run yourself. From there you can
search it, filter by tag, replay a session, or watch a device live while QA reproduces a bug.

Code Scout is not a crash reporter. Crashlytics tells you the app crashed. Code Scout shows you
what it was doing for the five minutes before. Most teams run both.

| | |
|---|---|
| **Dashboard** (this repo) | Go 1.23, MySQL, Templ + HTMX + Tailwind |
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
│ SQLite (on device)     │   tar.gz     │  MySQL                   │
│          ↓             │   upload     │          ↓               │
│ Sync worker            │              │  Dashboard + live tail   │
└────────────────────────┘              └──────────────────────────┘
```

Logs go to SQLite on the device first, so nothing is lost when the network is flaky. A
background worker batches them, compresses them in an isolate, and uploads. If an upload fails,
the batch rolls back and gets retried.

---

## Running the dashboard

You need **Go 1.23+** and a **MySQL 5.7+** database. A Docker Compose setup is on the roadmap.
For now the steps are manual.

**1. Create a database**

```sql
CREATE DATABASE code_scout CHARACTER SET utf8mb4;
```

**2. Write `/etc/code-scout.conf`**

```toml
host = "localhost"
port = 24275

mysql_host     = "localhost"
mysql_port     = 3306
mysql_user     = "root"
mysql_password = "your-password"
mysql_database = "code_scout"
```

**3. Start it**

```bash
go run main.go          # or: make run   (templ watch + air hot reload)
```

Tables are created automatically on first start. Open <http://localhost:24275>.

**4. Create your account and a project**

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
rotation, live device streaming, Docker Compose, redaction of sensitive headers.

That last one matters if you are considering this for production. Request and response headers
are stored exactly as captured, including `Authorization`. Redaction by default is planned
before 1.0. Until then, be deliberate about what you point it at.

---

## Development

```bash
make run     # dev server with hot reload (templ watch + air)
make build   # build a linux/amd64 binary into ./bin
make test    # run the test suite
```

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

Issues and pull requests are welcome. The most useful things right now are the Docker Compose
setup, the project settings screen, and anything that makes the first fifteen minutes easier.
Open an issue before starting something large, so we can check it fits the direction.

---

## License

MIT. See [LICENSE](LICENSE).
