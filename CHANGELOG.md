# Changelog

All notable changes to the Code Scout dashboard are recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This file is the source of the GitHub release notes. When a `v*` tag is pushed,
CI reads the section matching that version and publishes it, and the release
fails if there is no matching section. So writing the entry is part of making
the change, not something to remember at release time.

The version itself lives in `app/version.go`. CI checks the tag against it and
refuses to publish when the two disagree.

The Flutter SDK has its own changelog, in
[code_scout_flutter](https://github.com/getcodescout/code_scout_flutter).

## [Unreleased]

### Added

- The running version is now visible. It shows in the sidebar, in the startup
  log, and in `/healthz`, which answers it without a login because "what are you
  running?" is the first question asked about a misbehaving instance.
- A daily check for a newer release, on by default and switchable off in
  Settings. It asks GitHub and sends nothing about the instance. An instance
  with no route to the internet shows nothing rather than an error.
- Sessions record which version of the SDK sent them, so an app running a build
  old enough to be missing a fix is visible instead of guessed at. It shows as a
  column on Sessions and on the session itself, and `sdk_version:` filters on
  it, which is how you find every launch still on a build that predates a fix.

- **Logging you can configure.** `log_level` (now `info`, not the hardcoded
  `debug`), `log_format`, and `log_file` with size, count, age and compression
  for rotation. Unset, it still writes to stdout, which systemd and Docker
  already capture and rotate. Lines are readable text on a console and JSON in a
  file. Errors and fatals also go to stderr when a file is configured, so a
  server that fails to start says why where you would look.
- One line per request, with `duration_ms` as a number, the response size and
  the caller's address. `/healthz` and `/static/` log at debug, so a probe every
  15 seconds stops being most of the volume.

### Changed

- Build metadata moved from package `main` into `app`, so every layer reads the
  same values. The three build paths that inject it had already drifted: the
  Dockerfile passed a build argument named `VERSION` into a variable named
  `BranchName`, and nothing displayed the result, so nothing caught it.

### Security

- **The session token is no longer logged.** It was written at debug on every
  request that arrived with a cookie, and debug was the hardcoded level, so a
  running instance accumulated live `cs_session` values in its journal. Anyone
  who could read that log could paste one into a browser and be that user.
  **Rotate existing sessions if your log has been readable by anyone you would
  not hand an account to**; the fix stops new lines being written, it cannot
  retract old ones.
- **A panic no longer sends the client its stack trace.** The panic value and
  the full Go stack went into the response body, which handed anyone who could
  trigger one the source paths, package layout and function names of the server.
  The client gets a plain 500 and the stack goes to the log.
- **The response body is no longer logged on a 4xx or 5xx.** Whatever a handler
  wrote went into the log, which is a second way for anything sensitive to
  escape. It was also only ever the last chunk written, so it was a fragment.

### Fixed

- **Requests to the dashboard were not logged at all.** The logging middleware
  was mounted on the auth and SDK routes only, so every project screen, every
  page of the log viewer and every settings save produced no line. The database
  lines those requests did produce had no request id on them either, because the
  request-scoped logger never reached them.
- **Export answered 200 with an empty body when the search query was invalid**,
  which is indistinguishable from a search that matched nothing. It answers 400
  and says what was wrong. The query is parsed before the first byte is written,
  because once a download has started its status is already decided.

### Removed

- The `DirtyFiles` build variable. It piped `git status --porcelain`, which is
  newline separated and contains arbitrary filenames, into a linker flag, and
  nothing ever read it.
- `pkg/cslog/log_hook.go`, which had no callers and would have deadlocked the
  first time it was given one: it sent on an unbuffered channel to a goroutine
  that returned on its first write error.
- The absolute build path and second timestamp on every line from the
  package-level log helpers. They came from splitting the source path on
  `codescout_api`, a module name this project has not used in years, so the
  split never matched and the whole path from the build machine was printed.

## [1.0.0] - 2026-08-07

The first release. A self-hosted dashboard that receives batched logs and
network calls from a Flutter app, and can watch a paired device live.

### Added

- **Projects.** A create wizard that sets access as part of creating the
  project, settings in four tabs, and a secret you can reveal and rotate rather
  than one shown once and lost.
- **Accounts and access.** Three instance roles and a per-project level on top,
  decided in one place so a handler cannot forget to check. A project you cannot
  see answers 404 rather than 403. A temporary password forces a change on first
  use.
- **Log viewer.** Level toggles, tag chips with counts, time windows, keyboard
  navigation and infinite scroll. Every control is a link to the query you would
  have after clicking it, so the filter state lives in the URL and sharing a
  view is sharing a link.
- **Search that reaches the session.** `user:`, `installation:`, `app_version:`,
  `device:` and `os:` filter on facts about the launch rather than the log line.
- **Errors**, grouped by fingerprint so one bug is one row. Messages group with
  their varying parts blanked, and network failures group on method and path
  because every one of them carries the same message.
- **Network.** The three phases of a call paired back into one row, with a
  waterfall and a split inspector that swaps without reloading the page.
- **Sessions and Devices.** One row per launch, and launches rolled up by
  installation id.
- **Live sessions.** A paired device streams to the dashboard in real time over
  a WebSocket, fanned out to watchers over SSE. Nothing is persisted. A
  reconnecting watcher is caught up from a ring buffer, and told plainly when it
  missed more than the buffer held.
- **The live database browser.** While a device is paired, browse its SQLite
  tables, `shared_preferences` and Hive boxes, and edit one cell at a time. The
  dashboard never sends SQL: five structured operations, none of which has a
  field a statement could go in, so read-only is a property of what can be
  expressed. Nothing is browsable until the app registers it, and nothing is
  editable unless the app marks it writable.
- **Instance settings**, read live so a change applies with no restart. Display
  timezone and a 12 or 24 hour clock, retention, upload cap and a per-project
  daily log cap.
- **Volume controls.** Session sampling per project, which the SDK reads at
  startup and combines with its own rate by taking the lower of the two, so a
  server can only ever make an app quieter. A daily log cap that refuses a batch
  whole, with `Retry-After`, because the SDK has no way to say it accepted part
  of one.
- **Ingest** that reads archive entries by name rather than position, and is
  idempotent on the client's own id, so a replayed batch after a lost response
  inserts nothing and is charged for nothing.
- **Export** to CSV and JSON, log streaming over SSE, a nightly retention job,
  graceful shutdown and panic recovery.
- **A `reset-password` subcommand**, the recovery path for a locked out super
  admin. No admin outranks them and no email is ever sent, so the way back in is
  shell access to the server.

[Unreleased]: https://github.com/getcodescout/code_scout/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/getcodescout/code_scout/releases/tag/v1.0.0
