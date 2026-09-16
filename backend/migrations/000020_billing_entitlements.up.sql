CREATE TABLE billing_accounts (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    plan_id text NOT NULL DEFAULT 'developer' CHECK (plan_id IN ('developer','starter','pro','organization','enterprise')),
    status text NOT NULL DEFAULT 'free' CHECK (status IN ('free','trialing','active','past_due','canceled','suspended')),
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL CHECK (period_end > period_start),
    catalog_version text NOT NULL CHECK (catalog_version <> ''),
    provider text,
    customer_ref text,
    subscription_ref text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO billing_accounts(workspace_id, plan_id, status, period_start, period_end, catalog_version)
SELECT id, 'developer', 'free', date_trunc('month', now()), date_trunc('month', now()) + interval '1 month', 'billing-catalog-contract:v1'
FROM workspaces ON CONFLICT (workspace_id) DO NOTHING;

CREATE UNIQUE INDEX billing_accounts_provider_customer_unique
    ON billing_accounts(provider, customer_ref) WHERE provider IS NOT NULL AND customer_ref IS NOT NULL;
CREATE UNIQUE INDEX billing_accounts_provider_subscription_unique
    ON billing_accounts(provider, subscription_ref) WHERE provider IS NOT NULL AND subscription_ref IS NOT NULL;

CREATE TABLE billing_usage_ledger (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('grant','reservation','commit','release','refund','adjustment')),
    quantity bigint NOT NULL CHECK ((kind = 'adjustment') OR quantity > 0),
    meter_contract text NOT NULL,
    catalog_contract text NOT NULL,
    idempotency_key text NOT NULL,
    suite_execution_id uuid REFERENCES suite_executions(id) ON DELETE SET NULL,
    run_id uuid REFERENCES runs(id) ON DELETE SET NULL,
    batch_id uuid REFERENCES evaluation_batches(id) ON DELETE SET NULL,
    scenario_version_id uuid REFERENCES scenario_versions(id) ON DELETE SET NULL,
    environment_profile text,
    provider text,
    isolation_tier text,
    reason text NOT NULL DEFAULT '',
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, idempotency_key)
);
CREATE INDEX billing_usage_ledger_workspace_period_idx ON billing_usage_ledger(workspace_id, created_at DESC);
CREATE INDEX billing_usage_ledger_run_idx ON billing_usage_ledger(run_id) WHERE run_id IS NOT NULL;

CREATE TABLE billing_provider_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider text NOT NULL,
    provider_event_id text NOT NULL,
    event_type text NOT NULL,
    workspace_id uuid REFERENCES workspaces(id) ON DELETE SET NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL CHECK (status IN ('received','applied','duplicate','failed','stale')),
    failure_reason text,
    received_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    UNIQUE(provider, provider_event_id)
);
CREATE INDEX billing_provider_events_status_idx ON billing_provider_events(status, received_at);

