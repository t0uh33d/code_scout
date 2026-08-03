// Session filters are the only ones that cross tables: everything else is a
// column on `logs`, these live on `sessions`. That makes them the ones most
// likely to be right in the parser and wrong on screen, so this drives the
// whole path — a link on a session or device page, through the URL, to a
// viewer showing the right rows.

const { test, before, after } = require('node:test')
const assert = require('node:assert')
const { randomUUID } = require('node:crypto')

const { BASE, launch, linger, signIn, createProject, seedLogs } = require('./harness')

let browser, page, projectID, secret

// Two people, three launches, two app versions and two devices — enough that
// every filter has something to exclude as well as something to match.
const alice = { session: randomUUID(), install: randomUUID() }
const aliceUpgraded = { session: randomUUID(), install: alice.install }
const bob = { session: randomUUID(), install: randomUUID() }
const anon = { session: randomUUID(), install: randomUUID() }

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
  ;({ id: projectID, secret } = await createProject(page, 'Session Filters E2E'))

  await seedLogs(
    projectID,
    secret,
    [
      { sessionID: alice.session, message: 'alice on the pixel' },
      { sessionID: aliceUpgraded.session, message: 'alice after upgrading' },
      { sessionID: bob.session, message: 'bob on the iphone' },
      { sessionID: anon.session, message: 'nobody signed in' },
    ],
    [
      {
        id: alice.session, installationID: alice.install, userID: 'u_alice',
        deviceModel: 'Pixel 7', osName: 'Android', osVersion: '14', appVersion: '3.11.2',
      },
      {
        id: aliceUpgraded.session, installationID: aliceUpgraded.install, userID: 'u_alice',
        deviceModel: 'Pixel 7', osName: 'Android', osVersion: '14', appVersion: '3.12.0',
      },
      {
        id: bob.session, installationID: bob.install, userID: 'u_bob',
        deviceModel: 'iPhone 15 Pro', osName: 'iOS', osVersion: '17.4', appVersion: '3.11.2',
      },
      {
        id: anon.session, installationID: anon.install,
        deviceModel: 'Galaxy S23', osName: 'Android', osVersion: '13', appVersion: '3.10.0',
      },
    ],
  )
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

// Reads the messages currently on screen in the log viewer.
async function messagesFor(query) {
  await page.goto(`${BASE}/project/${projectID}/logs?q=${encodeURIComponent(query)}`)
  await page.waitForSelector('[data-log-row], [data-empty]', { timeout: 5000 })
  return page.locator('[data-log-row]').evaluateAll((rows) =>
    rows.map((r) => r.textContent.replace(/\s+/g, ' ').trim()),
  )
}

function includesMessage(messages, needle) {
  return messages.some((m) => m.includes(needle))
}

test('user: narrows to one person across their launches', async () => {
  const messages = await messagesFor('user:u_alice')

  assert.ok(includesMessage(messages, 'alice on the pixel'), 'missing her first launch')
  assert.ok(includesMessage(messages, 'alice after upgrading'), 'missing her second launch')
  assert.ok(!includesMessage(messages, 'bob on the iphone'), 'another user leaked in')
  assert.ok(!includesMessage(messages, 'nobody signed in'), 'an anonymous session leaked in')
})

// The value has a space in it, so this also proves the quoting survives the URL.
test('device: matches partially and ignores case', async () => {
  const messages = await messagesFor('device:pixel')

  assert.ok(includesMessage(messages, 'alice on the pixel'))
  assert.ok(!includesMessage(messages, 'bob on the iphone'))

  const exact = await messagesFor('device:"Pixel 7"')
  assert.ok(includesMessage(exact, 'alice on the pixel'),
    'a device model with a space in it did not survive the URL')
})

test('os: matches either the name or the version', async () => {
  const byName = await messagesFor('os:iOS')
  assert.ok(includesMessage(byName, 'bob on the iphone'))
  assert.ok(!includesMessage(byName, 'alice on the pixel'))

  const byVersion = await messagesFor('os:17.4')
  assert.ok(includesMessage(byVersion, 'bob on the iphone'),
    'the version half of "iOS 17.4" should match too')
})

test('app_version: is exact, so a prefix matches nothing', async () => {
  const exact = await messagesFor('app_version:3.12.0')
  assert.ok(includesMessage(exact, 'alice after upgrading'))
  assert.ok(!includesMessage(exact, 'alice on the pixel'), 'her older launch leaked in')

  const prefix = await messagesFor('app_version:3.1')
  assert.deepEqual(prefix, [], 'a version prefix must not match — 3.1 is not 3.11.2')
})

test('session filters combine with each other and with level filters', async () => {
  const both = await messagesFor('user:u_alice app_version:3.12.0')
  assert.ok(includesMessage(both, 'alice after upgrading'))
  assert.ok(!includesMessage(both, 'alice on the pixel'),
    'two session filters should be an AND')
})

// The chips are the only way back out of a filter that has no toggle of its
// own, since a project has thousands of users and no full set to render.
test('an applied session filter shows a chip that clears just itself', async () => {
  await page.goto(
    `${BASE}/project/${projectID}/logs?q=${encodeURIComponent('level:info user:u_alice device:pixel')}`,
  )
  await page.waitForSelector('[data-session-filter="user"]')

  assert.equal(await page.locator('[data-session-filter]').count(), 2,
    'one chip per applied session filter')

  await page.click('[data-session-filter="user"]')
  await page.waitForSelector('[data-session-filter="device"]')

  assert.equal(await page.locator('[data-session-filter="user"]').count(), 0,
    'clicking the user chip should clear it')
  assert.equal(await page.locator('[data-session-filter="device"]').count(), 1,
    'clearing one session filter must not clear the other')
  assert.equal(await page.locator('[data-level="info"][aria-pressed="true"]').count(), 1,
    'clearing a session filter must not clear the level filter')
})

// The filters exist to be arrived at, not typed. This is the route someone
// actually takes.
test('the sessions list links a user to their logs', async () => {
  await page.goto(`${BASE}/project/${projectID}/sessions`)
  await page.waitForSelector('[data-scope-link="user"]')

  await page.click('[data-scope-link="user"] >> nth=0')
  await page.waitForSelector('[data-session-filter="user"]', { timeout: 5000 })

  assert.match(page.url(), /user%3A/, 'the link should carry a user filter in the URL')
})

test('a device page links to every log from that install', async () => {
  await page.goto(`${BASE}/project/${projectID}/device/${alice.install}`)
  await page.waitForSelector('[data-scope-link="installation"]')

  await page.click('[data-scope-link="installation"]')
  await page.waitForSelector('[data-session-filter="installation"]', { timeout: 5000 })

  const messages = await page.locator('[data-log-row]').evaluateAll((rows) =>
    rows.map((r) => r.textContent.replace(/\s+/g, ' ').trim()),
  )
  assert.ok(includesMessage(messages, 'alice on the pixel'))
  assert.ok(includesMessage(messages, 'alice after upgrading'),
    'both launches from the install should be there')
  assert.ok(!includesMessage(messages, 'bob on the iphone'))
})
