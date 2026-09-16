ALTER TABLE runs
  ADD COLUMN worker_id TEXT,
  ADD COLUMN heartbeat_at TIMESTAMPTZ,
  ADD COLUMN lease_expires_at TIMESTAMPTZ,
  ADD CONSTRAINT runs_worker_id_length CHECK (worker_id IS NULL OR (length(worker_id) BETWEEN 1 AND 200));

-- Give pre-lease active work a bounded rollout grace instead of immediately
-- treating it as abandoned while an old worker may still be shutting down.
UPDATE runs
SET worker_id = 'pre-lease-worker',
    heartbeat_at = now(),
    lease_expires_at = now() + interval '5 minutes'
WHERE status IN ('provisioning', 'running');

CREATE INDEX runs_active_lease_expiry_idx
  ON runs (lease_expires_at, id)
  WHERE status IN ('provisioning', 'running');
