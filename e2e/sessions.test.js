// The Sessions and Devices screens. Sessions arrive as a second tar entry in
// the same upload the logs come in, so these tests also prove that half of the
// wire contract before the SDK sends it.

const { test, before, after } = require('node:test')
const assert = require('node:assert')

const { BASE, launch, linger, signIn, createProject, seedLogs } = require('./harness')

let browser, page, projectID, secret

// Fixed ids so the assertions can name what they are looking at.
const PHONE = 'e7c1b8a2-40aa-4f12-9c07-3d5581bb40aa'
const IPHONE = 'b2049100-91cd-4a0b-8f21-77c0c3a11190'
const S1 = '4f2a81b0-0000-4000-8000-000000000001'
const S2 = '4f2a81b0-0000-4000-8000-000000000002'
const S3 = '9c073d55-0000-4000-8000-000000000003'

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
  ;({ id: projectID, secret } = await createProject(page, 'Sessions E2E'))

  const now = Date.now()
  const min = n => new Date(now - n * 60_000)

  await seedLogs(
    projectID,
    secret,
    [
      { message: 'started up', level: 'info', sessionID: S1, at: min(120) },
      { message: 'checkout failed', level: 'error', sessionID: S1, at: min(119) },
      { message: 'and again', level: 'fatal', sessionID: S1, at: min(118) },
      { message: 'GET /v2/cart', level: 'debug', sessionID: S1, at: min(117), network: true,
        metadata: { method: 'GET', url: 'https://api.test.dev/v2/cart' } },
      { message: 'quiet launch', level: 'info', sessionID: S3, at: min(20) },
    ],
    [
      // Two launches of the same phone. The first was identified, the second
      // never called setUser() — the device should still remember the user.
      { id: S1, installationID: PHONE, userID: 'u_8812', deviceModel: 'Pixel 7',
        osName: 'Android', osVersion: '14', appVersion: '3.11.2', buildNumber: '418',
        startedAt: min(120), lastSeenAt: min(115) },
      { id: S2, installationID: PHONE, deviceModel: 'Pixel 7',
        osName: 'Android', osVersion: '14', appVersion: '3.11.2', buildNumber: '418',
        startedAt: min(60), lastSeenAt: min(59) },
      { id: S3, installationID: IPHONE, deviceModel: 'iPhone 15 Pro',
        osName: 'iOS', osVersion: '17.4', appVersion: '3.10.0', buildNumber: '402',
        startedAt: min(20), lastSeenAt: min(18) },
    ],
  )
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

test('every launch is a row, with its own counts', async () => {
  await page.goto(`${BASE}/project/${projectID}/sessions`)
  await page.waitForSelector('[data-session-row]')

  const rows = page.locator('[data-session-row]')
  assert.equal(await rows.count(), 3)

  // Newest first, so the most recent launch is the one you land on.
  assert.match(await rows.first().textContent(), /iPhone 15 Pro/)

  const busy = rows.filter({ hasText: 'u_8812' }).first()
  const cells = await busy.locator('td').allTextContents()
  assert.match(cells.join(' | '), /Pixel 7/)
  // 4 logs, of which a fatal and an error are both errors.
  assert.match(cells[4], /4/)
  assert.match(cells[5], /2/)

  // A launch that never called setUser() says so rather than inventing one.
  assert.match(await rows.filter({ hasText: 'anonymous' }).first().textContent(), /anonymous/)
})

test('a session row opens that launch, and its device', async () => {
  await page.goto(`${BASE}/project/${projectID}/sessions`)
  await page.waitForSelector('[data-session-row]')

  await Promise.all([
    page.waitForURL(new RegExp(`/session/${S1}`)),
    page.locator(`a[href*="${S1}"]`).first().click(),
  ])

  await page.goto(`${BASE}/project/${projectID}/sessions`)
  await Promise.all([
    page.waitForURL(new RegExp(`/device/${PHONE}`)),
    page.locator(`a[href*="/device/${PHONE}"]`).first().click(),
  ])
})

test('one phone across many launches is one device row', async () => {
  await page.goto(`${BASE}/project/${projectID}/devices`)
  await page.waitForSelector('[data-device-row]')

  const rows = page.locator('[data-device-row]')
  assert.equal(await rows.count(), 2, 'two launches of one phone are still one device')

  const phone = rows.filter({ hasText: 'Pixel 7' }).first()
  const cells = await phone.locator('td').allTextContents()
  assert.match(cells[0], /Pixel 7 · Android 14/)
  assert.match(cells[1], /3\.11\.2\+418/)
  // The last launch was anonymous, but the device remembers who was on it.
  assert.match(cells[2], /u_8812/)
  assert.match(cells[3], /2/, 'two sessions')
  assert.match(cells[4], /2/, 'two errors, summed across the install')
})

test('device detail lists that phone every time it ran', async () => {
  await page.goto(`${BASE}/project/${projectID}/device/${PHONE}`)
  await page.waitForSelector('[data-device-session]')

  const body = await page.locator('main').textContent()
  assert.match(body, /Pixel 7/)
  assert.match(body, /Android 14/)
  assert.match(body, new RegExp(PHONE))

  const sessions = page.locator('[data-device-session]')
  assert.equal(await sessions.count(), 2, 'only this phone’s launches')
  assert.match(await sessions.first().textContent(), /3\.11\.2\+418/)

  // Back out to the list.
  await Promise.all([
    page.waitForURL(/\/devices$/),
    page.click('a:has-text("Back to devices")'),
  ])
})

test('the sidebar reaches both screens', async () => {
  await page.goto(`${BASE}/project/${projectID}/overview`)
  await Promise.all([
    page.waitForURL(/\/sessions$/),
    page.click('nav a:has-text("Sessions")'),
  ])
  assert.equal(await page.locator('nav a[aria-current="page"]').textContent(), 'Sessions')

  await Promise.all([
    page.waitForURL(/\/devices$/),
    page.click('nav a:has-text("Devices")'),
  ])
  assert.equal(await page.locator('nav a[aria-current="page"]').textContent(), 'Devices')
})

test('the overview sessions tile goes to the sessions screen', async () => {
  await page.goto(`${BASE}/project/${projectID}/overview`)
  // The tile, specifically — not the sidebar link that also says "Sessions".
  const tile = page.locator('a[data-stat="Sessions"]')
  await tile.waitFor()
  await Promise.all([
    page.waitForURL(/\/sessions$/),
    tile.click(),
  ])
})

test('a session opens on two tabs, with times measured from the launch', async () => {
  await page.goto(`${BASE}/project/${projectID}/session/${S1}`)

  // The journey strip this replaced had nowhere to put a status or a duration,
  // so both kinds are now tables with the columns each actually needs.
  await page.waitForSelector('[data-session-tab="logs"]')
  await page.waitForSelector('[data-session-tab="network"]')

  // Counts on both tabs whichever one is showing: a count you cannot see until
  // you click is not a count.
  const logsTab = await page.locator('[data-session-tab="logs"]').textContent()
  const netTab = await page.locator('[data-session-tab="network"]').textContent()
  assert.match(logsTab, /Logs \(\d+\)/, `logs tab had no count: ${logsTab}`)
  assert.match(netTab, /Network \(\d+\)/, `network tab had no count: ${netTab}`)

  // Every row says how long after launch it happened. That is the question a
  // session view exists to answer, and an absolute clock alone makes you do
  // the arithmetic.
  const body = await page.content()
  assert.match(body, /\+\d+\.\d{3}s|\+\d+:\d{2}/, 'no row reported its distance from launch')

  // The tab is in the URL, so the link you send opens where you were.
  await Promise.all([
    page.waitForURL(/tab=network/),
    page.locator('[data-session-tab="network"]').click(),
  ])
  assert.match(await page.locator('[data-session-tab="network"]').getAttribute('aria-current'), /true/)
})
