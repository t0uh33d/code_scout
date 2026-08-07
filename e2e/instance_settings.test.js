// The point of the timezone setting is that it changes what people read, not
// what is stored. That is only observable in a rendered page, so it is checked
// here rather than in a Go test.

const { test, before, after } = require('node:test')
const assert = require('node:assert')
const { randomBytes } = require('node:crypto')

const { BASE, launch, linger, signIn, createProject, seedLogs } = require('./harness')

let browser, page, projectID, secret

// One log at a known instant, so the time on screen can be predicted exactly
// rather than merely "looks different". The SDK sends each log's timestamp, so
// this needs no database access.
const LOG_UTC_HOUR = 6
const LOG_UTC_MINUTE = 15

function knownInstant() {
  const d = new Date()
  d.setUTCHours(LOG_UTC_HOUR, LOG_UTC_MINUTE, 0, 0)
  return d
}

async function setDisplay(page, { tz, clock } = {}) {
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#timezone-form')
  if (tz) await page.selectOption('#timezone', tz)
  if (clock) await page.check(`#timezone-form input[name="time_format"][value="${clock}"]`)
  await page.click('#timezone-form button[type="submit"]')
  await page.waitForSelector('#timezone-form:has-text("Saved")', { timeout: 5000 })
}

const setTimezone = (page, tz) => setDisplay(page, { tz })

// Reads the timestamp of the seeded row out of the log viewer.
async function probeTimestamp(page) {
  await page.goto(`${BASE}/project/${projectID}/logs`)
  const row = page.locator('[data-log-row]', { hasText: 'Timezone probe' }).first()
  await row.waitFor({ timeout: 5000 })
  return (await row.locator('span').first().innerText()).trim()
}

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
  ;({ id: projectID, secret } = await createProject(page, 'Timezone E2E'))
  await seedLogs(projectID, secret, [{ message: 'Timezone probe', at: knownInstant() }])
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

test('the instance timezone changes the times the dashboard renders', async () => {
  await setTimezone(page, 'UTC')
  const utc = await probeTimestamp(page)
  assert.equal(utc, `0${LOG_UTC_HOUR}:${LOG_UTC_MINUTE}:00`,
    `expected the log to read in UTC, got ${utc}`)

  // +05:30 on purpose: a zone that is not a whole number of hours from UTC is
  // what broke the overview buckets, so it is the one worth exercising.
  await setTimezone(page, 'Asia/Kolkata')
  const kolkata = await probeTimestamp(page)
  assert.equal(kolkata, '11:45:00',
    `06:15 UTC should read 11:45 in Kolkata, got ${kolkata}`)

  // And the change survives a reload, i.e. it was persisted rather than held
  // in the response that saved it.
  await page.reload()
  const afterReload = await probeTimestamp(page)
  assert.equal(afterReload, '11:45:00', 'the timezone did not persist across a reload')
})

test('the timezone select shows the saved zone when the page is reopened', async () => {
  await setTimezone(page, 'Europe/London')
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#timezone')
  assert.equal(await page.inputValue('#timezone'), 'Europe/London')
})

// Members used to be its own screen. It is a tab now, so the switch has to
// replace the pane without a page load and put the tab in the address bar.
test('settings tabs switch in place and are linkable', async () => {
  await page.goto(`${BASE}/settings`)
  await page.waitForSelector('#instance-settings-body')

  // A super admin lands on General.
  await page.waitForSelector('#timezone-form')

  // A marker on window, not a framenavigated listener: hx-push-url calls
  // pushState, which Playwright also reports as a navigation. Only a real
  // document load clears this.
  await page.evaluate(() => { window.__tabProbe = 'alive' })

  await page.click('#instance-settings-body button:has-text("Members")')
  await page.waitForSelector('#members-table', { timeout: 5000 })

  const survived = await page.evaluate(() => window.__tabProbe)
  assert.equal(survived, 'alive', 'switching tabs should swap the pane, not reload the page')
  assert.ok(page.url().includes('tab=members'), `the tab should be in the URL, got ${page.url()}`)
  assert.equal(await page.locator('#timezone-form').count(), 0, 'the General pane should be gone')

  // The URL is real: loading it directly lands on the same tab.
  await page.goto(`${BASE}/settings?tab=members`)
  await page.waitForSelector('#members-table')
  assert.equal(await page.locator('#timezone-form').count(), 0)
})

// Members moved, so anything still pointing at the old address has to arrive
// somewhere useful rather than 404.
test('the old members address forwards into the tab', async () => {
  await page.goto(`${BASE}/members`)
  await page.waitForSelector('#members-table')
  assert.ok(page.url().includes('/settings?tab=members'), `got ${page.url()}`)
})

// Adding a member still works from inside the tab: the sheet lives on the page
// rather than in the swapped pane, so a tab switch cannot remove it.
test('the add-member sheet still opens from the Members tab', async () => {
  await page.goto(`${BASE}/settings?tab=members`)
  await page.click('button:has-text("Add member")')
  await page.waitForSelector('#member-sheet-body input[name="email"]', { timeout: 5000 })
})

test('a zone the server cannot load is refused with an inline error', async () => {
  await page.goto(`${BASE}/settings`)
  await page.waitForSelector('#timezone-form')

  // Bypasses the select, the way a hand-edited request would.
  const status = await page.evaluate(async () => {
    const body = new URLSearchParams({ timezone: 'Middle/Earth' })
    const res = await fetch('/settings/display', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded', 'HX-Request': 'true' },
      body,
    })
    return { code: res.status, html: await res.text() }
  })

  // 200 so htmx will actually swap the error into the page.
  assert.equal(status.code, 200, 'an error fragment must be 200 or htmx drops it')
  assert.ok(/not a timezone/i.test(status.html), `expected an inline error, got: ${status.html.slice(0, 200)}`)

  await page.goto(`${BASE}/settings`)
  await page.waitForSelector('#timezone')
  assert.equal(await page.inputValue('#timezone'), 'Europe/London',
    'a refused zone must not overwrite the saved one')
})

// The Retention and Limits cards. These are settings that actually enforce
// something, so the test that matters is the last one: changing the value in
// the UI changes what ingest accepts, with no restart.

async function saveCard(form, fields) {
  for (const [name, value] of Object.entries(fields)) {
    await page.fill(`${form} input[name="${name}"]`, String(value))
  }
  await page.click(`${form} button[type="submit"]`)
  await page.waitForSelector(`${form}:has-text("Saved")`)
}

test('retention saves and survives a reload', async () => {
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#retention-form')

  await saveCard('#retention-form', { retention_days: 90, purge_after_days: 14 })

  await page.goto(`${BASE}/settings?tab=general`)
  assert.equal(await page.inputValue('#retention-form input[name="retention_days"]'), '90')
  assert.equal(await page.inputValue('#retention-form input[name="purge_after_days"]'), '14')
})

test('a refused retention value comes back inline, with what was typed', async () => {
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#retention-form')

  // Hand-posted, because the browser's own number validation would stop this
  // ever reaching the server — and the server is the boundary that matters.
  const res = await page.request.post(`${BASE}/settings/retention`, {
    headers: { 'HX-Request': 'true', 'Content-Type': 'application/x-www-form-urlencoded' },
    data: 'retention_days=0&purge_after_days=7',
  })

  // 200 on purpose: htmx drops the body of a non-2xx response, so an error
  // sent as 400 would leave the card looking like nothing happened.
  assert.equal(res.status(), 200)
  const body = await res.text()
  assert.match(body, /between 1 and 3650/)
  // The rejected value stays on screen to be corrected.
  assert.match(body, /value="0"/)

  // And nothing was stored.
  await page.goto(`${BASE}/settings?tab=general`)
  assert.equal(await page.inputValue('#retention-form input[name="retention_days"]'), '90')
})

test('the upload cap is enforced, and changing it changes what ingest takes', async () => {
  const { id, secret } = await createProject(page, 'Limits E2E')

  // A batch that is comfortably fine at the default 50 MB.
  const ok = await seedLogs(id, secret, [{ message: 'under the cap', level: 'info' }])
  assert.ok(ok)

  // Drop the cap to the floor, then send something over it.
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#limits-form')
  await saveCard('#limits-form', { max_upload_mb: 1 })

  // Random hex, not repeated text: the upload is gzipped, and a repeated
  // string compresses to almost nothing, so a "large" payload built that way
  // sails under the cap and the test passes for the wrong reason.
  const big = Array.from({ length: 3000 }, (_, i) => ({
    message: `padding ${i} ${randomBytes(1024).toString('hex')}`,
    level: 'info',
  }))
  await assert.rejects(
    () => seedLogs(id, secret, big),
    err => {
      // 413, not 400: an SDK has to tell "too big, send fewer" apart from
      // "malformed", because only one of those is fixable by retrying smaller.
      assert.match(err.message, /413/, `want 413, got: ${err.message}`)
      return true
    },
  )

  // Put it back and the same upload is accepted again — no restart involved.
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#limits-form')
  await saveCard('#limits-form', { max_upload_mb: 50 })
  assert.ok(await seedLogs(id, secret, big))
})

test('a project over its daily cap is refused with a Retry-After', async () => {
  const { id, secret } = await createProject(page, 'Cap E2E')

  // Uncapped by default, so this lands.
  assert.ok(await seedLogs(id, secret, [{ message: 'first', level: 'info' }]))

  // Set a cap the project has already passed.
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#limits-form')
  await saveCard('#limits-form', { max_upload_mb: 50, daily_log_cap: 1000 })

  // Fill the day, then send one more.
  const CAPPED_SESSION = '33333333-3333-4333-8333-333333333333'
  const fill = Array.from({ length: 1200 }, (_, i) => ({
    message: `bulk ${i}`, level: 'info', sessionID: CAPPED_SESSION,
  }))
  const fillSessions = [{ id: CAPPED_SESSION, deviceModel: 'Pixel 7', osName: 'Android' }]

  await assert.rejects(
    () => seedLogs(id, secret, fill, fillSessions),
    err => {
      assert.equal(err.status, 429, `want 429, got ${err.status}`)
      // Delta-seconds, not an HTTP date: a phone's clock is routinely wrong.
      const wait = Number(err.retryAfter)
      assert.ok(Number.isInteger(wait) && wait >= 60,
        `want whole seconds of at least a minute, got ${err.retryAfter}`)
      assert.ok(wait <= 86400, `a daily cap should not park longer than a day, got ${wait}`)
      return true
    },
  )

  // Refused whole: nothing from that batch was stored.
  await page.goto(`${BASE}/project/${id}/logs`)
  const body = await page.locator('main').textContent()
  assert.ok(!body.includes('bulk 0'), 'a refused batch must be refused entirely')

  // And nothing else from it either. Sessions are upserted outside the logs'
  // transaction, so a cap checked too late would leave launches on the
  // Sessions screen with no logs under them — for an upload the server said it
  // did not take.
  await page.goto(`${BASE}/project/${id}/sessions`)
  const sessionRows = await page.locator('[data-session-row]').count()
  assert.equal(sessionRows, 0, 'a refused upload left session rows behind')

  // Lifting the cap lets the same batch through, with no restart.
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#limits-form')
  await saveCard('#limits-form', { max_upload_mb: 50, daily_log_cap: 0 })
  assert.ok(await seedLogs(id, secret, fill, fillSessions))
})

// The clock is one setting that has to reach every timestamp on every screen,
// which is the only reason it is worth having rather than a per-screen toggle.
test('the clock format changes what the dashboard renders, everywhere', async () => {
  await setDisplay(page, { tz: 'UTC', clock: '24h' })
  assert.equal(await probeTimestamp(page), `0${LOG_UTC_HOUR}:${LOG_UTC_MINUTE}:00`)

  await setDisplay(page, { clock: '12h' })
  const twelve = await probeTimestamp(page)
  assert.equal(twelve, `${LOG_UTC_HOUR}:${LOG_UTC_MINUTE}:00 AM`,
    `06:15 should read as a 12-hour morning time, got ${twelve}`)

  // Persisted, not just held in the response that saved it.
  await page.reload()
  assert.equal(await probeTimestamp(page), `${LOG_UTC_HOUR}:${LOG_UTC_MINUTE}:00 AM`,
    'the clock did not survive a reload')

  // A different format, rendered by a different template: the settings card's
  // own preview. One setting means every format, not only the log viewer's.
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#timezone-form')
  const preview = await page.locator('#timezone-form').innerText()
  assert.match(preview, /Right now that reads .*(AM|PM)/,
    `the settings preview ignored the clock: ${preview}`)

  // And the setting shows its own state when the page is reopened, or you
  // cannot tell which one you are on.
  assert.equal(
    await page.isChecked('#timezone-form input[name="time_format"][value="12h"]'), true,
    'the saved clock was not reflected back')

  await setDisplay(page, { clock: '24h' })
  assert.equal(await probeTimestamp(page), `0${LOG_UTC_HOUR}:${LOG_UTC_MINUTE}:00`,
    'switching back to 24-hour did not take')
})

// The About card. The version has to be visible somewhere a person can quote it
// in a bug report, and the update-check setting has to be genuinely switchable,
// because "it sends nothing" is a claim you can only take on trust if you can
// also turn it off.
test('the About card shows the version and the update setting persists', async () => {
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#about-card')

  const card = page.locator('#about-card')
  assert.match(await card.innerText(), /Version/)
  // Whatever app.Version currently is. Pinning the number here would mean
  // every release breaks a test that is not about the release.
  assert.match(await card.innerText(), /\d+\.\d+\.\d+/,
    'the About card shows no version at all')

  const box = card.locator('input[name="enabled"]')
  assert.equal(await box.isChecked(), true, 'update checks should default to on')

  // htmx swaps the card in place, so waiting for the checkbox to change state
  // is waiting for the response to have landed.
  await box.uncheck()
  await page.waitForSelector('#about-card input[name="enabled"]:not(:checked)')

  // Reopened, not just re-rendered: this is the assertion that fails if the
  // save is a no-op, which is exactly what a struct-form GORM update would do
  // with a false.
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#about-card')
  assert.equal(await page.isChecked('#about-card input[name="enabled"]'), false,
    'turning the update check off did not survive a reload')

  const off = await page.locator('#about-card').innerText()
  assert.match(off, /Update checks are off/)
  assert.doesNotMatch(off, /is available/,
    'it claims to know about releases while switched off')

  // And back, so the suite leaves the instance as it found it.
  await page.check('#about-card input[name="enabled"]')
  await page.waitForSelector('#about-card input[name="enabled"]:checked')
})

// The sidebar version is on every project screen, which is where someone
// actually is when they need it.
test('the version is in the chrome, not only in settings', async () => {
  await page.goto(`${BASE}/project/${projectID}/logs`)
  await page.waitForSelector('aside')

  assert.match(await page.locator('aside').innerText(), /v\d+\.\d+\.\d+/,
    'the project sidebar shows no version')
})
