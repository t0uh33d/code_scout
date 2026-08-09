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

## [1.0.0] - 2026-08-09

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
  waterfall and a split inspector that swaps without reloading the page. A
  session's Network tab is the same split, filtered to that launch, so you can
  read one call after another without leaving it.
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
- **Favourites.** Star a project and it pins to its own tab, per user rather
  than per project. The tab is in the query string, so it is a link you can send.
- **A time range on the overview**: the last 24 hours, 7 days or 30 days, in the
  query string so the view is a link you can send. The chart's bucket grows with
  the range, so it keeps its shape at roughly 24 to 30 columns instead of going
  from 24 bars to 168 hairlines, and the labels follow: a column is an hour at a
  day and a day at a month. Ranges longer than your retention are not offered,
  because a chart of rows the nightly job deleted reads as a collapse in traffic
  rather than as the edge of what you keep. For the same reason the day-over-day
  delta is hidden when the period it compares against is past retention.
- **A `reset-password` subcommand**, the recovery path for a locked out super
  admin. No admin outranks them and no email is ever sent, so the way back in is
  shell access to the server.
- **A version you can see.** It shows in the sidebar, in the startup log, and in
  `/healthz`, which answers it without a login because "what are you running?"
  is the first question asked about a misbehaving instance. A daily check for a
  newer release is on by default and switchable off in Settings; it asks GitHub
  and sends nothing about the instance.
- **Sessions record which version of the SDK sent them**, so an app running a
  build old enough to be missing a fix is visible instead of guessed at. It is a
  column on Sessions and on the session itself, and `sdk_version:` filters on it.
- **Logging you can configure.** `log_level` (`info` by default), `log_format`,
  and `log_file` with size, count, age and compression for rotation. Unset, it
  writes to stdout, which systemd and Docker already capture and rotate. Lines
  are readable text on a console and JSON in a file, one per request with
  `duration_ms` as a number, the response size and the caller's address. Errors
  and fatals also go to stderr when a file is configured, so a server that fails
  to start says why where you would look.

### If you have been running this from source

Three things were fixed shortly before this release. They never reached a
tagged build, but an instance built from `main` before 2026-08-07 has them.

- **The session token was written to the log** at debug on every request that
  arrived with a cookie, and debug was the hardcoded level, so the journal
  accumulated live `cs_session` values. Anyone who could read that log could
  paste one into a browser and be that user. **Rotate existing sessions if your
  log has been readable by anyone you would not hand an account to.** The fix
  stops new lines being written; it cannot retract old ones.
- **A panic sent the client its stack trace**, which handed anyone who could
  trigger one the source paths, package layout and function names of the server.
- **The response body was logged on a 4xx or 5xx**, a second way for anything
  sensitive to escape.

[1.0.0]: https://github.com/getcodescout/code_scout/releases/tag/v1.0.0
