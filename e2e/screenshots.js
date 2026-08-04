// Regenerates the screenshots in the README.
//
// Not hand-captured: a screenshot taken once drifts the moment the UI moves,
// and nobody notices because nobody re-opens the README. `make screenshots`
// seeds a throwaway instance through the real ingest endpoint and captures the
// live product, so refreshing them is one command rather than an afternoon.
//
// Run it with `make screenshots`, which supplies the server and database.

const { mkdirSync } = require('node:fs')
const { join } = require('node:path')
const { randomUUID } = require('node:crypto')

const { BASE, launch, signIn, createProject, seedLogs } = require('./harness')

// .github/assets, not docs/: docs/ is gitignored in this repo because it holds
// internal design notes, so anything the README points at has to live outside it.
const OUT = join(__dirname, '..', '.github', 'assets', 'screenshots')

// A believable slice of a real app rather than lorem: the value of the product
// is that these rows tell a story, and a screenshot of "test log 1..20" shows
// none of it.
const SESSION = randomUUID()
const INSTALL = randomUUID()
const PAY = randomUUID()
const CART = randomUUID()
const PROFILE = randomUUID()

const ago = ms => new Date(Date.now() - ms)

function story() {
  const s = (message, level, at, extra = {}) =>
    ({ message, level, at, sessionID: SESSION, ...extra })

  return [
    s('App launched', 'info', ago(9 * 60_000), { tags: ['lifecycle'] }),
    s('Signing in', 'debug', ago(8.5 * 60_000), { tags: ['auth'] }),
    s('User signed in', 'info', ago(8.4 * 60_000), { tags: ['auth'], metadata: { method: 'oauth' } }),
    s('Cache lookup: hit', 'debug', ago(8 * 60_000), { tags: ['cache'] }),

    // A GET that answered.
    s('Network Request', 'debug', ago(7 * 60_000), {
      tags: ['network'], network: true, requestID: CART, callPhase: 'request',
      metadata: { method: 'GET', url: 'https://api.shop.dev/v2/cart', headers: { accept: 'application/json' } },
    }),
    s('Network Response', 'debug', ago(7 * 60_000 - 118), {
      tags: ['network'], network: true, requestID: CART, callPhase: 'response',
      metadata: { status_code: 200, body: { items: 3, subtotal_cents: 4999 } },
    }),

    s('Checkout started', 'info', ago(6 * 60_000), { tags: ['checkout'] }),

    // A 401, which is the interesting kind of success.
    s('Network Request', 'debug', ago(5 * 60_000), {
      tags: ['network'], network: true, requestID: PROFILE, callPhase: 'request',
      metadata: { method: 'GET', url: 'https://api.shop.dev/v2/user/profile' },
    }),
    s('Network Response', 'debug', ago(5 * 60_000 - 96), {
      tags: ['network'], network: true, requestID: PROFILE, callPhase: 'response',
      metadata: { status_code: 401, body: { error: 'token_expired' } },
    }),
    s('Token expired, refreshing', 'warning', ago(4.9 * 60_000), { tags: ['auth'] }),

    // And the one that failed, with a redacted header so the marker shows.
    s('Network Request', 'debug', ago(3 * 60_000), {
      tags: ['network', 'payments'], network: true, requestID: PAY, callPhase: 'request',
      metadata: {
        method: 'POST', url: 'https://api.shop.dev/v2/payments/confirm',
        headers: { 'content-type': 'application/json', authorization: '[redacted]' },
        body: { order_id: 'ord_8812f', amount_cents: 4999 },
      },
    }),
    s('Network Error', 'error', ago(2.5 * 60_000), {
      tags: ['network', 'payments'], network: true, requestID: PAY, callPhase: 'error',
      metadata: {
        type: 'DioExceptionType.receiveTimeout',
        message: 'Receiving data timed out after 30000ms',
      },
    }),
    s('Payment declined', 'error', ago(2.4 * 60_000), {
      tags: ['checkout', 'payments'],
      error: 'PaymentException: card_declined',
      stackTrace: [
        { index: 0, method: 'PaymentService.confirm', path: 'lib/payments/service.dart', line: 88, column: 12 },
        { index: 1, method: 'CheckoutBloc._onConfirm', path: 'lib/checkout/bloc.dart', line: 141, column: 7 },
      ],
      metadata: { order: 'ord_8812f', total: 49.99, currency: 'GBP' },
    }),
    s('Checkout abandoned', 'warning', ago(2 * 60_000), { tags: ['checkout'] }),
    s('Unrecoverable state in cart', 'fatal', ago(90_000), { tags: ['checkout'] }),
  ]
}

const sessions = () => ([{
  id: SESSION,
  installationID: INSTALL,
  userID: 'ada@example.com',
  deviceModel: 'Pixel 7',
  osName: 'Android',
  osVersion: '14',
  appVersion: '3.11.2',
  buildNumber: '418',
  metadata: { plan: 'pro' },
  startedAt: ago(10 * 60_000),
  lastSeenAt: ago(60_000),
}])

async function shoot(page, name) {
  // A settle beat: the charts animate in, and catching one mid-transition looks
  // like a rendering bug rather than a product.
  await page.waitForTimeout(600)
  await page.screenshot({ path: join(OUT, `${name}.png`) })
  console.log(`  ${name}.png`)
}

async function main() {
  mkdirSync(OUT, { recursive: true })

  // 1440x900 at 2x: wide enough for the network split to show both columns, and
  // retina so the type is not mushy on a GitHub page.
  const { browser, page } = await launch({ width: 1440, height: 900 })
  await page.setViewportSize({ width: 1440, height: 900 })

  await signIn(page)
  const project = await createProject(page, 'Shop')
  await seedLogs(project.id, project.secret, story(), sessions())

  console.log('Capturing:')

  await page.goto(`${BASE}/project/${project.id}/overview`)
  await page.waitForSelector('h1')
  await shoot(page, 'overview')

  await page.goto(`${BASE}/project/${project.id}/logs`)
  await page.waitForSelector('[data-log-row]')
  await shoot(page, 'logs')

  await page.goto(`${BASE}/project/${project.id}/network?rid=${PAY}`)
  await page.waitForSelector('[data-network-row]')
  await shoot(page, 'network')

  await page.goto(`${BASE}/project/${project.id}/errors`)
  await page.waitForSelector('h1')
  await shoot(page, 'errors')

  await page.goto(`${BASE}/project/${project.id}/sessions`)
  await page.waitForSelector('h1')
  await shoot(page, 'sessions')

  await browser.close()
  console.log(`\nWritten to .github/assets/screenshots/`)
}

main().catch(err => {
  console.error(err)
  process.exit(1)
})
