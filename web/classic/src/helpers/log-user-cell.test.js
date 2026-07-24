import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  getLogUserDisplayName,
  getLogUserId,
  openLogUserInfo,
} from './log-user-cell.js';

test('log user display prefers username and falls back to real user id', () => {
  assert.equal(
    getLogUserDisplayName({ username: 'current-user', user_id: 42 }),
    'current-user'
  );
  assert.equal(getLogUserDisplayName({ username: '', user_id: 42 }), '#42');
  assert.equal(getLogUserDisplayName({ username: '   ', user_id: 42 }), '#42');
  assert.equal(getLogUserDisplayName({ username: '', user_id: 0 }), '');
});

test('log user id keeps only real user ids', () => {
  assert.equal(getLogUserId({ user_id: 42 }), 42);
  assert.equal(getLogUserId({ user_id: '42' }), '42');
  assert.equal(getLogUserId({ user_id: 0 }), null);
  assert.equal(getLogUserId({}), null);
});

test('log user info click opens by user id and stops row propagation', () => {
  const opened = [];
  const event = {
    stopped: false,
    stopPropagation() {
      this.stopped = true;
    },
  };

  assert.equal(
    openLogUserInfo(
      { username: 'renamed-user', user_id: 42 },
      (userId) => opened.push(userId),
      event
    ),
    true
  );
  assert.deepEqual(opened, [42]);
  assert.equal(event.stopped, true);
});

test('log user info click does nothing without a real user id', () => {
  const opened = [];

  assert.equal(
    openLogUserInfo({ username: 'renamed-user', user_id: 0 }, (userId) =>
      opened.push(userId)
    ),
    false
  );
  assert.deepEqual(opened, []);
});
