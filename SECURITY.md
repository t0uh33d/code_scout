# Security

## Reporting a vulnerability

Please do not open a public issue.

Use GitHub's [private vulnerability reporting](https://github.com/getcodescout/code_scout/security/advisories/new)
on this repository, or email **touheedkhan1408@gmail.com**.

Tell us what you found and how to reproduce it. You will get a reply within a few days. This is a
small project run by one person, so please be patient with the timeline, but the report will be
taken seriously.

## Supported versions

There is no tagged release yet. Fixes land on the tip of `main` and in the
`touheed10/code_scout:edge` Docker image, and upgrading is how a fix reaches you. `GET /healthz`
reports the version and commit an instance is running without a login, so a report can say exactly
what it was tested against.

## What is in scope

Code Scout runs on your own server, so there is no hosted service of ours to attack and no shared
tenancy to escape. What is left are the places where something outside the instance is trusted, and
those are the ones worth your time:

- **The ingest endpoint.** `POST /api/logs/dump` is authenticated with the project ID and secret
  the SDK sends on every upload. Anything that lets one project write into another, or lets an
  unauthenticated caller write at all, is in scope.

- **Who can see which project.** Every account has one instance role, and separately a level on
  each project it has been added to. A super admin sees every project without being a member of
  any, and that is the only role that skips membership; anyone else with no membership row has no
  access at all, including an admin who was never added. Deleting a project needs both maintainer
  on that project and the instance-wide Admin role. All of this is decided in one function,
  `ResolveProjectAccess` in `internal/domain/access.go`, and enforced by middleware on the
  `/project/{id}` subrouter, so a handler cannot forget the check. On the dashboard, a project you
  cannot see answers 404 and never 403, so you cannot learn that it exists. Anything that reaches
  another team's logs is in scope.

- **The live session socket.** A device gets onto `GET /api/live/socket` with the same project ID
  and secret it uses to upload, checked by middleware before any pairing frame is read, so a
  pairing code never decides whether a device may connect, only which session it joins. A code is
  six characters from `crypto/rand`, single use, dead after five minutes, and bound to the project
  that minted it. A code that pairs a device into another project's session, or one that survives
  being claimed, is worth reporting.

- **The live database browser.** While a device is paired, the dashboard can browse the app's own
  local storage and change one cell at a time, which makes it the most invasive feature in the
  product. Two properties hold it together. First, the dashboard never sends SQL: there are five
  structured operations and none of them has a field a statement could travel in, so read-only is
  a property of what can be expressed rather than a rule anyone enforces. Second, writing takes
  two gates: the dashboard side needs project-manage rights, the same bar as rotating a secret,
  and the app must have registered that database with `writable: true`. Nothing is browsable at
  all until the app calls `registerDatabase`. A path that smuggles a statement to the device,
  reads a database the app never registered, or writes past either gate is very much in scope.

- **Personal access tokens and the MCP endpoint.** `/api/mcp` lets a coding agent read a project
  over MCP, authenticated with `Authorization: Bearer csp_...`, a personal token minted on
  `/account`. Only a hex SHA-256 hash of the token is ever stored, in `personal_access_tokens`;
  the plaintext is shown once at minting. Tokens can be revoked on the same page, can carry an
  expiry, and are capped at 20 per account. A token grants exactly what its owner can already see:
  every tool resolves access through the same resolver the web router uses, and a malformed
  project id, an absent one and one the owner cannot read all answer "project not found",
  byte-identical. The eighteen tools are read-only by construction: each database-browser tool
  hardcodes its operation, no input carries an op, a statement or a value, and the rows limit is a
  server constant. A tool that can be made to write, or a token that reads a project its owner
  cannot, is in scope.

- **The dashboard session cookie and the login flow.** `cs_session` is a random token checked
  against the `user_sessions` table on every request, and it lasts thirty days. Anything that gets
  you a session without knowing a password, or keeps one working after a password change or a
  reset, is worth reporting.

## Things that are deliberate, and are not bugs

**Project secrets are stored in plain text.** The reason is that the SDK setup screen has to show
you the secret again, and re-reading it there is how most people set up a second app. The secret
is 32 characters drawn from a 62-character alphabet with `crypto/rand`, not something a person
chose, so a stored hash would buy less than it sounds like it would. The comparison on the ingest
path is a plain string equality rather than a constant-time one, which we are comfortable with for
a long random key arriving over a network and would not be for anything shorter. Treat a project
secret like an API key: rotate it if it leaks, which the settings screen supports. Rotation
replaces the secret in one transaction, so every installed build of your app starts failing
uploads immediately and keeps its logs locally until a build with the new secret ships. Both a
reveal and a rotate are logged at warn with the project id, so you can see afterwards that one
happened.

**The ingest path tells you which header was wrong.** An SDK sending a project ID that does not
exist gets a different error from one sending a wrong secret, both as HTTP 400, because a
developer wiring up a new app needs to know which of the two they got wrong. That means the ingest
endpoint is the one place the server will confirm to a caller with no credentials that a project
exists, so the dashboard's 404-never-403 rule deliberately does not extend here. Project IDs are
UUIDv7, which is worth being precise about: the first 48 bits are the creation time in
milliseconds, so an id is not uniformly random and knowing roughly when a project was made narrows
the space. What is left is 74 random bits, which is far past guessable, but if you find a way to
turn this endpoint into a practical oracle we want to hear about it.

**Nothing in a log is redacted unless your app names it.** A debugging tool that hides the
`Authorization` header is not much of a debugging tool, because that header is often the reason
the request is failing. So Code Scout hides nothing by default. Whatever you name in
`RedactionBehavior` is stripped on the device at capture, before anything reaches the phone's own
database and long before an upload, so a redacted value never existed anywhere the server could
see it. The flip side is that redaction is not retroactive: it only affects logs written after you
turn it on, and whatever already reached the dashboard stays there until retention removes it or
you delete the project. Decide this before pointing the SDK at production, not after. See
[the redaction guide](https://codescout.tech/docs/guides/redaction/).

**The daily cap can overshoot by up to one batch per concurrent upload.** Enforcement is check then
act with no row lock, on purpose, because locking would serialise every upload for a project onto
one row on the hot path. It is a volume control, not a billing boundary.

**`reset-password` needs nothing beyond a shell.** `code_scout reset-password --email=...` prints
a fresh temporary password once and signs that account out everywhere. Anyone who can run it can
take over any account, which is the point: the trust boundary is shell access to the machine the
server runs on. A finding that begins "given shell on the host" is not a finding.

**The MCP transport disables the SDK's localhost protection on purpose.** The Go MCP SDK refuses a
loopback connection carrying a public `Host` header, a DNS-rebinding guard for local servers that
have no authentication. This endpoint always has authentication, because the bearer-token
middleware sits in front of it, and a rebinding attack cannot carry the `Authorization` header. A
reverse proxy forwarding to 127.0.0.1 with the public Host is exactly the shape the guard refuses,
so `DisableLocalhostProtection: true` is load-bearing rather than a hardening flag someone forgot,
and `TestAProxiedHostHeaderIsNotRefused` fails if it comes back.

## Running it safely

- Register your own account before the instance is reachable from anywhere but your own machine.
  Until the first account exists, the login page is a signup page, and whoever fills it in becomes
  the super admin of every project on the instance.
- Put it behind TLS, and redirect plain HTTP rather than serving it. The session cookie is set
  `HttpOnly` and `SameSite=Lax` but not `Secure`, and there is no setting that adds the flag, so a
  single request over `http://` puts a thirty-day session token in the clear. Sending HSTS from
  the proxy closes that for good. The project secret and any personal access token travel on every
  request too.
- Change both passwords in `docker-compose.yml` before anyone else can reach the instance.
  `CS_DB_PASSWORD` and `POSTGRES_PASSWORD` ship as `change-me`, and they have to match.
- Set `CS_DB_SSLMODE=require` or stronger if the database is not on the same host.
- Put whatever rate limiting your proxy offers in front of `POST /api/auth/submit`. The server
  does not throttle login attempts, and it accepts a password as short as six characters, so pick
  a longer one.
- Treat a personal access token like a password for everything its owner can see. It tends to live
  in editor and agent configs for months, so mint it with an expiry when you can, and revoke it
  from `/account` the moment nothing uses it any more.
