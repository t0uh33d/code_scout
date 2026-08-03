// Session sampling is a setting whose only consumer is an SDK on someone's
// phone. Its whole job is to travel: from a number typed into project settings,
// through the database, out of GET /api/validate, to a client that has to be
// able to read it. A Go test can prove each hop; only this can prove the
// journey, so it drives the real form and then reads the real endpoint with
// the same headers the Flutter SDK sends.

const { test, before, after } = require('node:test')
const assert = require('node:assert')

const { BASE, launch, linger, signIn, createProject } = require('./harness')

let browser, page, projectID, secret

// Asks the server exactly what the SDK asks it, with the SDK's headers.
async function validateAsSDK() {
  const res = await fetch(`${BASE}/api/validate`, {
    headers: { 'X-Project-ID': projectID, 'X-Project-Secret': secret },
  })
  assert.equal(res.status, 200, 'the SDK should be able to validate its credentials')
  return res.json()
}

async function setSampling(page, percent) {
  await page.goto(`${BASE}/project/${projectID}/settings`)
  await page.waitForSelector('#project-sampling')
  await page.fill('#project-sampling', String(percent))
  await page.click('#general-form button[type="submit"]')
}

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
  ;({ id: projectID, secret } = await createProject(page, 'Sampling E2E'))
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

test('a new project tells the SDK to record every session', async () => {
  const body = await validateAsSDK()
  assert.equal(body.session_sample_rate, 1,
    'a project nobody has configured must not be quietly sampling')
})

test('a rate set in project settings reaches the SDK as a fraction', async () => {
  await setSampling(page, 25)
  await page.waitForSelector('#general-form:has-text("Saved")', { timeout: 5000 })

  const body = await validateAsSDK()
  assert.equal(body.session_sample_rate, 0.25,
    'the dashboard works in percent and the SDK in fractions; this is where they meet')
})

// Zero is the value that breaks anything treating "unset" as "off". It has to
// survive the form, the update, the database and the JSON encoder.
test('zero percent survives the whole journey', async () => {
  await setSampling(page, 0)
  await page.waitForSelector('#general-form:has-text("Saved")', { timeout: 5000 })

  const body = await validateAsSDK()
  assert.equal(body.session_sample_rate, 0,
    'a project switched off reported as anything else would keep sending logs')
})

test('the saved rate is what the settings screen shows on the way back', async () => {
  await setSampling(page, 40)
  await page.waitForSelector('#general-form:has-text("Saved")', { timeout: 5000 })

  // A fresh load, not the swapped fragment: someone returning to rename the
  // project must not save 100% back over their own setting.
  await page.goto(`${BASE}/project/${projectID}/settings`)
  await page.waitForSelector('#project-sampling')
  assert.equal(await page.inputValue('#project-sampling'), '40')
})

test('an out-of-range rate is refused inline and changes nothing', async () => {
  // Past the browser's own max, so this is the server's answer being tested.
  await page.goto(`${BASE}/project/${projectID}/settings`)
  await page.waitForSelector('#project-sampling')
  await page.evaluate(() => {
    const input = document.querySelector('#project-sampling')
    input.removeAttribute('max')
    input.value = '400'
  })
  await page.click('#general-form button[type="submit"]')

  await page.waitForSelector('#general-form:has-text("Sampling must be between 0 and 100 percent.")',
    { timeout: 5000 })

  const body = await validateAsSDK()
  assert.equal(body.session_sample_rate, 0.4,
    'a refused save must leave the previous rate in place')
})
