// The overview's time range, and the empty state it used to get wrong.
//
// The bug worth pinning: the "nothing has reported yet" screen was gated on the
// count in the current window, so a project with months of history that was
// quiet overnight was told to install the SDK, and lost its tiles, its chart
// and its errors to say so.

const { test, before, after } = require('node:test')
const assert = require('node:assert')

const { BASE, launch, linger, signIn, createProject, seedLogs } = require('./harness')

let browser, page, busy, quiet, fresh

const ago = ms => new Date(Date.now() - ms)
const HOUR = 3600_000
const DAY = 24 * HOUR

async function openOverview(id, query = '') {
  await page.goto(`${BASE}/project/${id}/overview${query}`)
  await page.waitForSelector('h1')
}

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)

  // Logged in the last hour: busy in every range.
  busy = await createProject(page, 'Overview Busy')
  await seedLogs(busy.id, busy.secret, [
    { message: 'App launched', level: 'info', at: ago(30 * 60_000) },
    { message: 'Checkout failed', level: 'error', at: ago(25 * 60_000) },
    { message: 'Retrying', level: 'warning', at: ago(20 * 60_000) },
  ])

  // Logged ten days ago and nothing since: empty at 24h, present at 30d, and
  // the case the old condition got wrong.
  quiet = await createProject(page, 'Overview Quiet')
  await seedLogs(quiet.id, quiet.secret, [
    { message: 'Ancient history', level: 'info', at: ago(10 * DAY) },
    { message: 'Also ancient', level: 'error', at: ago(10 * DAY + HOUR) },
  ])

  // Never logged anything.
  fresh = await createProject(page, 'Overview Fresh')
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

test('a project that has never reported is told how to start', async () => {
  await openOverview(fresh.id)

  const body = await page.locator('main').textContent()
  assert.match(body, /Nothing has reported yet/)
  assert.match(body, /Show me the snippet/)

  // No range control: there is nothing to range over yet.
  assert.equal(await page.locator('[data-range]').count(), 0,
    'a project with no data was offered a time range')
})

test('a quiet window is not mistaken for a project that never reported', async () => {
  // Ten day old logs, so the default 24 hour window is empty.
  await openOverview(quiet.id)

  const body = await page.locator('main').textContent()
  assert.ok(!body.includes('Nothing has reported yet'),
    'a project with logs was told it has never reported')
  assert.ok(!body.includes('Show me the snippet'),
    'a working project was sent to the SDK setup page')

  // It says the window is empty, and offers the way out.
  assert.match(body, /Nothing was logged in last 24 hours/)
  assert.match(body, /Try a longer range/)

  // The tiles survive, at zero.
  assert.ok(await page.locator('[data-stat="Total logs"]').count() > 0,
    'the tiles went away on a quiet window')
})

test('a longer range finds the logs the short one could not', async () => {
  await openOverview(quiet.id)
  const short = await page.locator('[data-stat="Total logs"]').textContent()
  assert.match(short, /\b0\b/, `expected zero in the 24h window, got: ${short}`)

  await Promise.all([
    page.waitForURL(/range=30d/),
    page.click('[data-range="30d"]'),
  ])

  const long = await page.locator('[data-stat="Total logs"]').textContent()
  assert.match(long, /\b2\b/, `expected both logs in the 30d window, got: ${long}`)

  const body = await page.locator('main').textContent()
  assert.ok(!body.includes('Nothing was logged in'),
    'the 30 day window still reported itself empty')
})

test('the range is in the URL, so it survives a reload and can be shared', async () => {
  await openOverview(busy.id, '?range=7d')

  assert.equal(await page.locator('[data-range="7d"]').getAttribute('aria-current'), 'page',
    'the range in the URL did not select its tab')
  assert.match(await page.locator('main').textContent(), /Activity · last 7 days/)

  // And a nonsense range falls back rather than erroring, because anyone can
  // edit a query string.
  await openOverview(busy.id, '?range=nonsense')
  assert.equal(await page.locator('[data-range="24h"]').getAttribute('aria-current'), 'page')
})

test('the labels follow the range', async () => {
  await openOverview(busy.id)
  let body = await page.locator('main').textContent()
  assert.match(body, /Activity · last 24 hours/)
  assert.match(body, /vs yesterday/)

  await Promise.all([page.waitForURL(/range=7d/), page.click('[data-range="7d"]')])
  body = await page.locator('main').textContent()
  assert.match(body, /vs the previous 7 days/)
  assert.ok(!body.includes('vs yesterday'),
    'a week-long window still claimed a day-over-day delta')
})

// Retention decides what is honest to offer: a window longer than we keep draws
// a cliff and then flat ground, and nothing on the screen says why.
test('a range past retention is not offered', async () => {
  await page.goto(`${BASE}/settings?tab=retention`)
  await page.waitForSelector('input[name="retention_days"]')
  await page.fill('input[name="retention_days"]', '7')
  await Promise.all([
    page.waitForResponse(r => r.url().includes('/settings/retention') && r.request().method() === 'POST'),
    page.click('form:has(input[name="retention_days"]) button[type="submit"]'),
  ])

  await openOverview(busy.id)
  assert.equal(await page.locator('[data-range="30d"]').count(), 0,
    'a 30 day range was offered on a 7 day retention')
  assert.ok(await page.locator('[data-range="7d"]').count() > 0,
    'the 7 day range was dropped on a 7 day retention')

  // Put it back, since every other file in this suite shares the instance.
  await page.goto(`${BASE}/settings?tab=retention`)
  await page.waitForSelector('input[name="retention_days"]')
  await page.fill('input[name="retention_days"]', '30')
  await Promise.all([
    page.waitForResponse(r => r.url().includes('/settings/retention') && r.request().method() === 'POST'),
    page.click('form:has(input[name="retention_days"]) button[type="submit"]'),
  ])
})
