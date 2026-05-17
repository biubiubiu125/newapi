import http from 'k6/http'
import { check, sleep } from 'k6'
import exec from 'k6/execution'

const baseUrl = __ENV.BASE_URL || 'http://127.0.0.1:3000'
const promoters = Number(__ENV.PROMOTERS || 100)
const inviteesPerPromoter = Number(__ENV.INVITEES_PER_PROMOTER || 100)
const timestamp = __ENV.RUN_ID || `${Date.now()}`
const shortRunId = `${timestamp}`.replace(/\D/g, '').slice(-4).padStart(4, '0')
const password = __ENV.TEST_PASSWORD || 'AuditLoadTest123!'
const codes = (__ENV.INVITE_CODES || '').split(',').map((s) => s.trim()).filter(Boolean)

export const options = {
  scenarios: {
    referral_registration_10k: {
      executor: 'shared-iterations',
      vus: Number(__ENV.VUS || 1000),
      iterations: promoters * inviteesPerPromoter,
      maxDuration: __ENV.MAX_DURATION || '15m',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.05'],
    http_req_duration: ['p(95)<2000', 'p(99)<5000'],
  },
}

export default function () {
  const i = exec.scenario.iterationInTest
  const promoterIndex = Math.floor(i / inviteesPerPromoter)
  const userIndex = i % inviteesPerPromoter
  const code = codes[promoterIndex]
  const username = `u${shortRunId}${String(promoterIndex).padStart(3, '0')}${String(userIndex).padStart(3, '0')}`
  const payload = JSON.stringify({
    username,
    password,
    aff: code,
    email: `${username}@example.test`,
  })
  const res = http.post(`${baseUrl}/api/user/register`, payload, {
    headers: { 'content-type': 'application/json' },
    tags: { promoter: `${promoterIndex}` },
  })
  check(res, {
    'registered or clear duplicate': (r) => r.status === 200 && (r.body.includes('"success":true') || r.body.includes('exists')),
  })
  sleep(0.01)
}
