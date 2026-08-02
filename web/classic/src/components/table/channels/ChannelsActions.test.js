import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

test('classic channel consistency repair is guarded in the action menu and confirm callback', () => {
  const source = readFileSync(
    new URL('./ChannelsActions.jsx', import.meta.url),
    'utf8',
  );

  assert.match(source, /canRepairChannelConsistency/);
  assert.match(source, /disabled=\{!canRepairChannelConsistency\}/);
  assert.match(source, /if \(!canRepairChannelConsistency\) return;/);
});
