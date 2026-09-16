DROP INDEX IF EXISTS runs_active_lease_expiry_idx;

ALTER TABLE runs
  DROP CONSTRAINT IF EXISTS runs_worker_id_length,
  DROP COLUMN IF EXISTS lease_expires_at,
  DROP COLUMN IF EXISTS heartbeat_at,
  DROP COLUMN IF EXISTS worker_id;
