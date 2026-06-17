-- Hourly rollup table for knowledge effect analysis. Materialised in UTC.
-- Aggregates task efficiency metrics grouped by dimensions including
-- whether knowledge was injected, enabling "with injection" vs "without
-- injection" comparison queries.
--
-- Follows the same pattern as task_usage_hourly (migration 101):
--   * Hourly UTC buckets are tz-neutral — viewer-side tz applied at query time.
--   * UNIQUE NULLS NOT DISTINCT handles nullable dimensions (project_id).
--   * Separate duration_task_count tracks how many tasks contributed to
--     duration sums, so the UI can distinguish "no data" from "zero duration".
--
-- WHY HAS_INJECTION AS A DIMENSION:
--   The primary read pattern is comparing efficiency metrics between tasks
--   that received knowledge injection and those that didn't. Making it a
--   PK dimension lets a single query fetch both sides and pivot in the UI.
--
-- WHY NO KNOWLEDGE_ITEM_ID:
--   Per-item analytics continue to use the existing ListKnowledgeAnalytics
--   live query. This rollup focuses on the with/without injection comparison
--   across agent, project, model, and task type dimensions.
CREATE TABLE knowledge_effect_hourly (
    bucket_hour          TIMESTAMPTZ     NOT NULL,   -- UTC, truncated to hour boundary
    workspace_id         UUID            NOT NULL,
    agent_id             UUID            NOT NULL,
    project_id           UUID,                       -- nullable (tasks without project)
    model                TEXT            NOT NULL,
    provider             TEXT            NOT NULL,
    task_kind            TEXT            NOT NULL,   -- chat / autopilot / quick_create / comment / direct
    has_injection        BOOLEAN         NOT NULL,
    task_count           BIGINT          NOT NULL DEFAULT 0,
    successful_count     BIGINT          NOT NULL DEFAULT 0,
    failed_count         BIGINT          NOT NULL DEFAULT 0,
    total_duration_secs  DOUBLE PRECISION NOT NULL DEFAULT 0,
    duration_task_count  BIGINT          NOT NULL DEFAULT 0,
    input_tokens         BIGINT          NOT NULL DEFAULT 0,
    output_tokens        BIGINT          NOT NULL DEFAULT 0,
    cache_read_tokens    BIGINT          NOT NULL DEFAULT 0,
    cache_write_tokens   BIGINT          NOT NULL DEFAULT 0,
    rerun_count          BIGINT          NOT NULL DEFAULT 0,
    follow_up_count      BIGINT          NOT NULL DEFAULT 0,
    max_attempt          INT             NOT NULL DEFAULT 1,
    updated_at           TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT uq_knowledge_effect_hourly_key
        UNIQUE NULLS NOT DISTINCT
        (bucket_hour, workspace_id, agent_id, project_id, model, provider, task_kind, has_injection)
);

-- Workspace-wide trend (no other filter). Leading workspace_id matches
-- every dashboard query; bucket_hour DESC avoids an extra sort.
CREATE INDEX idx_knowledge_effect_hourly_workspace_time
    ON knowledge_effect_hourly (workspace_id, bucket_hour DESC);

-- Filtered by agent.
CREATE INDEX idx_knowledge_effect_hourly_workspace_agent_time
    ON knowledge_effect_hourly (workspace_id, agent_id, bucket_hour DESC);

-- Filtered by project. Partial because no-project buckets aggregate
-- separately and the filter excludes them.
CREATE INDEX idx_knowledge_effect_hourly_workspace_project_time
    ON knowledge_effect_hourly (workspace_id, project_id, bucket_hour DESC)
    WHERE project_id IS NOT NULL;

-- has_injection comparison is the primary read pattern.
CREATE INDEX idx_knowledge_effect_hourly_workspace_injection_time
    ON knowledge_effect_hourly (workspace_id, has_injection, bucket_hour DESC);

-- Single-row state table tracking the rollup worker's watermark.
CREATE TABLE knowledge_effect_hourly_rollup_state (
    id                    SMALLINT    PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    watermark_at          TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01 00:00:00+00',
    last_run_started_at   TIMESTAMPTZ,
    last_run_finished_at  TIMESTAMPTZ,
    last_run_rows         BIGINT      NOT NULL DEFAULT 0,
    last_error            TEXT
);
INSERT INTO knowledge_effect_hourly_rollup_state (id) VALUES (1) ON CONFLICT DO NOTHING;

-- Dirty queue for invalidations the updated_at watermark cannot see:
--   * DELETE on task_usage / agent_task_queue (no row left for watermark).
--   * UPDATE of issue.project_id — moves bucket to new key.
--   * INSERT/DELETE on knowledge_injection_event — changes has_injection.
--
-- TTL: rows pruned after 7 days by prune_knowledge_effect_hourly_dirty().
CREATE TABLE knowledge_effect_hourly_dirty (
    bucket_hour   TIMESTAMPTZ NOT NULL,
    workspace_id  UUID        NOT NULL,
    agent_id      UUID        NOT NULL,
    project_id    UUID,
    model         TEXT        NOT NULL,
    provider      TEXT        NOT NULL,
    task_kind     TEXT        NOT NULL,
    has_injection BOOLEAN     NOT NULL,
    enqueued_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_knowledge_effect_hourly_dirty_key
        UNIQUE NULLS NOT DISTINCT
        (bucket_hour, workspace_id, agent_id, project_id, model, provider, task_kind, has_injection)
);

CREATE INDEX idx_knowledge_effect_hourly_dirty_enqueued_at
    ON knowledge_effect_hourly_dirty (enqueued_at);
