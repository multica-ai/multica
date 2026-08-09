CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_family (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    active_policy_id UUID,
    current_draft_revision_id UUID,
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id)
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'hook_policy_family_workspace_unique'
    ) THEN
        ALTER TABLE cerebro_workflow_hook_policy
            ADD CONSTRAINT hook_policy_family_workspace_unique
            UNIQUE (workspace_id, family_id, id);
    END IF;
END
$$;

INSERT INTO cerebro_workflow_hook_family (id, workspace_id, created_at, updated_at)
SELECT family_id, workspace_id, MIN(created_at), MAX(updated_at)
FROM cerebro_workflow_hook_policy
GROUP BY workspace_id, family_id
ON CONFLICT (id) DO NOTHING;

WITH latest_non_draft AS (
    SELECT DISTINCT ON (workspace_id, family_id)
        workspace_id, family_id, id, mode
    FROM cerebro_workflow_hook_policy
    WHERE mode <> 'dry_run'
    ORDER BY workspace_id, family_id, policy_version DESC, updated_at DESC
)
UPDATE cerebro_workflow_hook_family family
SET active_policy_id = latest.id,
    disabled = latest.mode = 'off',
    updated_at = now()
FROM latest_non_draft latest
WHERE family.workspace_id = latest.workspace_id
  AND family.id = latest.family_id;

CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_draft_series (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'published', 'discarded')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    discarded_at TIMESTAMPTZ,
    UNIQUE (workspace_id, family_id, id),
    FOREIGN KEY (workspace_id, family_id)
        REFERENCES cerebro_workflow_hook_family (workspace_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS hook_one_active_draft_series_per_family
    ON cerebro_workflow_hook_draft_series (workspace_id, family_id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_draft_revision (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    draft_series_id UUID NOT NULL,
    family_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    candidate_version INTEGER NOT NULL CHECK (candidate_version > 0),
    revision INTEGER NOT NULL CHECK (revision > 0),
    configuration JSONB NOT NULL,
    configuration_hash TEXT NOT NULL,
    published_policy_id UUID,
    created_by_id UUID NOT NULL,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, draft_series_id, revision),
    UNIQUE (workspace_id, family_id, id),
    FOREIGN KEY (workspace_id, family_id, draft_series_id)
        REFERENCES cerebro_workflow_hook_draft_series (workspace_id, family_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, family_id, published_policy_id)
        REFERENCES cerebro_workflow_hook_policy (workspace_id, family_id, id)
);

WITH latest_dry_run AS (
    SELECT DISTINCT ON (policy.workspace_id, policy.family_id)
        policy.*
    FROM cerebro_workflow_hook_policy policy
    WHERE policy.mode = 'dry_run'
      AND NOT EXISTS (
          SELECT 1
          FROM cerebro_workflow_hook_policy newer
          WHERE newer.workspace_id = policy.workspace_id
            AND newer.family_id = policy.family_id
            AND newer.policy_version > policy.policy_version
      )
    ORDER BY policy.workspace_id, policy.family_id, policy.policy_version DESC
)
INSERT INTO cerebro_workflow_hook_draft_series (family_id, workspace_id, created_at)
SELECT family_id, workspace_id, created_at
FROM latest_dry_run
ON CONFLICT (workspace_id, family_id) WHERE status = 'active' DO NOTHING;

WITH latest_dry_run AS (
    SELECT DISTINCT ON (policy.workspace_id, policy.family_id)
        policy.*
    FROM cerebro_workflow_hook_policy policy
    WHERE policy.mode = 'dry_run'
      AND NOT EXISTS (
          SELECT 1
          FROM cerebro_workflow_hook_policy newer
          WHERE newer.workspace_id = policy.workspace_id
            AND newer.family_id = policy.family_id
            AND newer.policy_version > policy.policy_version
      )
    ORDER BY policy.workspace_id, policy.family_id, policy.policy_version DESC
)
INSERT INTO cerebro_workflow_hook_draft_revision (
    draft_series_id, family_id, workspace_id, candidate_version, revision,
    configuration, configuration_hash, created_by_id, created_by_type, created_at, updated_at
)
SELECT
    series.id,
    policy.family_id,
    policy.workspace_id,
    policy.policy_version,
    1,
    jsonb_build_object(
        'name', policy.name,
        'description', policy.description,
        'mode', 'dry_run',
        'fail_mode', policy.fail_mode,
        'condition_mode', policy.condition_mode,
        'events', policy.event_types,
        'conditions', policy.conditions,
        'bindings', COALESCE((
            SELECT jsonb_agg(jsonb_build_object(
                'kind', binding.scope_kind,
                'id', binding.scope_id,
                'priority', binding.priority
            ) ORDER BY binding.priority DESC, binding.created_at)
            FROM cerebro_workflow_hook_binding binding
            WHERE binding.policy_id = policy.id
        ), '[]'::jsonb),
        'handlers', COALESCE((
            SELECT jsonb_agg(jsonb_build_object(
                'id', handler.id,
                'decision', handler.decision,
                'requirement', handler.requirement,
                'modifications', handler.modifications,
                'actions', handler.actions
            ) ORDER BY handler.position)
            FROM cerebro_workflow_hook_handler handler
            WHERE handler.policy_id = policy.id
        ), '[]'::jsonb)
    ),
    encode(digest(
        jsonb_build_object(
            'name', policy.name,
            'description', policy.description,
            'fail_mode', policy.fail_mode,
            'condition_mode', policy.condition_mode,
            'events', policy.event_types,
            'conditions', policy.conditions
        )::text,
        'sha256'
    ), 'hex'),
    policy.created_by_id,
    policy.created_by_type,
    policy.created_at,
    policy.updated_at
FROM latest_dry_run policy
JOIN cerebro_workflow_hook_draft_series series
  ON series.workspace_id = policy.workspace_id
 AND series.family_id = policy.family_id
 AND series.status = 'active'
ON CONFLICT (workspace_id, draft_series_id, revision) DO NOTHING;

UPDATE cerebro_workflow_hook_family family
SET current_draft_revision_id = revision.id,
    updated_at = now()
FROM cerebro_workflow_hook_draft_revision revision
WHERE revision.workspace_id = family.workspace_id
  AND revision.family_id = family.id;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'hook_family_active_policy_fk'
    ) THEN
        ALTER TABLE cerebro_workflow_hook_family
            ADD CONSTRAINT hook_family_active_policy_fk
            FOREIGN KEY (workspace_id, id, active_policy_id)
            REFERENCES cerebro_workflow_hook_policy (workspace_id, family_id, id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'hook_family_current_draft_fk'
    ) THEN
        ALTER TABLE cerebro_workflow_hook_family
            ADD CONSTRAINT hook_family_current_draft_fk
            FOREIGN KEY (workspace_id, id, current_draft_revision_id)
            REFERENCES cerebro_workflow_hook_draft_revision (workspace_id, family_id, id);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS hook_family_workspace_updated
    ON cerebro_workflow_hook_family (workspace_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS hook_draft_revision_family_created
    ON cerebro_workflow_hook_draft_revision (workspace_id, family_id, created_at DESC);
