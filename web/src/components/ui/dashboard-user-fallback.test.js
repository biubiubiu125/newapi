import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

test('dashboard user chart falls back to real user id', () => {
  const source = readFileSync(new URL('./dashboard.jsx', import.meta.url), 'utf8');

  assert.match(source, /const displayQuotaUser = \(item\) =>/);
  assert.match(source, /return item\.user_id \? `#\$\{item\.user_id\}` : 'unknown'/);
  assert.match(source, /const user = displayQuotaUser\(item\)/);
  assert.match(source, /const itemUser = displayQuotaUser\(item\)/);
});
