-- NewAPI wallet/quota int64 migration.
-- Run this manually during a maintenance window after taking a backup.
-- This file is source/runbook only; application startup does not execute it.
--
-- PostgreSQL:
BEGIN;

ALTER TABLE users
  ALTER COLUMN quota TYPE BIGINT USING quota::bigint,
  ALTER COLUMN used_quota TYPE BIGINT USING used_quota::bigint,
  ALTER COLUMN aff_quota TYPE BIGINT USING aff_quota::bigint,
  ALTER COLUMN aff_history TYPE BIGINT USING aff_history::bigint;

ALTER TABLE tokens
  ALTER COLUMN remain_quota TYPE BIGINT USING remain_quota::bigint,
  ALTER COLUMN used_quota TYPE BIGINT USING used_quota::bigint;

ALTER TABLE redemptions
  ALTER COLUMN quota TYPE BIGINT USING quota::bigint;

ALTER TABLE top_ups
  ALTER COLUMN credit_quota_snapshot TYPE BIGINT USING credit_quota_snapshot::bigint;

COMMIT;

-- Verify:
-- SELECT table_name, column_name, data_type
-- FROM information_schema.columns
-- WHERE (table_name, column_name) IN (
--   ('users','quota'), ('users','used_quota'), ('users','aff_quota'),
--   ('users','aff_history'), ('tokens','remain_quota'),
--   ('tokens','used_quota'), ('redemptions','quota'),
--   ('top_ups','credit_quota_snapshot')
-- )
-- ORDER BY table_name, column_name;
