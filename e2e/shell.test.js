// Browser tests for the project shell. These cover the things a curl of the
// HTML cannot: whether a trigger actually fires, whether a swap lands, and
// whether an element is really visible where the layout says it is.

const { test, before, after } = require('node:test')
const assert = require('node:assert')

const { BASE, launch, linger, signIn, createProject, seedLogs } = require('./harness')

let browser, page, projectID, secret

// Spread over the last hour so ordering is stable and every row lands inside
// any date window a test might apply.
const manyLogs = count =>
  Array.from({ length: count }, (_, i) => ({
    message: `Seeded log #${i + 1}`,
    at: new Date(Date.now() - i * 1000),
  }))

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
  ;({ id: projectID, secret } = await createProject(page, 'Shell E2E'))
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

// The regression this whole file exists for. The shell makes the content card
// its own scroll container, so the window never scrolls on a desktop. htmx's
// `revealed` trigger polls only after a window scroll event, so pagination
// silently stopped; `intersect` uses an IntersectionObserver instead.
test('infinite scroll keeps paging inside the shell scroll container', async () => {
  await seedLogs(projectID, secret, manyLogs(140))
  await page.goto(`${BASE}/project/${projectID}/logs`)
  await page.waitForSelector('#log-rows > div')

  const firstBatch = await page.locator('#log-rows > div[class*="grid"]').count()
  assert.ok(firstBatch > 0, 'expected the first page of logs to render')
  assert.ok(firstBatch <= 50, `first batch should be one page, got ${firstBatch}`)

  // Prove the document itself does not scroll, which is what breaks `revealed`.
  const windowScrolls = await page.evaluate(
    () => document.documentElement.scrollHeight > document.documentElement.clientHeight + 1)
  assert.equal(windowScrolls, false, 'the window should not scroll; only <main> should')

  // Wait for the request rather than sleeping: that is what made the earlier
  // headless attempt non-deterministic.
  const paged = page.waitForResponse(
    r => r.url().includes('/logs/partial') && r.status() === 200, { timeout: 10000 })
  await page.evaluate(() => { document.querySelector('main').scrollTop = 99999 })
  await paged

  await page.waitForFunction(
    n => document.querySelectorAll('#log-rows > div[class*="grid"]').length > n,
    firstBatch, { timeout: 10000 })

  const afterScroll = await page.locator('#log-rows > div[class*="grid"]').count()
  assert.ok(afterScroll > firstBatch,
    `expected more rows after scrolling, had ${firstBatch} and still have ${afterScroll}`)
})

// The rename response carries an out-of-band fragment aimed at the sidebar
// heading. hx-swap-oob fails silently when the id is absent or the fragment
// drifts, so this asserts the heading actually changed on screen.
test('renaming a project updates the sidebar heading out of band', async () => {
  await page.goto(`${BASE}/project/${projectID}/settings`)
  await page.waitForSelector('#project-shell-name')

  // Armed before the click, or it would miss the navigation it is watching for.
  let navigated = false
  const watch = f => { if (f === page.mainFrame()) navigated = true }
  page.on('framenavigated', watch)

  await page.fill('#general-form input[name="name"]', 'Renamed By E2E')
  await page.click('#general-form button[type="submit"]')

  await page.waitForFunction(
    () => document.getElementById('project-shell-name').textContent.trim() === 'Renamed By E2E',
    null, { timeout: 5000 })

  // The point of the out-of-band swap is that the heading updates without a
  // page load. If this ever reloads, the swap has stopped doing anything and
  // the test would otherwise still pass.
  page.off('framenavigated', watch)
  assert.equal(navigated, false, 'the heading should update in place, not via a reload')

  // Whether the swapped-in markup MATCHES the sidebar's own is asserted in Go,
  // in TestShellNameFragmentMatchesTheSidebarHeading — deterministic there, and
  // a browser assertion on computed style proved unreliable.

  await page.fill('#general-form input[name="name"]', 'Shell E2E')
  await page.click('#general-form button[type="submit"]')
})

// The account menu sits at the foot of the sidebar and opens upward. It is
// wider than the sidebar, so it only works if no ancestor clips it — which is
// why the sidebar's scroll lives on an inner element.
test('the account menu opens upward and is not clipped by the sidebar', async () => {
  await page.goto(`${BASE}/project/${projectID}/logs`)
  await page.click('#account-menu summary')

  const logout = page.locator('#account-menu button:has-text("Log out")')
  await logout.waitFor({ state: 'visible', timeout: 5000 })

  const menu = await logout.boundingBox()
  const trigger = await page.locator('#account-menu summary').boundingBox()
  assert.ok(menu.y < trigger.y, 'the menu should sit above its trigger, not below')

  const viewport = page.viewportSize()
  assert.ok(menu.x >= 0 && menu.x + menu.width <= viewport.width,
    'the menu should be fully on screen')
  assert.ok(menu.y >= 0, 'the menu should not run off the top of the page')
})

// The sidebar is a fixed-height card. Its content has to scroll inside it
// rather than spilling out when the viewport is short.
test('the sidebar contains its own content on a short viewport', async () => {
  // Same context as `page`, so it carries the session cookie. A page opened
  // straight off the browser gets a fresh context and lands on /login.
  const short = await page.context().newPage()
  try {
    await short.setViewportSize({ width: 1280, height: 320 })
    await short.goto(`${BASE}/project/${projectID}/logs`)
    await short.waitForSelector('aside')

    const overflows = await short.evaluate(() => {
      const aside = document.querySelector('aside')
      const box = aside.getBoundingClientRect()
      return Array.from(aside.querySelectorAll('*')).some(el => {
        const r = el.getBoundingClientRect()
        return r.height > 0 && r.bottom > box.bottom + 1
      })
    })
    assert.equal(overflows, false, 'sidebar content escaped the sidebar card')

    const bodyScrollsSideways = await short.evaluate(
      () => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1)
    assert.equal(bodyScrollsSideways, false, 'the page should never scroll sideways')
  } finally {
    await short.close()
  }
})
