// Shared setup for the browser tests: sign in once, make a project, and hand
// back a page that is already authenticated.
//
// The server and its database are started by `make test-e2e`, not from here, so
// a failing test never leaves a half-migrated database behind.

const { chromium } = require('playwright')

const BASE = process.env.CS_E2E_BASE || 'http://localhost:24283'
const EMAIL = 'e2e@test.local'
const PASSWORD = 'e2e-password-123'

// Drives the Chrome already installed rather than downloading Playwright's own
// build, which keeps `npm i` from pulling ~150MB.
//
// Returns an explicit context, not a bare page: pages made with
// browser.newPage() get an implicit context that cannot open a second page, and
// a test that needs one (a different viewport, say) would lose the session
// cookie and land on /login.
// CS_E2E_HEADED=1 opens a real window so you can watch the run. It also slows
// each action down, because at full speed a passing test is a blur — override
// with CS_E2E_SLOWMO (milliseconds per action, 0 for full speed).
async function launch(viewport = { width: 1440, height: 768 }) {
  const headed = process.env.CS_E2E_HEADED === '1'
  const slowMo = process.env.CS_E2E_SLOWMO !== undefined
    ? Number(process.env.CS_E2E_SLOWMO)
    : (headed ? 300 : 0)

  const browser = await chromium.launch({ channel: 'chrome', headless: !headed, slowMo })
  const context = await browser.newContext({ viewport })
  return { browser, context, page: await context.newPage() }
}

// linger keeps a headed window on screen for a moment after the last assertion,
// which otherwise closes the instant the run finishes. No-op when headless.
async function linger(page, ms = 2000) {
  if (process.env.CS_E2E_HEADED === '1') await page.waitForTimeout(ms)
}

// signIn registers on a fresh instance (the first account is the super admin) or
// logs in if the instance already has one. Both post the same form.
async function signIn(page) {
  await page.goto(BASE + '/login')
  const isFirstRun = await page.locator('input[name="confirm_password"]').count() > 0

  await page.fill('input[name="email"]', EMAIL)
  await page.fill('input[name="password"]', PASSWORD)
  if (isFirstRun) {
    await page.fill('input[name="name"]', 'E2E')
    await page.fill('input[name="confirm_password"]', PASSWORD)
  }
  await Promise.all([page.waitForURL(BASE + '/'), page.click('button[type="submit"]')])
}

// createProject drives the real wizard rather than posting to the API, so the
// sheet's own behaviour is covered by every test that needs a project.
async function createProject(page, name) {
  await page.goto(BASE + '/')
  await page.click('button:has-text("Add")')
  await page.waitForSelector('#project-sheet input[name="name"]')
  await page.fill('#project-sheet input[name="name"]', name)
  await page.click('#project-sheet button[type="submit"]')
  await page.waitForSelector('#project-wizard:has-text("is ready")')

  // The wizard shows the id in a readonly field; that is the only place the
  // page states it in full.
  const id = await page.inputValue('#project-wizard input[readonly]')
  await page.click('#project-wizard button:has-text("Done")')
  return id
}

// seedLogs posts through the SDK ingest endpoint, which needs the project
// secret. It is the second readonly field in the wizard's last step.
async function projectSecret(page) {
  const values = await page.locator('#project-wizard input[readonly]').allInputValues()
  return values[1]
}

module.exports = { BASE, EMAIL, PASSWORD, launch, linger, signIn, createProject, projectSecret }
