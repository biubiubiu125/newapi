import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

test('task log user column falls back to real user id with prefix', () => {
  const source = readFileSync(
    new URL('./task-logs-columns.tsx', import.meta.url),
    'utf8'
  )

  assert.match(source, /getLogUserDisplayName\(log\)/)
  assert.doesNotMatch(
    source,
    /log\.username\s*\|\|\s*String\(log\.user_id\s*\|\|\s*'\?'\)/
  )
  assert.match(
    source,
    /openLogUserInfo\(\s*log,\s*setSelectedUserId,\s*setUserInfoDialogOpen,\s*e\s*\)/
  )
  assert.match(
    source,
    /const settlementFailed = log\.settlement_status === 'REVIEW'/
  )
  assert.match(source, /Settlement failed/)
})

test('drawing log status highlights settlement review', () => {
  const source = readFileSync(
    new URL('./drawing-logs-columns.tsx', import.meta.url),
    'utf8'
  )

  assert.match(source, /log\.settlement_status === 'REVIEW'/)
  assert.match(source, /Settlement review/)
})

test('task log status highlights settlement review', () => {
  const source = readFileSync(
    new URL('./task-logs-columns.tsx', import.meta.url),
    'utf8'
  )

  assert.match(
    source,
    /const settlementFailed = log\.settlement_status === 'REVIEW'/
  )
  assert.match(source, /settlementFailed[\s\S]*Settlement review/)
  assert.match(
    source,
    /variant=\{\s*settlementFailed \? 'red' : taskStatusMapper\.getVariant\(status\)\s*\}/
  )
})

test('task log details prioritize settlement review over media previews', () => {
  const source = readFileSync(
    new URL('./task-logs-columns.tsx', import.meta.url),
    'utf8'
  )

  const settlementBranch = source.indexOf('if (settlementFailed)')
  const sunoBranch = source.indexOf('if (isSunoSuccess)')
  const videoBranch = source.indexOf('if (isSuccess && isVideoTask && isUrl)')

  assert.notEqual(settlementBranch, -1)
  assert.notEqual(sunoBranch, -1)
  assert.notEqual(videoBranch, -1)
  assert.ok(settlementBranch < sunoBranch)
  assert.ok(settlementBranch < videoBranch)
})
