// The point of the timezone setting is that it changes what people read, not
// what is stored. That is only observable in a rendered page, so it is checked
// here rather than in a Go test.

const { test, before, after } = require('node:test')
const assert = require('node:assert')
const { execFileSync } = require('node:child_process')

const { BASE, launch, linger, signIn, createProject } = require('./harness')

let browser, page, projectID

// One log at a known instant, so the time on screen can be predicted exactly
// rather than merely "looks different".
const LOG_UTC_HOUR = 6
const LOG_UTC_MINUTE = 15

function seedLogAtKnownTime(projectID) {
  // The trailing AT TIME ZONE 'UTC' is load-bearing. date_trunc on a naive
  // timestamp yields a naive timestamp, and inserting that into timestamptz
  // makes Postgres read it in the session's zone — which stored 06:15 as
  // 00:45 UTC and made this test fail against correct code.
  const sql = `
    INSERT INTO logs (id, created_at, updated_at, project_id, session_id, level, message, time_stamp, is_network_call)
    VALUES (gen_random_uuid(), now(), now(), '${projectID}', gen_random_uuid(), 'info', 'Timezone probe',
            (date_trunc('day', now() AT TIME ZONE 'UTC')
              + interval '${LOG_UTC_HOUR} hours' + interval '${LOG_UTC_MINUTE} minutes') AT TIME ZONE 'UTC', false);`
  execFileSync('psql', [
    '-h', process.env.CS_DB_HOST, '-p', process.env.CS_DB_PORT,
    '-U', process.env.CS_DB_USER, '-d', process.env.CS_DB_NAME, '-q', '-c', sql,
  ], { env: { ...process.env, PGPASSWORD: process.env.CS_DB_PASSWORD }, stdio: 'pipe' })
}

async function setTimezone(page, tz) {
  await page.goto(`${BASE}/settings?tab=general`)
  await page.waitForSelector('#timezone-form')
  await page.selectOption('#timezone', tz)
  await page.click('#timezone-form button[type="submit"]')
  await page.waitForSelector('#timezone-form:has-text("Saved")', { timeout: 5000 })
}

// Reads the timestamp of the seeded row out of the log viewer.
async function probeTimestamp(page) {
  await page.goto(`${BASE}/project/${projectID}/logs`)
  const row = page.locator('#log-rows > div[class*="grid"]', { hasText: 'Timezone probe' }).first()
  await row.waitFor({ timeout: 5000 })
  return (await row.locator('span').first().innerText()).trim()
}

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
  projectID = await createProject(page, 'Timezone E2E')
  seedLogAtKnownTime(projectID)
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
    const res = await fetch('/settings/timezone', {
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
