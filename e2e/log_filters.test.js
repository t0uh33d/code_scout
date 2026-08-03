// The log viewer's filters. Every control is a link to a modified query
// string, so these tests are as much about the URL as about the rows.

const { test, before, after } = require('node:test')
const assert = require('node:assert')

const { BASE, launch, linger, signIn, createProject, seedLogs } = require('./harness')

let browser, page, projectID, secret

const rows = () => page.locator('[data-log-row]')

async function open(query) {
  const url = query
    ? `${BASE}/project/${projectID}/logs?q=${encodeURIComponent(query)}`
    : `${BASE}/project/${projectID}/logs`
  await page.goto(url)
  await page.waitForSelector('[data-log-row], #log-rows')
}

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
  ;({ id: projectID, secret } = await createProject(page, 'Filters E2E'))

  const now = Date.now()
  const min = n => new Date(now - n * 60_000)
  await seedLogs(projectID, secret, [
    { message: 'boom one', level: 'error', tags: ['checkout'], at: min(1) },
    { message: 'boom two', level: 'error', tags: ['checkout', 'heartbeat'], at: min(2) },
    { message: 'the end', level: 'fatal', tags: ['checkout'], at: min(3) },
    { message: 'careful', level: 'warning', tags: ['heartbeat'], at: min(4) },
    { message: 'just so you know', level: 'info', tags: ['analytics'], at: min(5) },
    // No tags at all: the one that vanishes if an exclusion is written the
    // obvious way, since NOT(NULL) is not true.
    { message: 'untagged and easily lost', level: 'info', at: min(6) },
    // Old enough to fall outside a one-hour window.
    { message: 'ancient history', level: 'info', tags: ['analytics'], at: min(180) },
    {
      message: 'GET https://api.test.dev/v2/cart',
      level: 'debug',
      tags: ['network'],
      network: true,
      metadata: { method: 'GET', url: 'https://api.test.dev/v2/cart' },
      at: min(7),
    },
  ])
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

test('level toggles narrow, combine and clear', async () => {
  await open('')
  assert.equal(await rows().count(), 8, 'every log should show when nothing is filtered')

  // Clicking one level when nothing is filtered narrows to it, rather than
  // hiding it — a filter control should filter.
  await page.click('[data-level="error"]')
  await page.waitForURL(/q=level%3Aerror/)
  assert.equal(await rows().count(), 2)

  // A second level ORs in.
  await page.click('[data-level="fatal"]')
  await page.waitForFunction(() => document.querySelectorAll('[data-log-row]').length === 3)
  assert.ok(page.url().includes('level%3Aerror'), `error should still be on: ${page.url()}`)

  // Turning the last one off means unfiltered, not an empty screen.
  await page.click('[data-level="error"]')
  await page.click('[data-level="fatal"]')
  await page.waitForFunction(() => document.querySelectorAll('[data-log-row]').length === 8)
})

test('a tag chip cycles include, exclude, neutral', async () => {
  await open('')
  const chip = () => page.locator('[data-tag="heartbeat"]')

  assert.equal(await chip().getAttribute('data-state'), 'neutral')

  await chip().click()
  await page.waitForURL(/q=tag%3Aheartbeat/)
  assert.equal(await chip().getAttribute('data-state'), 'included')
  assert.equal(await rows().count(), 2, 'only heartbeat logs')

  await chip().click()
  await page.waitForURL(/-tag%3Aheartbeat/)
  assert.equal(await chip().getAttribute('data-state'), 'excluded')

  // The untagged log must survive an exclusion. This is the case the obvious
  // SQL gets wrong, and it is invisible unless you look for it.
  const messages = await rows().allInnerTexts()
  assert.ok(messages.some(m => m.includes('untagged and easily lost')),
    `an untagged log disappeared when excluding a tag: ${messages.join(' | ')}`)
  assert.equal(await rows().count(), 6)

  await chip().click()
  await page.waitForFunction(() => document.querySelectorAll('[data-log-row]').length === 8)
  assert.equal(await chip().getAttribute('data-state'), 'neutral')
})

test('include and exclude combine', async () => {
  await open('')
  await page.click('[data-tag="checkout"]')
  await page.waitForFunction(() => document.querySelectorAll('[data-log-row]').length === 3)

  const heartbeat = page.locator('[data-tag="heartbeat"]')
  await heartbeat.click() // include
  await heartbeat.click() // exclude
  await page.waitForFunction(() => document.querySelectorAll('[data-log-row]').length === 2)

  const messages = await rows().allInnerTexts()
  assert.ok(!messages.some(m => m.includes('boom two')),
    'the log carrying both tags should be excluded')
})

test('the time window filters and toggles off', async () => {
  await open('')
  await page.click('[data-window="1h"]')
  await page.waitForURL(/last%3A1h/)

  const messages = await rows().allInnerTexts()
  assert.ok(!messages.some(m => m.includes('ancient history')),
    'a three-hour-old log should fall outside the last hour')

  // Clicking the active window clears it.
  await page.click('[data-window="1h"]')
  await page.waitForFunction(() => document.querySelectorAll('[data-log-row]').length === 8)
})

test('network only shows network calls, rendered as calls', async () => {
  await open('')
  await page.click('[data-filter="network"]')
  await page.waitForURL(/is%3Anetwork/)
  assert.equal(await rows().count(), 1)

  // Not the raw message: a method, a path, and the direction of the phase.
  const text = await rows().first().innerText()
  assert.ok(text.includes('GET'), `expected the method, got: ${text}`)
  assert.ok(text.includes('/v2/cart'), `expected the path, got: ${text}`)
  assert.ok(!text.includes('https://api.test.dev'),
    `the host should be trimmed so paths line up, got: ${text}`)
})

// The whole point of putting filter state in the URL.
test('a filtered view survives being pasted into a new tab', async () => {
  await open('')
  await page.click('[data-level="error"]')
  await page.click('[data-tag="checkout"]')
  await page.waitForFunction(() => document.querySelectorAll('[data-log-row]').length === 2)
  const shared = page.url()

  const other = await page.context().newPage()
  try {
    await other.goto(shared)
    await other.waitForSelector('[data-log-row]')
    assert.equal(await other.locator('[data-log-row]').count(), 2,
      'the pasted URL should reproduce the same rows')
    assert.equal(await other.locator('[data-tag="checkout"]').getAttribute('data-state'), 'included',
      'and the controls should show the same state')
    assert.equal(await other.locator('[data-level="error"]').getAttribute('aria-pressed'), 'true')
  } finally {
    await other.close()
  }
})

// A typo used to answer 400 and throw away the whole screen, including the box
// you would fix it in.
test('a bad query explains itself without losing the page', async () => {
  await open('level:banana')
  assert.ok(await page.locator('text=invalid level').count() > 0, 'expected the parser complaint')
  assert.equal(await rows().count(), 8, 'the unfiltered list should still be there')
  assert.equal(await page.inputValue('#log-search'), 'level:banana',
    'the broken query should stay in the box to be corrected')
})

// WCAG 2.1.1: the expanded detail carries the error, stack trace and metadata,
// so it cannot be mouse-only.
test('log rows expand from the keyboard', async () => {
  await open('')
  await page.keyboard.press('j')

  const focusedIsRow = await page.evaluate(() => document.activeElement?.hasAttribute('data-log-row'))
  assert.equal(focusedIsRow, true, 'j should move focus onto a log row')

  assert.equal(await page.evaluate(() => document.activeElement.getAttribute('aria-expanded')), 'false')
  await page.keyboard.press('Enter')
  assert.equal(await page.evaluate(() => document.activeElement.getAttribute('aria-expanded')), 'true',
    'Enter should expand the focused row')

  const detailVisible = await page.evaluate(
    () => !document.activeElement.nextElementSibling.classList.contains('hidden'))
  assert.equal(detailVisible, true, 'the detail panel should be shown')

  // k walks back up.
  const first = await page.evaluate(() => document.activeElement.textContent.trim())
  await page.keyboard.press('j')
  await page.keyboard.press('k')
  const back = await page.evaluate(() => document.activeElement.textContent.trim())
  assert.equal(back, first, 'k should return to the previous row')
})

// Reading a few screens of logs and then narrowing the search should not mean
// scrolling back up to find the box you are narrowing it in.
test('the list scrolls under the toolbar, not the toolbar with it', async () => {
  // Its own project with enough rows to overflow: the shared one is a handful
  // of logs, which would pass by never scrolling at all.
  const long = await createProject(page, 'Long list')
  await seedLogs(long.id, long.secret,
    Array.from({ length: 120 }, (_, i) => ({ message: `row ${i}` })))

  await page.goto(`${BASE}/project/${long.id}/logs`)
  await page.waitForSelector('[data-log-row]')

  const box = page.locator('#log-search')
  const before = await box.boundingBox()

  const rows = page.locator('#log-rows')
  await rows.evaluate(el => { el.scrollTop = el.scrollHeight })
  await page.waitForFunction(() => document.querySelector('#log-rows').scrollTop > 0)

  const after = await box.boundingBox()
  assert.equal(Math.round(after.y), Math.round(before.y),
    'the search box moved when the list scrolled')

  // And the page itself is not what scrolled — otherwise the rows would have
  // gone nowhere and this would pass for the wrong reason.
  const pageScrolled = await page.evaluate(() => window.scrollY)
  assert.equal(pageScrolled, 0, 'the window scrolled instead of the list')

  // The level filters stay reachable too, which is the other half of the point.
  assert.ok(await page.locator('a:has-text("Error")').first().isVisible(),
    'the filter row scrolled out of view')
})

// A project with a long tail of tags must not push the log list off screen.
test('only the busiest tags get a chip, the rest fold away', async () => {
  const many = await createProject(page, 'Many tags')
  const logs = []
  for (let i = 0; i < 25; i++) {
    logs.push({ message: `tagged ${i}`, tags: [`tag-${String(i).padStart(2, '0')}`] })
  }
  await seedLogs(many.id, many.secret, logs)

  await page.goto(`${BASE}/project/${many.id}/logs`)
  await page.waitForSelector('#log-search')

  // Visible, not present: the folded chips are in the DOM inside the closed
  // <details>, which is what makes expanding instant and needs no request.
  const chips = await page.locator('[data-tag]:visible').count()
  assert.ok(chips <= 10, `expected at most 10 chips before expanding, got ${chips}`)

  const more = page.locator('details summary', { hasText: 'more' })
  assert.equal(await more.count(), 1, 'no disclosure for the remaining tags')

  await more.click()
  const expanded = await page.locator('[data-tag]:visible').count()
  assert.equal(expanded, 25, `expanding should reveal every tag, got ${expanded}`)
})

