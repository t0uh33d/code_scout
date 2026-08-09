// Shared setup for the browser tests: sign in once, make a project, and hand
// back a page that is already authenticated.
//
// The server and its database are started by `make test-e2e`, not from here, so
// a failing test never leaves a half-migrated database behind.

const { chromium } = require('playwright')
const { execFileSync } = require('node:child_process')
const { randomUUID } = require('node:crypto')
const { mkdtempSync, writeFileSync, readFileSync, rmSync } = require('node:fs')
const { tmpdir } = require('node:os')
const { join } = require('node:path')

const BASE = process.env.CS_E2E_BASE || 'http://localhost:24283'
const EMAIL = 'e2e@test.local'
const PASSWORD = 'e2e-password-123'

// Drives the Chrome already installed rather than downloading Playwright's own
// build, which keeps `npm i` from pulling ~150MB.
//
// Returns an explicit context, not a bare page: pages made with
// browser.newPage() get an implicit context that cannot open a second page, and
// a test that needs one (a different viewport, say) would lose the session
// cookie and land on /login.
// CS_E2E_HEADED=1 opens a real window so you can watch the run. It also slows
// each action down, because at full speed a passing test is a blur — override
// with CS_E2E_SLOWMO (milliseconds per action, 0 for full speed).
// contextOptions is merged into the browser context — screenshots.js passes
// deviceScaleFactor: 2 so captured type is not mushy when the marketing site
// renders it at half size on a retina display.
async function launch(viewport = { width: 1440, height: 768 }, contextOptions = {}) {
  const headed = process.env.CS_E2E_HEADED === '1'
  const slowMo = process.env.CS_E2E_SLOWMO !== undefined
    ? Number(process.env.CS_E2E_SLOWMO)
    : (headed ? 300 : 0)

  const browser = await chromium.launch({ channel: 'chrome', headless: !headed, slowMo })
  const context = await browser.newContext({ viewport, ...contextOptions })
  return { browser, context, page: await context.newPage() }
}

// linger keeps a headed window on screen for a moment after the last assertion,
// which otherwise closes the instant the run finishes. No-op when headless.
async function linger(page, ms = 2000) {
  if (process.env.CS_E2E_HEADED === '1') await page.waitForTimeout(ms)
}

// signIn registers on a fresh instance (the first account is the super admin) or
// logs in if the instance already has one. Both post the same form.
async function signIn(page) {
  await page.goto(BASE + '/login')
  const isFirstRun = await page.locator('input[name="confirm_password"]').count() > 0

  await page.fill('input[name="email"]', EMAIL)
  await page.fill('input[name="password"]', PASSWORD)
  if (isFirstRun) {
    await page.fill('input[name="name"]', 'E2E')
    await page.fill('input[name="confirm_password"]', PASSWORD)
  }
  await Promise.all([page.waitForURL(BASE + '/'), page.click('button[type="submit"]')])
}

// createProject drives the real wizard rather than posting to the API, so the
// sheet's own behaviour is covered by every test that needs a project.
//
// Returns the credentials too, because the wizard's last step is the only place
// the plaintext secret is ever shown and seeding needs it.
async function createProject(page, name) {
  await page.goto(BASE + '/')
  await page.click('button:has-text("Add")')
  await page.waitForSelector('#project-sheet input[name="name"]')
  await page.fill('#project-sheet input[name="name"]', name)
  await page.click('#project-sheet button[type="submit"]')
  await page.waitForSelector('#project-wizard:has-text("is ready")')

  const [id, secret] = await page.locator('#project-wizard input[readonly]')
    .evaluateAll(els => els.map(e => e.value))
  await page.click('#project-wizard button:has-text("Done")')
  return { id, secret }
}

// seedLogs uploads through the real ingest endpoint — the same tar.gz, the same
// multipart field, the same auth headers the Flutter SDK uses.
//
// Writing straight to Postgres would be marginally simpler, but this way every
// test that needs data also proves the wire contract still works: the headers,
// the archive, the JSON shape and the idempotent insert. That contract is
// published and a break in it is a break for real users, so it is worth
// exercising continuously rather than only when someone remembers to.
//
// The SDK sends each log's own timestamp, which is what makes this usable for
// tests about time — backdating a log needs no database access.
async function seedLogs(projectID, secret, logs, sessions) {
  const payload = logs.map(l => ({
    // Fresh per call. Ingest is idempotent on client_id, so reusing ids across
    // tests would silently insert nothing and look like a broken query.
    id: l.id || randomUUID(),
    session_id: l.sessionID || randomUUID(),
    level: l.level || 'info',
    message: l.message,
    error: l.error ?? null,
    stack_trace: l.stackTrace ?? null,
    metadata: l.metadata ?? null,
    tags: l.tags ?? null,
    timestamp: (l.at instanceof Date ? l.at : new Date()).toISOString(),
    is_network_call: l.network ? 1 : 0,
    request_id: l.requestID ?? null,
    call_phase: l.callPhase ?? null,
  }))

  // Sessions ride in a second tar entry, exactly as the SDK will send them.
  // The server reads entries by name, so a batch with no sessions is still a
  // valid batch — which is what the published SDK sends today.
  const sessionPayload = (sessions || []).map(s => ({
    id: s.id,
    installation_id: s.installationID ?? null,
    user_id: s.userID ?? null,
    device_model: s.deviceModel ?? null,
    os_name: s.osName ?? null,
    os_version: s.osVersion ?? null,
    app_version: s.appVersion ?? null,
    build_number: s.buildNumber ?? null,
    // Left out entirely when not given, rather than sent as null, so a test can
    // seed a session the way an SDK older than the field would.
    ...(s.sdkVersion ? { sdk_version: s.sdkVersion } : {}),
    metadata: s.metadata ?? null,
    started_at: (s.startedAt instanceof Date ? s.startedAt : new Date()).toISOString(),
    last_seen_at: (s.lastSeenAt instanceof Date ? s.lastSeenAt : new Date()).toISOString(),
  }))

  const dir = mkdtempSync(join(tmpdir(), 'cs-e2e-'))
  try {
    const entries = ['data.json']
    writeFileSync(join(dir, 'data.json'), JSON.stringify(payload))
    if (sessionPayload.length > 0) {
      writeFileSync(join(dir, 'sessions.json'), JSON.stringify(sessionPayload))
      entries.push('sessions.json')
    }
    // System tar rather than an npm package: it is already here, and the
    // archive shape is the point of the test, not the library that made it.
    execFileSync('tar', ['-czf', join(dir, 'logs.tar.gz'), '-C', dir, ...entries])

    const body = new FormData()
    body.append('file', new Blob([readFileSync(join(dir, 'logs.tar.gz'))]), 'logs.tar.gz')

    const res = await fetch(`${BASE}/api/logs/dump`, {
      method: 'POST',
      headers: { 'X-Project-ID': projectID, 'X-Project-Secret': secret },
      body,
    })
    if (!res.ok) {
      const err = new Error(`ingest failed: ${res.status} ${await res.text()}`)
      // Exposed so a test can assert on the refusal itself rather than on the
      // shape of a message.
      err.status = res.status
      err.retryAfter = res.headers.get('retry-after')
      throw err
    }
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
  return payload
}

module.exports = { BASE, EMAIL, PASSWORD, launch, linger, signIn, createProject, seedLogs }
