CREATE TABLE incident_source_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('pagerduty', 'datadog')),
    display_name TEXT NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 120),
    provider_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    credential_envelope BYTEA NOT NULL,
    credential_envelope_version SMALLINT NOT NULL DEFAULT 1 CHECK (credential_envelope_version = 1),
    status TEXT NOT NULL DEFAULT 'untested' CHECK (status IN (
        'untested','healthy','authentication_failed','permission_denied','unreachable',
        'rate_limited','provider_error','configuration_error','timeout'
    )),
    last_diagnostic_code TEXT,
    last_checked_at TIMESTAMPTZ,
    credential_updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, provider, display_name)
);

CREATE INDEX incident_source_connections_workspace_idx
    ON incident_source_connections (workspace_id, created_at DESC, id);

