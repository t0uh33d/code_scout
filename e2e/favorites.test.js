// Starring a project.
//
// The whole feature was built and had no browser test at all, which is how it
// came to be written down in two places as "the tab exists, the backend does
// not". It exists. What was missing was anything that would notice if it
// stopped working: the star posts to one URL, the route is registered at
// another, and a mismatch is a silently dead button on every card.

const { test, before, after } = require('node:test')
const assert = require('node:assert')

const { BASE, launch, linger, signIn, createProject } = require('./harness')

let browser, page, starred, plain

const cards = () => page.locator('[data-project-card]')
const starOf = name => cards().filter({ hasText: name }).first().locator('button[aria-pressed]')

// The page is `/`. `/dashboard/projects` answers a bare fragment for htmx to
// swap, so navigating a browser there gets markup with no stylesheet and no
// htmx: the star renders and does nothing, which looks exactly like the bug
// this file is here to catch.
async function openProjects(query = '') {
  await page.goto(`${BASE}/${query}`)
  await page.waitForSelector('[data-project-card]')
}

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
  starred = await createProject(page, 'Favorite Me')
  plain = await createProject(page, 'Leave Me Alone')
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

test('the star turns on and stays on across a reload', async () => {
  await openProjects()

  const star = starOf('Favorite Me')
  assert.equal(await star.getAttribute('aria-pressed'), 'false')

  // The button swaps itself: the handler answers with the same element in its
  // new state, so there is nothing else on the page to keep in step.
  await star.click()
  await page.waitForFunction(
    () => document.querySelector('button[aria-pressed="true"]') !== null)

  // A reload is what separates "the button changed colour" from "the row was
  // written". The first is a swap, the second is the feature.
  await openProjects()
  assert.equal(await starOf('Favorite Me').getAttribute('aria-pressed'), 'true',
    'the star was not stored, so it came back off')
  assert.equal(await starOf('Leave Me Alone').getAttribute('aria-pressed'), 'false')
})

test('the Favourites tab shows only what is starred', async () => {
  await openProjects()

  await Promise.all([
    page.waitForResponse(r => r.url().includes('filter=favorites')),
    page.click('button:has-text("Favorite projects")'),
  ])
  await page.waitForFunction(
    () => document.querySelectorAll('[data-project-card]').length === 1)

  const names = await cards().allTextContents()
  assert.equal(names.length, 1, `the tab did not filter: ${names}`)
  assert.match(names[0], /Favorite Me/)
})

// The tab is in the query string, so it survives a reload and can be sent to
// somebody. The landing page is the only place it can be restored from:
// switching tabs is an htmx swap against a fragment endpoint.
test('the favourites tab is a URL you can come back to', async () => {
  await openProjects('?filter=favorites')

  const names = await cards().allTextContents()
  assert.equal(names.length, 1, `the URL did not restore the tab: ${names}`)
  assert.match(names[0], /Favorite Me/)
})

test('unstarring takes it back out of the tab', async () => {
  await openProjects()
  await starOf('Favorite Me').click()
  await page.waitForFunction(
    () => document.querySelector('button[aria-pressed="true"]') === null)

  // Not openProjects: it waits for a card, and the point here is that there
  // are none.
  await page.goto(`${BASE}/?filter=favorites`)
  await page.waitForSelector('#projects-grid')

  // Nothing starred, so the tab says so rather than rendering an empty grid
  // that reads as a broken page.
  const grid = await page.locator('#projects-grid').textContent()
  assert.ok(!grid.includes('Favorite Me'),
    `an unstarred project is still in the favourites tab: ${grid}`)
  assert.match(grid, /No favourites yet/)
})
