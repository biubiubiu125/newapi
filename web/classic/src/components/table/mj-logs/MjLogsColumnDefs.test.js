import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

test('mj log status highlights settlement review', () => {
  const source = readFileSync(
    new URL('./MjLogsColumnDefs.jsx', import.meta.url),
    'utf8'
  );

  assert.match(source, /renderStatus\(text, t, record\.settlement_status\)/);
  assert.match(source, /settlementStatus === 'REVIEW'/);
  assert.match(source, /账务待复核/);
});

test('mj log columns use shared real user id helper', () => {
  const source = readFileSync(
    new URL('./MjLogsColumnDefs.jsx', import.meta.url),
    'utf8'
  );

  assert.match(source, /key:\s*COLUMN_KEYS\.USERNAME/);
  assert.match(source, /dataIndex:\s*'username'/);
  assert.match(source, /getLogUserDisplayName\(record\)/);
  assert.match(source, /openLogUserInfo\(record,\s*showUserInfoFunc,\s*event\)/);
  assert.match(source, /stringToColor\(displayText\)/);
});

test('mj logs page and hook expose user info modal for admin drilldown', () => {
  const pageSource = readFileSync(new URL('./index.jsx', import.meta.url), 'utf8');
  const hookSource = readFileSync(
    new URL('../../../hooks/mj-logs/useMjLogsData.js', import.meta.url),
    'utf8'
  );

  assert.match(pageSource, /import UserInfoModal/);
  assert.match(pageSource, /<UserInfoModal \{\.\.\.mjLogsData\} \/>/);
  assert.match(hookSource, /const showUserInfoFunc = async \(userId\)/);
  assert.match(hookSource, /API\.get\(`\/api\/user\/\$\{userId\}`\)/);
});

test('mj log column selector hides username from non-admin users', () => {
  const source = readFileSync(
    new URL('./modals/ColumnSelectorModal.jsx', import.meta.url),
    'utf8'
  );

  assert.match(source, /column\.key === COLUMN_KEYS\.USERNAME/);
});
