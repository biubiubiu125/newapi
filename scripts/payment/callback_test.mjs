import crypto from 'node:crypto'

const baseUrl = process.env.BASE_URL || 'http://127.0.0.1:3000'
const epayKey = process.env.EPAY_KEY || ''
const epayPid = process.env.EPAY_PID || ''
const epusdtKey = process.env.EPUSDT_SECRET_KEY || ''
const epusdtPid = process.env.EPUSDT_PID || ''

function md5(input) {
  return crypto.createHash('md5').update(input).digest('hex')
}

function formEncode(values) {
  return new URLSearchParams(values).toString()
}

function epaySign(values, key) {
  const source = Object.entries(values)
    .filter(([k, v]) => k !== 'sign' && k !== 'sign_type' && String(v) !== '')
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([k, v]) => `${k}=${v}`)
    .join('&')
  return md5(source + key)
}

function epusdtSign(values, key) {
  const source = Object.entries(values)
    .filter(([k, v]) => k !== 'signature' && k !== 'sign' && String(v) !== '')
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([k, v]) => `${k}=${v}`)
    .join('&')
  return md5(source + key)
}

async function postForm(path, values) {
  const res = await fetch(`${baseUrl}${path}`, {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: formEncode(values),
  })
  return { status: res.status, body: await res.text() }
}

async function postJson(path, values) {
  const res = await fetch(`${baseUrl}${path}`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(values),
  })
  return { status: res.status, body: await res.text() }
}

function epayPayload(overrides = {}) {
  const values = {
    pid: epayPid,
    type: process.env.EPAY_TYPE || 'alipay',
    out_trade_no: process.env.TRADE_NO || 'TEST_TRADE_NO',
    trade_no: `epay-${Date.now()}`,
    name: 'audit callback test',
    money: process.env.PAID_AMOUNT || '1.00',
    trade_status: 'TRADE_SUCCESS',
    sign_type: 'MD5',
    ...overrides,
  }
  values.sign = epaySign(values, epayKey)
  return values
}

function epusdtPayload(overrides = {}) {
  const values = {
    pid: epusdtPid,
    order_id: process.env.TRADE_NO || 'TEST_TRADE_NO',
    status: 'paid',
    amount: process.env.PAID_AMOUNT || '1.00',
    order_currency: process.env.PAID_CURRENCY || 'CNY',
    token: process.env.EPUSDT_TOKEN || 'usdt',
    network: process.env.EPUSDT_NETWORK || 'tron',
    ...overrides,
  }
  values.signature = epusdtSign(values, epusdtKey)
  return values
}

async function run() {
  if (!process.env.TRADE_NO) {
    throw new Error('TRADE_NO is required')
  }
  if (!epayKey || !epayPid) {
    console.warn('EPAY_PID/EPAY_KEY missing; epay cases will be skipped')
  }
  if (!epusdtKey || !epusdtPid) {
    console.warn('EPUSDT_PID/EPUSDT_SECRET_KEY missing; epusdt cases will be skipped')
  }

  const results = []
  if (epayKey && epayPid) {
    results.push(['epay success', await postForm('/api/user/epay/notify', epayPayload())])
    results.push(['epay duplicate', await postForm('/api/user/epay/notify', epayPayload())])
    results.push(['epay bad sign', await postForm('/api/user/epay/notify', { ...epayPayload(), sign: 'bad' })])
    results.push(['epay amount -0.01', await postForm('/api/user/epay/notify', epayPayload({ money: '0.99' }))])
    results.push(['epay cross subscription endpoint', await postForm('/api/subscription/epay/notify', epayPayload())])
  }
  if (epusdtKey && epusdtPid) {
    results.push(['epusdt success', await postJson('/api/user/epusdt/notify', epusdtPayload())])
    results.push(['epusdt duplicate', await postJson('/api/user/epusdt/notify', epusdtPayload())])
    results.push(['epusdt bad sign', await postJson('/api/user/epusdt/notify', { ...epusdtPayload(), signature: 'bad' })])
    results.push(['epusdt amount +0.01', await postJson('/api/user/epusdt/notify', epusdtPayload({ amount: '1.01' }))])
    results.push(['epusdt currency mismatch', await postJson('/api/user/epusdt/notify', epusdtPayload({ order_currency: 'USDT' }))])
    results.push(['epusdt cross subscription endpoint', await postJson('/api/subscription/epusdt/notify', epusdtPayload())])
  }

  for (const [name, result] of results) {
    console.log(JSON.stringify({ name, ...result }))
  }
}

run().catch((err) => {
  console.error(err)
  process.exit(1)
})
