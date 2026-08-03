// Live streaming is the one feature whose whole value is that two different
// connections meet in the middle: a phone pushing over a WebSocket and a
// dashboard reading over SSE, joined by a code somebody typed. Every part of
// that can be right on its own and still not work together, so this drives
// both ends for real — a genuine WebSocket client with the SDK's headers, and
// the actual browser page reading the actual stream.

const { test, before, after } = require('node:test')
const assert = require('node:assert')
const { randomBytes, randomUUID } = require('node:crypto')
const { connect } = require('node:net')

const { BASE, launch, linger, signIn, createProject } = require('./harness')

let browser, page, projectID, secret

before(async () => {
  ;({ browser, page } = await launch())
  await signIn(page)
  ;({ id: projectID, secret } = await createProject(page, 'Live E2E'))
})

after(async () => {
  await linger(page)
  if (browser) await browser.close()
})

// A minimal WebSocket client over a raw socket. Node has no built-in client
// and the point of this file is to avoid trusting a library to speak the
// protocol correctly on our behalf — if the frame the server reads is wrong,
// this should be what says so.
class DeviceSocket {
  constructor() {
    this.socket = null
    this.pending = []
    this.waiters = []
    this.buffer = Buffer.alloc(0)
  }

  connect(headers) {
    const url = new URL(BASE)
    return new Promise((resolve, reject) => {
      // Exactly 16 bytes, per RFC 6455. The server checks the length, so a
      // longer digest is refused before it ever looks at the credentials.
      const key = randomBytes(16).toString('base64')
      this.socket = connect({ host: url.hostname, port: Number(url.port) }, () => {
        const lines = [
          'GET /api/live/socket HTTP/1.1',
          `Host: ${url.host}`,
          'Upgrade: websocket',
          'Connection: Upgrade',
          `Sec-WebSocket-Key: ${key}`,
          'Sec-WebSocket-Version: 13',
          ...Object.entries(headers).map(([k, v]) => `${k}: ${v}`),
          '', '',
        ]
        this.socket.write(lines.join('\r\n'))
      })

      let handshake = Buffer.alloc(0)
      const onHandshake = (chunk) => {
        handshake = Buffer.concat([handshake, chunk])
        const end = handshake.indexOf('\r\n\r\n')
        if (end === -1) return
        const head = handshake.subarray(0, end).toString()
        this.socket.removeListener('data', onHandshake)
        if (!/^HTTP\/1\.1 101/.test(head)) {
          reject(new Error(`upgrade refused: ${head.split('\r\n')[0]}`))
          return
        }
        this.buffer = handshake.subarray(end + 4)
        this.socket.on('data', (c) => this._feed(c))
        this._drainBuffer()
        resolve()
      }
      this.socket.on('data', onHandshake)
      this.socket.on('error', reject)
    })
  }

  _feed(chunk) {
    this.buffer = Buffer.concat([this.buffer, chunk])
    this._drainBuffer()
  }

  // Only what the server actually sends: unmasked text frames and pings, all
  // small enough for the short-length form or one 16-bit extension.
  _drainBuffer() {
    for (;;) {
      if (this.buffer.length < 2) return
      const opcode = this.buffer[0] & 0x0f
      let length = this.buffer[1] & 0x7f
      let offset = 2
      if (length === 126) {
        if (this.buffer.length < 4) return
        length = this.buffer.readUInt16BE(2)
        offset = 4
      }
      if (this.buffer.length < offset + length) return
      const payload = this.buffer.subarray(offset, offset + length)
      this.buffer = this.buffer.subarray(offset + length)

      if (opcode === 0x9) { this._pong(payload); continue }
      if (opcode === 0x8) { this.socket.end(); continue }
      if (opcode !== 0x1) continue

      const message = payload.toString()
      const waiter = this.waiters.shift()
      if (waiter) waiter(message)
      else this.pending.push(message)
    }
  }

  _pong(payload) {
    this._frame(payload, 0xa)
  }

  send(obj) {
    this._frame(Buffer.from(JSON.stringify(obj)), 0x1)
  }

  // Client frames must be masked. A server that ignored the mask bit would
  // read our JSON as gibberish, which is exactly the kind of thing worth
  // finding here rather than on a phone.
  _frame(payload, opcode) {
    const mask = Buffer.from([1, 2, 3, 4])
    const masked = Buffer.from(payload)
    for (let i = 0; i < masked.length; i++) masked[i] ^= mask[i % 4]

    let header
    if (payload.length < 126) {
      header = Buffer.from([0x80 | opcode, 0x80 | payload.length])
    } else {
      header = Buffer.alloc(4)
      header[0] = 0x80 | opcode
      header[1] = 0x80 | 126
      header.writeUInt16BE(payload.length, 2)
    }
    this.socket.write(Buffer.concat([header, mask, masked]))
  }

  next(timeoutMs = 5000) {
    if (this.pending.length) return Promise.resolve(this.pending.shift())
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('the server said nothing')), timeoutMs)
      this.waiters.push((m) => { clearTimeout(timer); resolve(m) })
    })
  }

  close() {
    if (this.socket) this.socket.destroy()
  }
}

function sdkHeaders() {
  return { 'X-Project-ID': projectID, 'X-Project-Secret': secret }
}

// Mints a code through the real button and reads it off the real card.
async function mintCode(page) {
  await page.goto(`${BASE}/project/${projectID}/live`)
  await page.waitForSelector('button:has-text("New session")')
  await page.click('button:has-text("New session")')
  await page.waitForSelector('[data-pairing-code]')
  return (await page.locator('[data-pairing-code]').first().innerText()).trim()
}

async function pair(code, device) {
  const dev = new DeviceSocket()
  await dev.connect(sdkHeaders())
  dev.send({ code, device: device || { session_id: randomUUID(), installation_id: 'e2e-install-01', device_model: 'Pixel 7', os_name: 'Android', os_version: '14' } })
  const reply = JSON.parse(await dev.next())
  return { dev, reply }
}

test('a minted code is six typeable characters', async () => {
  const code = await mintCode(page)
  assert.match(code, /^[234679ACDEFGHJKMNPQRTUVWXYZ]{6}$/,
    `the code must avoid characters people mistype, got ${code}`)
})

test('a device pairs with the code and appears as connected', async () => {
  const code = await mintCode(page)
  const { dev, reply } = await pair(code)

  try {
    assert.equal(reply.ok, true, `pairing was refused: ${JSON.stringify(reply)}`)
    assert.ok(reply.session_id, 'the server should name the session it joined')

    // The list polls every 3s, so this is the poll working as much as the
    // pairing.
    await page.waitForSelector('[data-live-row]:has-text("Pixel 7")', { timeout: 10000 })
  } finally {
    dev.close()
  }
})

// The property the whole flow rests on: a code is a credential, and a
// credential that works twice is not one.
test('the same code cannot pair a second device', async () => {
  const code = await mintCode(page)
  const first = await pair(code)

  try {
    assert.equal(first.reply.ok, true)

    const second = await pair(code)
    assert.equal(second.reply.ok, false,
      'a second device paired with a code that had already been used')
    second.dev.close()
  } finally {
    first.dev.close()
  }
})

test('a made-up code is refused', async () => {
  const { dev, reply } = await pair('ZZZZZZ')
  try {
    assert.equal(reply.ok, false)
  } finally {
    dev.close()
  }
})

// The end-to-end one. A log leaves a socket and arrives, rendered, in a
// browser that was already watching.
test('a log pushed by the device shows up on the stream screen', async () => {
  const code = await mintCode(page)
  const { dev, reply } = await pair(code)

  try {
    assert.equal(reply.ok, true)
    await page.goto(`${BASE}/project/${projectID}/live/${reply.session_id}`)
    // Wait for the SSE connection itself, or the log could be published into a
    // stream nobody has subscribed to yet.
    await page.waitForSelector('#live-status:has-text("Connected")', { timeout: 10000 })

    dev.send({
      logs: [{
        id: randomUUID(),
        session_id: 'sess-live-1',
        level: 'error',
        message: 'payment declined from the live stream',
        timestamp: new Date().toISOString(),
      }],
    })

    await page.waitForSelector('#live-stream:has-text("payment declined from the live stream")',
      { timeout: 10000 })
  } finally {
    dev.close()
  }
})

// A log message is app-controlled text. It must never become markup.
test('a hostile log message is rendered as text', async () => {
  const code = await mintCode(page)
  const { dev, reply } = await pair(code)

  try {
    await page.goto(`${BASE}/project/${projectID}/live/${reply.session_id}`)
    await page.waitForSelector('#live-status:has-text("Connected")', { timeout: 10000 })

    dev.send({
      logs: [{
        id: randomUUID(),
        level: 'error',
        message: '<img src=x onerror="window.__xss = true">',
        timestamp: new Date().toISOString(),
      }],
    })

    await page.waitForSelector('#live-stream:has-text("onerror")', { timeout: 10000 })
    assert.equal(await page.evaluate(() => window.__xss), undefined,
      'the log message executed as markup')
    assert.equal(await page.locator('#live-stream img').count(), 0,
      'the log message became an element')
  } finally {
    dev.close()
  }
})

// When the phone goes, the person watching should be told, not left staring at
// a stream that quietly stopped producing.
test('the watcher is told when the device disconnects', async () => {
  const code = await mintCode(page)
  const { dev, reply } = await pair(code)

  await page.goto(`${BASE}/project/${projectID}/live/${reply.session_id}`)
  await page.waitForSelector('#live-status:has-text("Connected")', { timeout: 10000 })

  dev.close()

  await page.waitForSelector('#live-badge:has-text("Ended")', { timeout: 10000 })
})

test('another project cannot pair with this project\'s code', async () => {
  const code = await mintCode(page)
  const other = await createProject(page, 'Live E2E Other ' + randomUUID().slice(0, 8))

  const dev = new DeviceSocket()
  await dev.connect({ 'X-Project-ID': other.id, 'X-Project-Secret': other.secret })
  dev.send({ code, device: { installation_id: 'intruder' } })

  try {
    const reply = JSON.parse(await dev.next())
    assert.equal(reply.ok, false,
      "a device holding another project's credentials joined this project's session")
  } finally {
    dev.close()
  }
})

test('the socket refuses a device with no credentials', async () => {
  const dev = new DeviceSocket()
  await assert.rejects(
    () => dev.connect({}),
    /upgrade refused/,
    'the live socket accepted a device that sent no project credentials',
  )
  dev.close()
})

// Reads an SSE stream over plain fetch, so a test can reconnect deliberately
// with a Last-Event-ID the way EventSource does after a dropped connection.
async function readEvents(sessionID, { lastEventID, forMs = 1500 } = {}) {
  const headers = { cookie: await cookieHeader() }
  if (lastEventID != null) headers['Last-Event-ID'] = String(lastEventID)

  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), forMs)
  const events = []
  try {
    const res = await fetch(
      `${BASE}/project/${projectID}/live/${sessionID}/events`,
      { headers, signal: controller.signal },
    )
    assert.equal(res.status, 200, 'the stream should open')

    let buffer = ''
    for await (const chunk of res.body) {
      buffer += Buffer.from(chunk).toString()
      let split
      while ((split = buffer.indexOf('\n\n')) !== -1) {
        const frame = buffer.slice(0, split)
        buffer = buffer.slice(split + 2)
        const event = {}
        for (const line of frame.split('\n')) {
          if (line.startsWith('id: ')) event.id = Number(line.slice(4))
          else if (line.startsWith('event: ')) event.kind = line.slice(7)
          else if (line.startsWith('data: ')) event.data = JSON.parse(line.slice(6))
        }
        if (event.kind) events.push(event)
      }
    }
  } catch (e) {
    if (e.name !== 'AbortError') throw e
  } finally {
    clearTimeout(timer)
  }
  return events
}

// The browser page is already signed in; this reuses its cookie for the raw
// fetches above.
async function cookieHeader() {
  const cookies = await page.context().cookies()
  return cookies.map((c) => `${c.name}=${c.value}`).join('; ')
}

// The recovery property. EventSource reconnects on its own after a blip, and
// without this the events in that window are gone with nothing saying so.
test('a watcher that reconnects is caught up on what it missed', async () => {
  const code = await mintCode(page)
  const { dev, reply } = await pair(code)

  try {
    // Watch, see one log, note its id — that is what the browser would send
    // back as Last-Event-ID.
    const firstWatch = readEvents(reply.session_id, { forMs: 2000 })
    await new Promise((r) => setTimeout(r, 300))
    dev.send({ logs: [{ level: 'info', message: 'before the drop', timestamp: new Date().toISOString() }] })

    const seen = await firstWatch
    const logs = seen.filter((e) => e.kind === 'log')
    assert.ok(logs.length >= 1, `expected a log before the drop, got ${JSON.stringify(seen)}`)
    const lastID = logs[logs.length - 1].id
    assert.ok(Number.isInteger(lastID), 'every event must carry an id, or recovery is impossible')

    // Nobody is watching now. The app keeps going.
    dev.send({ logs: [{ level: 'error', message: 'missed while away', timestamp: new Date().toISOString() }] })
    await new Promise((r) => setTimeout(r, 300))

    const afterReconnect = await readEvents(reply.session_id, { lastEventID: lastID, forMs: 1500 })
    const messages = afterReconnect
      .filter((e) => e.kind === 'log')
      .map((e) => e.data.log.message)

    assert.ok(messages.includes('missed while away'),
      `the reconnecting watcher was never told about the log it missed: ${JSON.stringify(messages)}`)
    assert.ok(!messages.includes('before the drop'),
      'an event the watcher had already seen was replayed')
  } finally {
    dev.close()
  }
})

test('a first-time watcher gets no replay', async () => {
  const code = await mintCode(page)
  const { dev, reply } = await pair(code)

  try {
    const early = readEvents(reply.session_id, { forMs: 1200 })
    await new Promise((r) => setTimeout(r, 300))
    dev.send({ logs: [{ level: 'info', message: 'happened first', timestamp: new Date().toISOString() }] })
    await early

    // A fresh watcher arriving now wants what happens next, not a replay.
    const fresh = await readEvents(reply.session_id, { forMs: 800 })
    const messages = fresh.filter((e) => e.kind === 'log').map((e) => e.data.log.message)

    assert.deepEqual(messages, [],
      `a new watcher was shown history it did not ask for: ${JSON.stringify(messages)}`)
  } finally {
    dev.close()
  }
})

test('a session that never existed renders as over rather than erroring', async () => {
  await page.goto(`${BASE}/project/${projectID}/live/${randomUUID()}`)
  await page.waitForSelector('text=That session is over', { timeout: 5000 })
})
