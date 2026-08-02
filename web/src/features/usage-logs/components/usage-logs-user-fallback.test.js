import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

test('common usage log user cell falls back to user id and uses it on click', () => {
  const source = readFileSync(
    new URL('./columns/common-logs-columns.tsx', import.meta.url),
    'utf8'
  )

  assert.match(source, /getLogUserDisplayName\(log\)/)
  assert.match(
    source,
    /openLogUserInfo\(\s*log,\s*setSelectedUserId,\s*setUserInfoDialogOpen,\s*e,?\s*\)/
  )
})

test('mobile usage log user card falls back to user id and uses it on click', () => {
  const source = readFileSync(
    new URL('./usage-logs-mobile-card.tsx', import.meta.url),
    'utf8'
  )

  assert.match(source, /getLogUserDisplayName\(log\)/)
  assert.match(
    source,
    /openLogUserInfo\(\s*log,\s*setSelectedUserId,\s*setUserInfoDialogOpen,\s*e,?\s*\)/
  )
})
