import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

test('usage log username column falls back to real user id', () => {
  const source = readFileSync(
    new URL('./UsageLogsColumnDefs.jsx', import.meta.url),
    'utf8',
  );

  assert.match(
    source,
    /record\.username\s*\|\|\s*\(record\.user_id\s*\?\s*`#\$\{record\.user_id\}`\s*:\s*''\)/,
  );
  assert.match(source, /stringToColor\(displayName\)/);
  assert.match(source, /showUserInfoFunc\(record\.user_id\)/);
});
