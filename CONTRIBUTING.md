# Contributing to Code Scout

Thanks for looking. This is a small project and pull requests are genuinely welcome.

## Before you start something big

Open an issue first. Not as a formality, but because the fastest way to waste a weekend is to
build something that does not fit where the project is going. A short issue saying what you want
to do and roughly how usually gets an answer the same day.

Small fixes need no ceremony. Typos, broken links, a confusing error message: just send the pull
request.

## Getting set up

You need Go 1.25 or newer, Postgres 16, and Node if you want to run the browser tests.

```bash
git clone https://github.com/getcodescout/code_scout.git
cd code_scout
make dev-setup     # writes .env and creates the local database
make dev           # starts the server with hot reload on :24275
```

`make dev` also needs [air](https://github.com/air-verse/air) and
[templ](https://templ.guide) on your PATH.

## Running the tests

```bash
make test          # unit tests, no database needed
make test-all      # adds the integration tests, against a scratch database
make test-e2e      # browser tests against a real server (needs Chrome and npm install)
make test-sdk-e2e  # the real Flutter SDK against a real server
```

The integration tests skip themselves unless `CS_TEST_DB` is set, which `make test-all` handles.
They are not optional extras: they cover unique indexes and `ON CONFLICT` behaviour, which is
exactly the part a mock cannot tell you anything about.

`make test-sdk-e2e` expects `code_scout_flutter` checked out next to this repo. It is the only
test anywhere that proves the SDK and the dashboard still agree with each other, so it is worth
running if you touch the ingest endpoint or anything about the wire format.

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

## House rules worth knowing

**Never edit a `_templ.go` file.** They are generated from the `.templ` files next to them and get
overwritten on the next build. CI checks for this and will fail the build.

**Access checks live in one place.** Project scoped routes hang off a subrouter behind
`RequireProjectAccess`, so a handler cannot forget to check. A project the caller cannot see
answers 404 rather than 403, deliberately, so the existence of projects you are not in stays
private. Please do not add per-handler checks.

**Read `DESIGN.md` before changing anything visual.** Colours, spacing and type are all defined
there.

**Filter state goes in the query string.** Every control in the log viewer is a link to the view
you would have after clicking it. That is what makes a pasted URL reproduce a screen exactly, and
it is worth preserving.

## Reporting a bug

Please include what you did, what you expected, and what happened instead. For anything involving
ingest, the server log around the failure is usually the whole answer.

For security issues, see [SECURITY.md](SECURITY.md) and please do not open a public issue.

## Licence

By contributing you agree that your contribution is licensed under the MIT licence, the same as
the rest of the project.
