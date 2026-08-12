// The token pane's whole contract is visible only in a browser: the plaintext
// appears in the response to the create POST and in no other response, and a
// revoke needs a confirm. The Go tests prove what is stored; this proves what
// a person actually sees — on /account, the personal settings screen, which
// every role has and which is deliberately separate from /settings.

const { test, before, after } = require('node:test')
const assert = require('node:assert')

const { BASE, launch, linger, signIn } = require('./harness')

let browser, page

const TOKEN_SHAPE = /csp_[A-Za-z0-9_-]{43}/

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

test('the tokens tab mints once, hides forever, and revokes', async () => {
  await page.goto(`${BASE}/account`)
  await page.waitForSelector('#tokens-pane')

  // Mint one.
  await page.fill('#token-name', 'e2e laptop')
  await page.click('#tokens-pane button[type="submit"]')
  const reveal = page.locator('#tokens-pane input[readonly]')
  await reveal.waitFor({ timeout: 5000 })
  const plaintext = await reveal.inputValue()
  assert.match(plaintext, TOKEN_SHAPE, 'the reveal card should carry a whole token')

  // A fresh GET of the same page must not show it again, only the tail.
  await page.goto(`${BASE}/account`)
  await page.waitForSelector('#tokens-pane')
  const body = await page.locator('#tokens-pane').innerText()
  assert.ok(!TOKEN_SHAPE.test(body), 'a reload still shows the full token')
  assert.ok(body.includes('csp_…' + plaintext.slice(-4)),
    'the list should show the display suffix')

  // Revoke it. hx-confirm asks through window.confirm, so accept the dialog.
  page.once('dialog', (d) => d.accept())
  await page.click('#tokens-pane tbody button:has-text("Revoke")')
  await page.waitForSelector('#tokens-pane:has-text("No tokens yet")', { timeout: 5000 })
})

test('the account menu reaches personal settings, and instance settings stays impersonal', async () => {
  await page.goto(`${BASE}/`)
  await page.click('#account-menu summary')
  await page.click('#account-menu a:has-text("Personal settings")')
  await page.waitForSelector('#tokens-pane', { timeout: 5000 })
  assert.ok(page.url().includes('/account'), 'the menu should land on /account')

  // The tokens tab must not have survived on the instance screen.
  await page.goto(`${BASE}/settings`)
  await page.waitForSelector('#instance-settings-body')
  const tabs = await page.locator('#instance-settings-body button').allInnerTexts()
  assert.ok(!tabs.some((t) => t.includes('API tokens')),
    `instance settings still shows a tokens tab: ${tabs}`)
})

test('the password tab swaps in the voluntary change form', async () => {
  await page.goto(`${BASE}/account`)
  await page.waitForSelector('#account-body')
  await page.click('#account-body button:has-text("Password")')
  await page.waitForSelector('input[name="current_password"]', { timeout: 5000 })
  // Deliberately no submission: the e2e suite shares one account, and
  // changing its password here would strand every test after this one.
})
