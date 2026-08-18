# Contributing to CodeScout

Thanks for looking. This is a small project and pull requests are genuinely welcome.

This repository is the Go half of CodeScout: the dashboard you look at and the endpoint the SDK
uploads to. The Flutter side, which is the SDK, the network interceptors and the in-app overlay,
lives in [code_scout_flutter](https://github.com/getcodescout/code_scout_flutter) and has its own
contributing guide. If the thing you want to change happens inside the app rather than in the
browser, that is the repository you want.

## Before you start something big

Open an issue first. Not as a formality, but because the fastest way to waste a weekend is to
build something that does not fit where the project is going. A short issue saying what you want
to do and roughly how usually gets an answer the same day.

Small fixes need no ceremony. Typos, broken links, a confusing error message: just send the pull
request.

## Getting set up

You need Go 1.25 or newer and a Postgres 16 server. You also need the `psql` client on your PATH,
because every database command in the Makefile shells out to it, and on most systems that is a
separate package from the server. It is the piece you are missing if your Postgres runs in Docker.
Node is only needed for the browser tests, and the Flutter toolchain only for `make test-sdk-e2e`.

```bash
git clone https://github.com/getcodescout/code_scout.git
cd code_scout
make dev-setup     # copies .env.example to .env, then creates the Postgres role and database
make dev           # starts the server, watching for changes
```

`make dev-setup` talks to Postgres through `psql` as your own operating system user. That is the
superuser Homebrew creates on macOS, so it works there without arguments. Most Linux packages call
their superuser `postgres` instead, so pass it along: `make dev-setup pg_super=postgres`. If the
first command fails with a role or authentication error, that is almost always what happened.

The server is then on <http://localhost:24275>, and it rebuilds itself whenever you save a `.go`
file. While you are working on the UI, browse to <http://localhost:9089> instead. That is templ's
watch proxy sitting in front of the same server, and it is the one that reloads the tab for you
when you save a `.templ` file.

`make dev` needs two tools on your PATH. [air](https://github.com/air-verse/air) rebuilds and
restarts the Go binary when you save, and [templ](https://templ.guide) regenerates the templates:

```bash
go install github.com/air-verse/air@latest
go install github.com/a-h/templ/cmd/templ@v0.3.960
```

Install templ at the version pinned in `go.mod`, not `@latest`, even though the Makefile's
missing-tool hint suggests otherwise. Every templ release stamps its own version number into the
header of each generated file, so a newer generator quietly rewrites all of them and CI's
staleness check goes red on files you never opened.

## Two generated files are committed

The `_templ.go` files are generated from the `.templ` sources next to them. `make dev` keeps them
current while it runs. To regenerate once, use the same invocation CI does:

```bash
TEMPL_EXPERIMENT=rawgo templ generate
```

The `TEMPL_EXPERIMENT=rawgo` prefix is not optional. And do not reach for `make templ` here: that
target is the watch and never returns.

The compiled Tailwind bundle at `view/static/css/tailwind.css` is committed too, and `make dev`
does not rebuild it. If you add a Tailwind class to a template, run `make tailwind` for a one-shot
build, or `npm run dev` to watch while you work, and commit the regenerated file with your change.
Nothing in CI checks that the bundle matches the templates, so a stale one ships silently. It is
the one generated artefact with no safety net.

## Changing the database schema

There are no migration files, on purpose. The server runs AutoMigrate on startup, so a new column
appears on your next `make dev`. A rename, a type change or a removed column does not, and there
is no deployed instance to write migrations for, so the answer is a rebuild: `make db-reset` drops
the local database and recreates it empty, and the next `make dev` rebuilds every table from the
current models. It asks you to type the database name before doing anything, and `force=1` skips
that prompt for scripts. Stop `make dev` first: the reset does terminate open connections, because
Postgres refuses to drop a database anything is attached to, but an `air` still running in another
terminal reconnects fast enough to make the drop fail anyway.

## Running the tests

```bash
make test          # unit tests, no database needed
make test-all      # adds the integration tests, against a separate code_scout_test database
make test-e2e      # browser tests against a real server (needs `npm install` and Chrome)
make test-sdk-e2e  # the real Flutter SDK against a real server
```

The integration tests skip themselves unless `CS_TEST_DB` is set, which `make test-all` handles.
They are not optional extras: they cover unique indexes and `ON CONFLICT` behaviour, which is
exactly the part a mock cannot tell you anything about.

The `code_scout_test` database is created once and kept, not rebuilt each run. If integration
tests start failing on a branch that changed a model, drop it and let the next run recreate it:
`psql -U <your superuser> -d postgres -c 'DROP DATABASE code_scout_test'`. AutoMigrate adds
columns but will not rename, retype or remove one.

The browser tests build their own server on port 24283 against a throwaway `code_scout_e2e`
database and clean both up afterwards, so they never touch what you develop against. Test files
live in `e2e/*.test.js` on Node's built-in test runner, and they run one at a time because they
all share that server: in parallel, two of them would race to register the first account. Seeding
goes through the real ingest endpoint with a real gzipped tar, so every dashboard test also
exercises the wire contract. A new test should seed the same way rather than writing rows
directly.

`make test-sdk-e2e` expects `code_scout_flutter` checked out next to this repo, and takes
`sdk_dir=/path/to/code_scout_flutter` when it is not. It is the only test anywhere that proves the
SDK and the dashboard still agree with each other, so it is worth running if you touch the ingest
endpoint or anything about the wire format.

## What runs on your pull request

One workflow, `.github/workflows/ci.yml`. On a pull request it runs `go build`, `go vet` and
`go test` with `CS_TEST_DB` pointed at a Postgres 16 service container, so the integration tests
do run in CI. Then it regenerates the templ output with the go.mod version and fails if the
committed files differ, and it finishes by building the Docker image as a smoke test.

There is no Node, no Playwright and no Flutter anywhere in that file, so `make test-e2e` and
`make test-sdk-e2e` never run in CI. Your local run is the only signal those two suites give,
which is worth knowing given how much this document leans on them.

## What a good pull request looks like

**It comes with a test, and the test fails without the fix.** This is the one thing we are strict
about. The honest way to check is to undo your change and watch the test go red. If it stays
green, the test is not testing what you think it is, and that is worth knowing before review
rather than after.

**It explains why in the code, not just what.** Comments that say what the line does age badly and
help nobody. Comments that say why it is written that way are the ones people are grateful for at
3am. If you worked around something surprising, that is exactly the sentence worth writing down.

**It is one change.** A pull request that fixes a bug and also reformats four files is hard to
review and hard to revert.

**It brings its changelog line with it.** `CHANGELOG.md` is the release notes: at release time CI
extracts the section matching the tag and refuses to release when it finds nothing there. Writing
the line while the change is fresh is cheap, and reconstructing it at tag time is not. Leave
`app/version.go` and the tags alone, though. Bumping the version and tagging are release steps,
not contribution steps.

## House rules worth knowing

**Never edit a `_templ.go` file.** They are generated from the `.templ` files next to them, and CI
regenerates them and fails the build if the committed output differs. The common way that check
goes red is not a hand edit at all: it is a templ binary at the wrong version, because each
release stamps its own number into every file it writes. Install the go.mod version and the
problem never comes up.

**Access is decided by middleware, never inside a handler.** Reading anything under
`/project/{id}` goes through `RequireProjectAccess` on `projectRouter`, and a project you cannot
see answers 404 rather than 403, so its existence stays private. Changing a project is a second
gate: hang the route off `manageRouter` (settings, the secret, membership, writing to a paired
device's database) or `deleteRouter` (destroying it), both in `server/routes.go`. Those answer
403, because by then you can already see the project and the refusal leaks nothing. Watch out for
routes that take the project id in the query string rather than the path: `/export/logs` and
`/stream/logs` do not look project-scoped, yet each hands back a whole project, so they sit on
`projectQueryRouter` behind the same read check. If you add a route and pick the wrong subrouter,
no test catches it, which is exactly why the choice is worth stopping over.

**Never log a credential, and never send internals to a client.** This is the house rule with the
worst history here: the session token was once logged at debug on every request, and stack traces
were once written into panic responses. Both shipped, and both went unnoticed for months. If your
change touches logging or an error path, test it the way `TestTheSessionTokenIsNeverLogged` does,
by capturing the logger's real output and asserting the secret's bytes are absent, because an
assertion on the call site keeps passing until the next `WithField`. On the response side, a panic
gets a plain 500 and the stack goes to the log.

**Read `DESIGN.md` before changing anything visual.** It is not a mood board. It pins the type
scale, the colour tokens, the spacing steps, the shape of every shared component, how much motion
is allowed and the keyboard shortcuts, and it keeps a log of the decisions behind all of it. The
dashboard is data dense, so a heading half a step off or a panel with its own idea of padding
shows up immediately next to the screens around it, and fifteen minutes in that file is usually
the difference between a pull request that merges and one that comes back asking you to redo the
visual half.

**Filter state goes in the query string.** Every control in the log viewer is a link to the view
you would have after clicking it. That is what makes a pasted URL reproduce a screen exactly, and
it is worth preserving.

## The MCP endpoint

`/api/mcp` lets a coding agent read CodeScout: eighteen read-only tools in `server/mcptools/`,
wrapping the same services the dashboard's screens call, so the two cannot answer differently.
Auth is a personal access token minted on `/account` and sent as `Authorization: Bearer csp_...`,
and only a hash of it is ever stored. `TOKEN=csp_... make mcp-smoke` pokes the endpoint by hand.

Two rules there must survive any edit. Read-only is structural, not policed: no tool input carries
an op, arguments, SQL or a value to write, and `TestNoToolCanExpressAWrite` walks the registered
tools and fails on a write-shaped name or field. Do not add a generic "ask the device" tool,
because that ends the argument. And errors are sanitised at the source: the MCP SDK packs a
returned Go error's text straight into the tool result, so unexpected errors go through
`internal()`, which logs the real thing and hands the client a bare "internal error". Never return
a raw service error from a tool.

## Reporting a bug

Please include what you did, what you expected, and what happened instead. For anything involving
ingest, the server log around the failure is usually the whole answer.

If the problem turns out to be in the app rather than the dashboard, in the SDK, an interceptor or
the on-device overlay, it belongs in the
[code_scout_flutter](https://github.com/getcodescout/code_scout_flutter/issues) repository, and
the issue chooser here will point you there.

For security issues, see [SECURITY.md](SECURITY.md) and please do not open a public issue.

## Licence

By contributing you agree that your contribution is licensed under the MIT licence, the same as
the rest of the project.
