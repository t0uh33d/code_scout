# Security

## Reporting a vulnerability

Please do not open a public issue.

Use GitHub's [private vulnerability reporting](https://github.com/getcodescout/code_scout/security/advisories/new)
on this repository, or email **touheedkhan1408@gmail.com**.

Tell us what you found and how to reproduce it. You will get a reply within a few days. This is a
small project run by one person, so please be patient with the timeline, but the report will be
taken seriously.

## What is in scope

Code Scout is self-hosted, so the interesting boundaries are:

- **The ingest endpoint** (`POST /api/logs/dump`), which is authenticated with a project ID and
  secret sent by the SDK. Anything letting one project write into another, or letting an
  unauthenticated caller write at all, is in scope.
- **Project access.** Instance roles and per project levels decide what an account can see. A
  project you are not a member of should return 404, and never leak that it exists.
- **The live session socket** and the pairing codes that authorise it.
- **The dashboard session cookie** and the login flow.

## Things that are deliberate, and are not bugs

**Project secrets are stored in plain text.** They have to be readable, because the settings screen
shows them and the ingest endpoint compares them on every upload. Hashing would mean the secret
could never be shown again and would add a bcrypt round to the hot path of every batch. Treat a
project secret like an API key: rotate it if it leaks, which the settings screen supports.

**Nothing in a log is redacted unless the app asks for it.** The SDK sends what your app told it to
send. That is a deliberate product decision, because the auth header is often exactly why a request
is failing. Redaction is opt in and happens on the device before anything is stored. See
[the redaction guide](https://codescout.tech/docs/guides/redaction/).

**The daily cap can overshoot by up to one batch per concurrent upload.** Enforcement is check then
act with no row lock, on purpose, because locking would serialise every upload for a project onto
one row on the hot path. It is a volume control, not a billing boundary.

## Running it safely

- Put it behind TLS. The session cookie and the project secrets travel over the wire.
- Change the passwords in `docker-compose.yml` before exposing it to anyone.
- Set `CS_DB_SSLMODE=require` or stronger if the database is not on the same host.
- Think about redaction before pointing it at production, not after.
