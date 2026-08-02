import { test } from 'node:test';
import assert from 'node:assert/strict';

import { canRepairChannelConsistency } from './adminPermissions.js';

test('classic channel consistency repair requires channel operate permission', () => {
  assert.equal(
    canRepairChannelConsistency({ canOperateChannel: true }),
    true,
  );
  assert.equal(
    canRepairChannelConsistency({ canOperateChannel: false }),
    false,
  );
  assert.equal(canRepairChannelConsistency(null), false);
});
