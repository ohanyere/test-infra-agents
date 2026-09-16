ALTER TABLE scenario_versions
  ADD COLUMN authoring_method TEXT NOT NULL DEFAULT 'manual'
    CHECK (authoring_method IN ('manual', 'description', 'incident_import')),
  ADD COLUMN agent_task TEXT NOT NULL DEFAULT '',
  ADD COLUMN agent_visible_context JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(agent_visible_context) = 'object'),
  ADD COLUMN allowed_actions TEXT[] NOT NULL DEFAULT '{}'::text[],
  ADD COLUMN allowed_resources JSONB NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(allowed_resources) = 'array'),
  ADD COLUMN ground_truth JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(ground_truth) = 'object'),
  ADD COLUMN execution_spec JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(execution_spec) = 'object'),
  ADD COLUMN source_metadata JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(source_metadata) = 'object');
