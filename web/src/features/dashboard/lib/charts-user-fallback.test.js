import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

test('dashboard user chart falls back to real user id', () => {
  const source = readFileSync(new URL('./charts.ts', import.meta.url), 'utf8')

  assert.match(source, /function displayQuotaUser/)
  assert.match(source, /return userId > 0 \? `#\$\{userId\}` : 'unknown'/)
  assert.match(source, /const username = displayQuotaUser\(item\)/)
  assert.match(source, /const user = displayQuotaUser\(item\)/)
})
