import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  getLogUserDisplayName,
  getLogUserId,
  openLogUserInfo,
} from './log-user.js'

test('usage log user display prefers username and falls back to real user id', () => {
  assert.equal(
    getLogUserDisplayName({ username: 'current-user', user_id: 42 }),
    'current-user'
  )
  assert.equal(getLogUserDisplayName({ username: '', user_id: 42 }), '#42')
  assert.equal(getLogUserDisplayName({ username: '   ', user_id: 42 }), '#42')
  assert.equal(getLogUserDisplayName({ username: '', user_id: 0 }), '')
})

test('usage log user click uses user id, not display name', () => {
  const selected = []
  let open = false
  const event = {
    stopped: false,
    stopPropagation() {
      this.stopped = true
    },
  }

  assert.equal(
    openLogUserInfo(
      { username: 'renamed-user', user_id: 42 },
      (userId) => selected.push(userId),
      (nextOpen) => {
        open = nextOpen
      },
      event
    ),
    true
  )
  assert.deepEqual(selected, [42])
  assert.equal(open, true)
  assert.equal(event.stopped, true)
})

test('usage log user click ignores rows without a real user id', () => {
  const selected = []

  assert.equal(
    openLogUserInfo(
      { username: 'renamed-user', user_id: 0 },
      (userId) => selected.push(userId),
      () => {}
    ),
    false
  )
  assert.deepEqual(selected, [])
})
