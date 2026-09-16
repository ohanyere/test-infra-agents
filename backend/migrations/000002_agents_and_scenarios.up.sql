CREATE TABLE agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_workspace_id UUID REFERENCES workspaces(id) ON DELETE SET NULL,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    visibility TEXT NOT NULL DEFAULT 'workspace' CHECK (visibility IN ('public', 'workspace', 'private')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX agents_global_slug_uidx ON agents (slug) WHERE owner_workspace_id IS NULL;
CREATE UNIQUE INDEX agents_workspace_slug_uidx ON agents (owner_workspace_id, slug) WHERE owner_workspace_id IS NOT NULL;
CREATE INDEX agents_owner_workspace_id_idx ON agents (owner_workspace_id) WHERE owner_workspace_id IS NOT NULL;
CREATE INDEX agents_visibility_idx ON agents (visibility);

CREATE TABLE agent_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    version_label TEXT NOT NULL,
    interface_kind TEXT NOT NULL DEFAULT 'unknown' CHECK (interface_kind IN ('unknown', 'http', 'mcp', 'webhook', 'container')),
    capabilities TEXT[] NOT NULL DEFAULT '{}',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_id, version_label)
);

COMMENT ON COLUMN agent_versions.metadata IS 'Non-sensitive metadata only. Credentials, secrets, tokens, and connection URLs must not be stored here.';
CREATE INDEX agent_versions_capabilities_gin_idx ON agent_versions USING GIN (capabilities);

CREATE TABLE scenarios (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_workspace_id UUID REFERENCES workspaces(id) ON DELETE SET NULL,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('built_in', 'custom')),
    visibility TEXT NOT NULL DEFAULT 'workspace' CHECK (visibility IN ('public', 'workspace', 'private')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX scenarios_global_slug_uidx ON scenarios (slug) WHERE owner_workspace_id IS NULL;
CREATE UNIQUE INDEX scenarios_workspace_slug_uidx ON scenarios (owner_workspace_id, slug) WHERE owner_workspace_id IS NOT NULL;
CREATE INDEX scenarios_owner_workspace_id_idx ON scenarios (owner_workspace_id) WHERE owner_workspace_id IS NOT NULL;
CREATE INDEX scenarios_kind_idx ON scenarios (kind);

CREATE TABLE scenario_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scenario_id UUID NOT NULL REFERENCES scenarios(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL,
    difficulty TEXT NOT NULL CHECK (difficulty IN ('easy', 'medium', 'hard')),
    duration_seconds INTEGER NOT NULL CHECK (duration_seconds > 0),
    environment JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(environment) = 'object'),
    conditions JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(conditions) = 'array'),
    observables TEXT[] NOT NULL DEFAULT '{}',
    expected_behaviors TEXT[] NOT NULL DEFAULT '{}',
    forbidden_behaviors TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scenario_id, version)
);

CREATE TABLE scenario_criteria (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scenario_version_id UUID NOT NULL REFERENCES scenario_versions(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    name TEXT NOT NULL,
    metric_key TEXT NOT NULL,
    operator TEXT NOT NULL CHECK (operator IN ('gt', 'gte', 'lt', 'lte', 'eq', 'neq', 'required')),
    target_value JSONB NOT NULL,
    unit TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scenario_version_id, key)
);

CREATE INDEX scenario_criteria_version_sort_idx ON scenario_criteria (scenario_version_id, sort_order);
