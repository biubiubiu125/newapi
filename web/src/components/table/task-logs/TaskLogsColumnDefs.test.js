import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

test('task log username column uses shared real user id helper', () => {
  const source = readFileSync(
    new URL('./TaskLogsColumnDefs.jsx', import.meta.url),
    'utf8',
  );

  assert.match(source, /getLogUserDisplayName\(record\)/);
  assert.match(source, /openLogUserInfo\(record,\s*showUserInfoFunc,\s*event\)/);
  assert.doesNotMatch(
    source,
    /String\(record\.username\s*\|\|\s*userId\s*\|\|\s*'\?'\)/,
  );
});

test('task logs page renders user info modal for admin user drilldown', () => {
  const source = readFileSync(new URL('./index.jsx', import.meta.url), 'utf8');

  assert.match(source, /import UserInfoModal/);
  assert.match(source, /<UserInfoModal \{\.\.\.taskLogsData\} \/>/);
});

test('task log status highlights settlement review', () => {
  const source = readFileSync(
    new URL('./TaskLogsColumnDefs.jsx', import.meta.url),
    'utf8',
  );

  assert.match(source, /record\.settlement_status/);
  assert.match(source, /账务待复核/);
});

test('task log details prioritize settlement review over media previews', () => {
  const source = readFileSync(
    new URL('./TaskLogsColumnDefs.jsx', import.meta.url),
    'utf8',
  );

  const settlementBranch = source.indexOf("if (record.settlement_status === 'REVIEW')");
  const sunoBranch = source.indexOf('if (isSunoSuccess)');
  const videoBranch = source.indexOf('if (isSuccess && isVideoTask && hasResultUrl)');

  assert.notEqual(settlementBranch, -1);
  assert.notEqual(sunoBranch, -1);
  assert.notEqual(videoBranch, -1);
  assert.ok(settlementBranch < sunoBranch);
  assert.ok(settlementBranch < videoBranch);
});
