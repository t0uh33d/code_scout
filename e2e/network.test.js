// The Network screen. The value is entirely in pairing three log rows back
// into one call, so these tests are about what collapses together, what state
// each call ends up in, and that inspecting one never navigates away.

const { test, before, after } = require('node:test')
const assert = require('node:assert')

const { BASE, launch, linger, signIn, createProject, seedLogs } = require('./harness')

let browser, page, projectID, secret

const rows = () => page.locator('[data-network-row]')

async function openNetwork(query = '') {
  await page.goto(`${BASE}/project/${projectID}/network${query}`)
  // The toolbar, not just any form: the account menu holds a hidden logout
  // form that matches first and never becomes visible.
  await page.waitForSelector('input[name="path"]')
}

const CART = '10000000-0000-4000-8000-000000000001'
const PAY = '10000000-0000-4000-8000-000000000002'
const PROFILE = '10000000-0000-4000-8000-000000000003'
const PRODUCTS = '10000000-0000-4000-8000-000000000004'

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
  ;({ id: projectID, secret } = await createProject(page, 'Network E2E'))

  const now = Date.now()
  const at = ms => new Date(now - ms)
  const call = (requestID, phase, extra) => ({
    message: `Network ${phase}`,
    level: phase === 'error' ? 'error' : 'debug',
    network: true,
    requestID,
    callPhase: phase,
    tags: ['network'],
    ...extra,
  })

  await seedLogs(projectID, secret, [
    // Complete: a GET that answered in 250ms.
    call(CART, 'request', {
      at: at(60_000),
      metadata: {
        method: 'GET', url: 'https://api.test.dev/v2/cart',
        headers: { 'content-type': 'application/json', accept: '*/*' },
        body: null,
      },
    }),
    call(CART, 'response', {
      at: at(59_750),
      metadata: {
        status_code: 200,
        headers: { 'content-type': 'application/json' },
        body: { items: 3, subtotal_cents: 4999 },
        request: { method: 'GET', url: 'https://api.test.dev/v2/cart' },
      },
    }),
    // Failed: a POST that timed out.
    call(PAY, 'request', {
      at: at(50_000),
      metadata: {
        method: 'POST', url: 'https://api.test.dev/v2/payments/confirm',
        headers: { 'content-type': 'application/json' },
        body: { order_id: 'ord_8812f', amount_cents: 4999 },
      },
    }),
    call(PAY, 'error', {
      at: at(20_000),
      metadata: {
        type: 'DioExceptionType.receiveTimeout',
        message: 'Receiving data timed out after 30000ms',
        request: { method: 'POST', url: 'https://api.test.dev/v2/payments/confirm' },
      },
    }),
    // Completed, but with a status worth noticing.
    call(PROFILE, 'request', {
      at: at(40_000),
      metadata: { method: 'GET', url: 'https://api.test.dev/v2/user/profile' },
    }),
    call(PROFILE, 'response', {
      at: at(39_900),
      metadata: {
        status_code: 401,
        body: { error: 'token_expired' },
        request: { method: 'GET', url: 'https://api.test.dev/v2/user/profile' },
      },
    }),
    // Pending: a request the app never saw the end of.
    call(PRODUCTS, 'request', {
      at: at(30_000),
      metadata: { method: 'GET', url: 'https://api.test.dev/v2/products?page=2' },
    }),
    // Not a network call at all.
    { message: 'user tapped checkout', level: 'info', at: at(45_000) },
  ])
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

test('three phase logs become one call, and the states are distinguishable', async () => {
  await openNetwork()

  assert.equal(await rows().count(), 4, 'seven network logs are four calls')

  const text = await page.locator('table').first().textContent()
  assert.ok(!text.includes('user tapped checkout'), 'a plain log is not a call')

  const cart = rows().filter({ hasText: '/v2/cart' }).first()
  assert.match(await cart.textContent(), /GET/)
  assert.match(await cart.textContent(), /200/)
  assert.match(await cart.textContent(), /250ms/)

  assert.match(await rows().filter({ hasText: '/v2/payments/confirm' }).first().textContent(), /error/)
  assert.match(await rows().filter({ hasText: '/v2/user/profile' }).first().textContent(), /401/)

  // A pending call has no duration: timing it against now would grow on every
  // refresh.
  const pending = rows().filter({ hasText: '/v2/products' }).first()
  assert.match(await pending.textContent(), /pending/)
  assert.match(await pending.textContent(), /—/)

  // Every row draws a waterfall bar.
  assert.equal(await page.locator('[data-network-row] span[style*="width"]').count(), 4)
})

test('selecting a call inspects it without navigating away', async () => {
  await openNetwork()

  // A marker that only survives if the page is never reloaded.
  await page.evaluate(() => { window.__stillHere = true })

  await rows().filter({ hasText: '/v2/payments/confirm' }).first().click()
  await page.waitForFunction(
    () => document.querySelector('#network-detail')?.textContent.includes('payments/confirm'))

  assert.equal(await page.evaluate(() => window.__stillHere), true,
    'inspecting a call reloaded the page')

  // The selection still reaches the address bar, so the view is shareable.
  assert.ok(page.url().includes(`rid=${PAY}`), `the URL did not follow: ${page.url()}`)

  const detail = page.locator('#network-detail')
  assert.match(await detail.textContent(), /DioExceptionType\.receiveTimeout/)
  assert.match(await detail.textContent(), /Receiving data timed out/)
})

// The obvious thing to do next, and the thing nothing covered: click a second
// row. Inspecting one call and then another is the whole point of a list.
test('clicking a second call swaps to it', async () => {
  await openNetwork()

  await rows().filter({ hasText: '/v2/cart' }).first().click()
  await page.waitForFunction(
    () => document.querySelector('#network-detail')?.textContent.includes('/v2/cart'))

  await page.evaluate(() => { window.__stillHere = true })

  await rows().filter({ hasText: '/v2/user/profile' }).first().click()
  await page.waitForFunction(
    () => document.querySelector('#network-detail')?.textContent.includes('/v2/user/profile'),
    null, { timeout: 4000 })

  const detail = await page.locator('#network-detail').textContent()
  assert.ok(!detail.includes('/v2/cart'), 'the first call is still in the pane')
  assert.equal(await page.evaluate(() => window.__stillHere), true,
    'the second click reloaded the page')
  assert.ok(page.url().includes(`rid=${PROFILE}`), `the URL did not follow: ${page.url()}`)
})

// Two calls to the same endpoint, which is what the example app produces when
// you tap "dio GET" and then "http GET". They are indistinguishable in the
// table, so the only way to tell the pane changed is the duration.
test('two calls to the same path are separately inspectable', async () => {
  const A = '10000000-0000-4000-8000-00000000000a'
  const B = '10000000-0000-4000-8000-00000000000b'
  const now = Date.now()
  const same = (rid, phase, ms, extra) => ({
    message: `Network ${phase}`,
    level: 'debug',
    network: true,
    requestID: rid,
    callPhase: phase,
    tags: ['network'],
    at: new Date(now - ms),
    metadata: extra,
  })

  // Its own project. Seeding into the shared one would change the counts every
  // other test in this file asserts on.
  const twin = await createProject(page, 'Network twins')
  await seedLogs(twin.id, twin.secret, [
    same(A, 'request', 30_000, { method: 'GET', url: 'https://jsonplaceholder.typicode.com/posts/1' }),
    same(A, 'response', 29_833, { status_code: 200, body: { id: 1, marker: 'first-call' } }),
    same(B, 'request', 20_000, { method: 'GET', url: 'https://jsonplaceholder.typicode.com/posts/1' }),
    same(B, 'response', 19_679, { status_code: 200, body: { id: 1, marker: 'second-call' } }),
  ])

  await page.goto(`${BASE}/project/${twin.id}/network`)
  await page.waitForSelector('input[name="path"]')
  assert.equal(await rows().count(), 2)

  await rows().nth(0).click()
  await page.waitForFunction(
    () => document.querySelector('#network-detail')?.textContent.includes('marker'))
  assert.match(await page.locator('#network-detail').textContent(), /second-call/,
    'the newest call is first in the list')

  // The highlight is the only way a person can tell these two rows apart, so
  // it is the thing that has to move. The pane swapping underneath while the
  // list still points at the old row reads as "the click did nothing".
  assert.equal(await rows().nth(0).getAttribute('aria-selected'), 'true',
    'the clicked row is not marked selected')

  await rows().nth(1).click()
  await page.waitForFunction(
    () => document.querySelector('#network-detail')?.textContent.includes('first-call'),
    null, { timeout: 4000 })

  assert.equal(await rows().nth(1).getAttribute('aria-selected'), 'true',
    'the highlight did not follow the second click')
  assert.equal(await rows().nth(0).getAttribute('aria-selected'), null,
    'the first row is still highlighted')
})

// The pane holds twenty-five response headers on a real call. If it grows to
// fit them the page scroll bar becomes the pane's, and the toolbar and the list
// scroll away while you read.
test('the inspector scrolls inside itself, not the page', async () => {
  await openNetwork(`?rid=${CART}`)

  const scrolls = await page.evaluate(() => {
    const body = document.querySelector('#network-detail > .overflow-y-auto')
    return { found: body !== null, page: document.body.scrollHeight <= window.innerHeight + 2 }
  })
  assert.ok(scrolls.found, 'the pane has no scroll container of its own')
  assert.ok(scrolls.page, 'the page itself scrolls, so the pane grew instead')
})

test('the inspector can be dismissed, and stays dismissed', async () => {
  await openNetwork(`?rid=${CART}`)
  assert.match(await page.locator('#network-detail').textContent(), /subtotal_cents/)

  await page.click('[data-dismiss-inspector]')
  await page.waitForFunction(
    () => document.querySelector('#network-detail')?.textContent.includes('Select a call'))

  // An absent rid means "no opinion" and opens the newest call, so closing has
  // to say so explicitly or a reload reopens what you just closed.
  assert.ok(page.url().includes('rid='), `closing lost its marker: ${page.url()}`)
  await page.reload()
  await page.waitForSelector('input[name="path"]')
  assert.match(await page.locator('#network-detail').textContent(), /Select a call/,
    'the reload reopened the call that was dismissed')
})

// A response body is the thing you want in a bug report or a curl command, and
// selecting it out of a scrolling <pre> by hand is miserable.
test('the payload and the response can be copied', async () => {
  const context = page.context()
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: BASE })

  // The response body.
  await openNetwork(`?rid=${CART}`)
  await page.click('[data-copy]')
  await page.waitForFunction(
    () => document.querySelector('[data-copy-label]')?.textContent === 'Copied')

  let copied = await page.evaluate(() => navigator.clipboard.readText())
  assert.match(copied, /subtotal_cents/, `the response body was not copied: ${copied}`)
  assert.ok(!copied.includes('Response body'),
    'the section heading came along with the body')

  // And the request body, on its own tab.
  await openNetwork(`?rid=${PAY}&tab=payload`)
  await page.click('[data-copy]')
  await page.waitForFunction(
    () => document.querySelector('[data-copy-label]')?.textContent === 'Copied')

  copied = await page.evaluate(() => navigator.clipboard.readText())
  assert.match(copied, /ord_8812f/, `the payload was not copied: ${copied}`)

  // The label goes back, or the second copy looks like it did nothing.
  await page.waitForFunction(
    () => document.querySelector('[data-copy-label]')?.textContent === 'Copy',
    null, { timeout: 4000 })
})

// The pane is replaced by every row click and every tab. A listener bound to
// the button would die with the first swap.
test('copying still works after the inspector has been swapped', async () => {
  const context = page.context()
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: BASE })

  await openNetwork()
  await rows().filter({ hasText: '/v2/user/profile' }).first().click()
  await page.waitForFunction(
    () => document.querySelector('#network-detail')?.textContent.includes('token_expired'))

  await page.click('[data-copy]')
  await page.waitForFunction(
    () => document.querySelector('[data-copy-label]')?.textContent === 'Copied')

  const copied = await page.evaluate(() => navigator.clipboard.readText())
  assert.match(copied, /token_expired/, `copying died with the swap: ${copied}`)
})

test('only the tabs with something behind them are offered', async () => {
  // A failed call: a POST body earns Payload, and there is no Response.
  await openNetwork(`?rid=${PAY}`)
  let tabs = await page.locator('[data-phase-tab]').allTextContents()
  assert.deepEqual(tabs, ['Headers', 'Payload', 'Error'])
  // The error is why you clicked, so it opens on it.
  assert.equal(await page.locator('[data-phase-tab][aria-current="page"]').textContent(), 'Error')

  // A GET that answered: nothing went out, so no Payload.
  await openNetwork(`?rid=${CART}`)
  tabs = await page.locator('[data-phase-tab]').allTextContents()
  assert.deepEqual(tabs, ['Headers', 'Response'])
  assert.equal(await page.locator('[data-phase-tab][aria-current="page"]').textContent(), 'Response')

  // A pending call has only what was asked for.
  await openNetwork(`?rid=${PRODUCTS}`)
  tabs = await page.locator('[data-phase-tab]').allTextContents()
  assert.deepEqual(tabs, ['Headers'])
})

test('switching tabs swaps the pane and shows that tab', async () => {
  await openNetwork(`?rid=${CART}`)
  assert.match(await page.locator('#network-detail').textContent(), /subtotal_cents/)

  await page.click('[data-phase-tab="headers"]')
  await page.waitForFunction(
    () => document.querySelector('[data-phase-tab="headers"][aria-current="page"]') !== null)

  const detail = await page.locator('#network-detail').textContent()
  // Both directions on one screen, which is the point of splitting them out.
  assert.match(detail, /Request headers/)
  assert.match(detail, /Response headers/)
  assert.match(detail, /accept/)
  assert.ok(!detail.includes('subtotal_cents'), 'the response body belongs to its own tab')
})

test('a selection survives a reload, filter and all', async () => {
  await openNetwork(`?path=cart&rid=${CART}&tab=headers`)

  assert.equal(await rows().count(), 1, 'the path filter should still apply')
  assert.match(await page.locator('#network-detail').textContent(), /Request headers/)
  assert.equal(await page.locator('[data-phase-tab][aria-current="page"]').textContent(), 'Headers')
})

test('the toolbar filters by path, method and status', async () => {
  await openNetwork()

  await page.fill('input[name="path"]', 'cart')
  await Promise.all([page.waitForURL(/path=cart/), page.click('[data-network-toolbar] button[type="submit"]')])
  assert.equal(await rows().count(), 1)

  await openNetwork()
  await page.selectOption('select[name="method"]', 'GET')
  await Promise.all([page.waitForURL(/method=GET/), page.click('[data-network-toolbar] button[type="submit"]')])
  assert.equal(await rows().count(), 3, 'three of the four calls are GETs')

  // "failed" is both kinds: a bad status, and a call that never answered.
  await openNetwork()
  await page.selectOption('select[name="status"]', 'failed')
  await Promise.all([page.waitForURL(/status=failed/), page.click('[data-network-toolbar] button[type="submit"]')])
  assert.equal(await rows().count(), 2)

  await Promise.all([page.waitForURL(/\/network$/), page.click('a:has-text("Clear")')])
  assert.equal(await rows().count(), 4)
})

test('the sidebar reaches the network screen', async () => {
  await page.goto(`${BASE}/project/${projectID}/overview`)
  await Promise.all([
    page.waitForURL(/\/network$/),
    page.click('nav a:has-text("Network")'),
  ])
  assert.equal(await page.locator('nav a[aria-current="page"]').textContent(), 'Network')
})
