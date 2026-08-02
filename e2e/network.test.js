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

test('only the phases that were recorded get a tab', async () => {
  // A failed call has a request and an error, and no response.
  await openNetwork(`?rid=${PAY}`)
  let tabs = await page.locator('[data-phase-tab]').allTextContents()
  assert.deepEqual(tabs, ['Request', 'Error'])
  // The error is why you clicked, so it opens on it.
  assert.equal(await page.locator('[data-phase-tab][aria-current="page"]').textContent(), 'Error')

  // A completed call has a request and a response.
  await openNetwork(`?rid=${CART}`)
  tabs = await page.locator('[data-phase-tab]').allTextContents()
  assert.deepEqual(tabs, ['Request', 'Response'])
  assert.equal(await page.locator('[data-phase-tab][aria-current="page"]').textContent(), 'Response')

  // A pending call has only the request.
  await openNetwork(`?rid=${PRODUCTS}`)
  tabs = await page.locator('[data-phase-tab]').allTextContents()
  assert.deepEqual(tabs, ['Request'])
})

test('switching tabs swaps the pane and shows that phase', async () => {
  await openNetwork(`?rid=${CART}`)
  assert.match(await page.locator('#network-detail').textContent(), /subtotal_cents/)

  await page.click('[data-phase-tab="request"]')
  await page.waitForFunction(
    () => document.querySelector('[data-phase-tab="request"][aria-current="page"]') !== null)

  const detail = await page.locator('#network-detail').textContent()
  assert.match(detail, /Request headers/)
  assert.match(detail, /accept/)
  assert.ok(!detail.includes('subtotal_cents'), 'the response body should be gone')
})

test('a selection survives a reload, filter and all', async () => {
  await openNetwork(`?path=cart&rid=${CART}&tab=request`)

  assert.equal(await rows().count(), 1, 'the path filter should still apply')
  assert.match(await page.locator('#network-detail').textContent(), /Request headers/)
  assert.equal(await page.locator('[data-phase-tab][aria-current="page"]').textContent(), 'Request')
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
