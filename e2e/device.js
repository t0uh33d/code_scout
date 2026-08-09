// A fake phone: the device half of a live session, spoken for real.
//
// This was inside live.test.js, which was the right place while it had one
// caller. `screenshots.js` needs the same thing — a device that pairs and then
// answers database questions — and a second copy of a WebSocket implementation
// is the kind of duplication that rots quietly: the framing changes on one side,
// the other keeps passing against its own stale idea of the protocol.
//
// Deliberately not a library. The point of driving the socket by hand is that
// if the frame the server reads is wrong, this is what says so.

const { randomBytes, randomUUID } = require('node:crypto')
const { connect } = require('node:net')

const { BASE } = require('./harness')

// A minimal WebSocket client over a raw socket. Node has no built-in client
// and the point is to avoid trusting a library to speak the protocol correctly
// on our behalf.
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

// Answers requests on the device socket until told to stop. Records what was
// asked, so a caller can assert on the command rather than only on the
// rendering.
function serveDatabase(dev, answers) {
  const asked = []
  let stopped = false
  ;(async () => {
    while (!stopped) {
      let frame
      try {
        frame = JSON.parse(await dev.next(8000))
      } catch {
        return
      }
      if (!frame.req) continue
      asked.push(frame)
      const body = answers[frame.op]
      dev.send({ req: frame.req, ...(typeof body === 'function' ? body(frame) : body) })
    }
  })()
  return { asked, stop: () => { stopped = true } }
}

// The default phone. Callers that care about the device facts pass their own.
const defaultDevice = () => ({
  session_id: randomUUID(),
  installation_id: 'e2e-install-01',
  device_model: 'Pixel 7',
  os_name: 'Android',
  os_version: '14',
})

module.exports = { DeviceSocket, serveDatabase, defaultDevice }
