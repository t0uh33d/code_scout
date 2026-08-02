// The Errors screen. The value of the screen is entirely in the grouping, so
// these tests are about which occurrences collapse together and which do not.

const { test, before, after } = require('node:test')
const assert = require('node:assert')

const { BASE, launch, linger, signIn, createProject, seedLogs } = require('./harness')

let browser, page, projectID, secret

const groups = () => page.locator('[data-error-group]')

async function openErrors() {
  await page.goto(`${BASE}/project/${projectID}/errors`)
  await page.waitForSelector('[data-error-group], h1')
}

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
  ;({ id: projectID, secret } = await createProject(page, 'Errors E2E'))

  const now = Date.now()
  const min = n => new Date(now - n * 60_000)
  const sessionA = '11111111-1111-4111-8111-111111111111'
  const sessionB = '22222222-2222-4222-8222-222222222222'

  await seedLogs(projectID, secret, [
    // One bug, three ids, two sessions. This is the case the whole screen
    // exists for: without grouping it is three rows that look unrelated.
    { message: 'User 4821 not found', level: 'error', sessionID: sessionA, at: min(30) },
    { message: 'User 9134 not found', level: 'error', sessionID: sessionA, at: min(20) },
    {
      message: 'User 7 not found',
      level: 'error',
      sessionID: sessionB,
      at: min(2),
      stackTrace: [
        { index: 0, method: 'CartBloc._resolveDiscount', path: 'package:ledger/cart/bloc.dart', line: 212, column: 31 },
      ],
      tags: ['checkout'],
    },
    // Two endpoints failing. Both carry the literal message the SDK sends, so
    // grouping on the message alone would make these one meaningless row.
    {
      message: 'Network Error',
      level: 'error',
      network: true,
      metadata: { method: 'POST', url: 'https://api.test.dev/v2/pay' },
      at: min(10),
    },
    {
      message: 'Network Error',
      level: 'error',
      network: true,
      metadata: { method: 'GET', url: 'https://api.test.dev/v2/cart' },
      at: min(9),
    },
    // Not a problem, so not on this screen.
    { message: 'User signed in', level: 'info', at: min(5) },
  ])
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

test('one bug with many ids is one row, and two endpoints are two', async () => {
  await openErrors()

  assert.equal(await groups().count(), 3,
    'want the user lookup, and one row per failing endpoint')

  const top = groups().first()
  assert.match(await top.textContent(), /User 7 not found/,
    'the row should show a real message, not the normalised key')
  assert.match(await top.locator('[data-error-count]').textContent(), /×3/)
  assert.match(await top.textContent(), /2 sessions/)

  const text = await page.locator('#log-rows, main').textContent()
  assert.ok(!text.includes('User signed in'), 'an info log is not a problem')
})

test('a row expands to its latest stack trace', async () => {
  await openErrors()

  const top = groups().first()
  assert.equal(await top.locator('pre').isVisible(), false, 'starts collapsed')

  await top.locator('summary').click()
  await top.locator('pre').waitFor({ state: 'visible' })
  assert.match(await top.locator('pre').textContent(), /#0 {2}CartBloc\._resolveDiscount/)
})

test('view in logs finds every occurrence, not just the one on the row', async () => {
  await openErrors()

  const top = groups().first()
  await top.locator('summary').click()
  await Promise.all([
    page.waitForURL(/\/logs\?q=/),
    top.locator('a:has-text("View in logs")').click(),
  ])
  await page.waitForSelector('[data-log-row]')

  // Three different ids in the message. A text search for the row's own
  // message would have found exactly one of them.
  assert.equal(await page.locator('[data-log-row]').count(), 3)

  // And the filter survives being filtered on top of, because the fingerprint
  // is in the query string like every other control.
  await page.click('[data-level="error"]')
  await page.waitForSelector('[data-log-row]')
  assert.ok(page.url().includes('fingerprint'), `fingerprint was dropped: ${page.url()}`)
  assert.equal(await page.locator('[data-log-row]').count(), 3)
})

test('the overview links its recent errors to the same groups', async () => {
  await page.goto(`${BASE}/project/${projectID}/overview`)
  await page.waitForSelector('[data-recent-error]')

  const rows = page.locator('[data-recent-error]')
  assert.equal(await rows.count(), 3)
  assert.match(await rows.first().textContent(), /User 7 not found/)

  await Promise.all([
    page.waitForURL(/\/logs\?q=/),
    rows.first().click(),
  ])
  await page.waitForSelector('[data-log-row]')
  assert.equal(await page.locator('[data-log-row]').count(), 3)
})

test('the sidebar can reach the errors screen', async () => {
  await page.goto(`${BASE}/project/${projectID}/logs`)
  await Promise.all([
    page.waitForURL(/\/errors$/),
    page.click('nav a:has-text("Errors")'),
  ])
  assert.equal(await page.locator('nav a[aria-current="page"]').textContent(), 'Errors')
})
