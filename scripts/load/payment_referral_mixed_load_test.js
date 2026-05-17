import http from 'k6/http'
import { check } from 'k6'
import exec from 'k6/execution'

const baseUrl = __ENV.BASE_URL || 'http://127.0.0.1:3000'
const epayNotify = `${baseUrl}/api/user/epay/notify`
const epusdtNotify = `${baseUrl}/api/user/epusdt/notify`
const trades = (__ENV.TRADE_NOS || '').split(',').map((s) => s.trim()).filter(Boolean)
const epayPayloads = (__ENV.EPAY_PAYLOADS || '').split('\n').filter(Boolean)
const epusdtPayloads = (__ENV.EPUSDT_PAYLOADS || '').split('\n').filter(Boolean)

export const options = {
  scenarios: {
    mixed_callbacks: {
      executor: 'shared-iterations',
      vus: Number(__ENV.VUS || 200),
      iterations: Number(__ENV.ITERATIONS || Math.max(trades.length, epayPayloads.length + epusdtPayloads.length)),
      maxDuration: __ENV.MAX_DURATION || '10m',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.05'],
    http_req_duration: ['p(95)<2000', 'p(99)<5000'],
  },
}

export default function () {
  const i = exec.scenario.iterationInTest
  if (i % 2 === 0 && epayPayloads.length > 0) {
    const body = epayPayloads[(i / 2) % epayPayloads.length]
    const res = http.post(epayNotify, body, { headers: { 'content-type': 'application/x-www-form-urlencoded' } })
    check(res, { 'epay callback accepted': (r) => r.status === 200 && r.body.includes('success') })
    return
  }
  if (epusdtPayloads.length > 0) {
    const body = epusdtPayloads[i % epusdtPayloads.length]
    const res = http.post(epusdtNotify, body, { headers: { 'content-type': 'application/json' } })
    check(res, { 'epusdt callback accepted': (r) => r.status === 200 && r.body.includes('ok') })
  }
}
