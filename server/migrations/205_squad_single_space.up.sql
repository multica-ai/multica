-- A Squad is an operating unit inside one Space, not a Workspace-wide asset.
-- Existing Squads may already be referenced by Issues or Autopilots in several
-- Spaces. Preserve those assignments by cloning the Squad (including its
-- roster) once per additional Space and rewiring references; never move an
-- Issue and never silently drop an assignee.
ALTER TABLE squad ADD COLUMN space_id UUID;

-- Keep the one-off split map inside a procedural block. Besides making its
-- lifecycle explicit, this prevents schema generators from treating the
-- migration-only temp table as a product model.
DO $migration$
BEGIN
CREATE TEMP TABLE _squad_space_map ON COMMIT DROP AS
WITH referenced AS (
    SELECT s.id AS original_squad_id, s.workspace_id, i.space_id, count(*)::bigint AS reference_count
    FROM squad s
    JOIN issue i
      ON i.workspace_id = s.workspace_id
     AND i.assignee_type = 'squad'
     AND i.assignee_id = s.id
    GROUP BY s.id, s.workspace_id, i.space_id

    UNION ALL

    SELECT s.id AS original_squad_id, s.workspace_id, a.space_id, count(*)::bigint AS reference_count
    FROM squad s
    JOIN autopilot a
      ON a.workspace_id = s.workspace_id
     AND a.assignee_type = 'squad'
     AND a.assignee_id = s.id
    GROUP BY s.id, s.workspace_id, a.space_id
), aggregated AS (
    SELECT original_squad_id, workspace_id, space_id, sum(reference_count)::bigint AS reference_count
    FROM referenced
    GROUP BY original_squad_id, workspace_id, space_id
), candidates AS (
    SELECT original_squad_id, workspace_id, space_id, reference_count
    FROM aggregated

    UNION ALL

    SELECT s.id, s.workspace_id, d.id, 0::bigint
    FROM squad s
    JOIN workspace_space d
      ON d.workspace_id = s.workspace_id
     AND d.is_default
    WHERE NOT EXISTS (
        SELECT 1 FROM aggregated a WHERE a.original_squad_id = s.id
    )
), ranked AS (
    SELECT *, row_number() OVER (
        PARTITION BY original_squad_id
        ORDER BY reference_count DESC, space_id ASC
    ) AS space_rank
    FROM candidates
)
SELECT
    original_squad_id,
    workspace_id,
    space_id,
    CASE WHEN space_rank = 1 THEN original_squad_id ELSE gen_random_uuid() END AS mapped_squad_id,
    space_rank
FROM ranked;

-- Clone the Squad definition and membership for every additional Space.
INSERT INTO squad (
    id, workspace_id, space_id, name, description, leader_id, creator_id,
    created_at, updated_at, archived_at, archived_by, avatar_url, instructions
)
SELECT
    m.mapped_squad_id, s.workspace_id, m.space_id, s.name, s.description,
    s.leader_id, s.creator_id, s.created_at, s.updated_at, s.archived_at,
    s.archived_by, s.avatar_url, s.instructions
FROM _squad_space_map m
JOIN squad s ON s.id = m.original_squad_id
WHERE m.space_rank > 1;

INSERT INTO squad_member (id, squad_id, member_type, member_id, role, created_at)
SELECT gen_random_uuid(), m.mapped_squad_id, sm.member_type, sm.member_id, sm.role, sm.created_at
FROM _squad_space_map m
JOIN squad_member sm ON sm.squad_id = m.original_squad_id
WHERE m.space_rank > 1;

UPDATE squad s
SET space_id = m.space_id
FROM _squad_space_map m
WHERE m.original_squad_id = s.id AND m.space_rank = 1;

UPDATE issue i
SET assignee_id = m.mapped_squad_id
FROM _squad_space_map m
WHERE i.assignee_type = 'squad'
  AND i.assignee_id = m.original_squad_id
  AND i.workspace_id = m.workspace_id
  AND i.space_id = m.space_id;

UPDATE autopilot a
SET assignee_id = m.mapped_squad_id
FROM _squad_space_map m
WHERE a.assignee_type = 'squad'
  AND a.assignee_id = m.original_squad_id
  AND a.workspace_id = m.workspace_id
  AND a.space_id = m.space_id;

-- Preserve historical attribution and queued leader briefings.
UPDATE autopilot_run r
SET squad_id = m.mapped_squad_id
FROM autopilot a, _squad_space_map m
WHERE r.autopilot_id = a.id
  AND r.squad_id = m.original_squad_id
  AND a.workspace_id = m.workspace_id
  AND a.space_id = m.space_id;

UPDATE agent_task_queue t
SET squad_id = m.mapped_squad_id
FROM issue i, _squad_space_map m
WHERE t.issue_id = i.id
  AND t.squad_id = m.original_squad_id
  AND i.workspace_id = m.workspace_id
  AND i.space_id = m.space_id;

UPDATE agent_task_queue t
SET squad_id = m.mapped_squad_id
FROM autopilot_run r, autopilot a, _squad_space_map m
WHERE t.autopilot_run_id = r.id
  AND r.autopilot_id = a.id
  AND t.squad_id = m.original_squad_id
  AND a.workspace_id = m.workspace_id
  AND a.space_id = m.space_id;

UPDATE agent_task_queue t
SET squad_id = m.mapped_squad_id,
    context = jsonb_set(t.context, '{squad_id}', to_jsonb(m.mapped_squad_id::text), false)
FROM _squad_space_map m
WHERE t.issue_id IS NULL
  AND t.autopilot_run_id IS NULL
  AND t.squad_id = m.original_squad_id
  AND t.context->>'type' = 'quick_create'
  AND t.context->>'workspace_id' = m.workspace_id::text
  AND t.context->>'space_id' = m.space_id::text;
END;
$migration$;

-- Compatibility for trusted/internal writers that predate the Space field
-- (old CLI versions, migrations, and operational scripts). Product API/UI
-- creation still requires an explicit Space; the database fallback is the
-- Workspace Default Space so even a legacy insert has deterministic ownership.
CREATE OR REPLACE FUNCTION set_squad_default_space()
RETURNS trigger AS $$
BEGIN
    IF NEW.space_id IS NULL THEN
        SELECT id INTO NEW.space_id
        FROM workspace_space
        WHERE workspace_id = NEW.workspace_id AND is_default
        LIMIT 1;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_squad_default_space
BEFORE INSERT ON squad
FOR EACH ROW EXECUTE FUNCTION set_squad_default_space();

ALTER TABLE squad ALTER COLUMN space_id SET NOT NULL;
ALTER TABLE squad ADD CONSTRAINT squad_workspace_id_id_unique UNIQUE (workspace_id, id);
ALTER TABLE squad ADD CONSTRAINT squad_workspace_space_fk
    FOREIGN KEY (workspace_id, space_id)
    REFERENCES workspace_space(workspace_id, id)
    ON DELETE CASCADE;

CREATE INDEX idx_squad_workspace_space
    ON squad(workspace_id, space_id, created_at);

COMMENT ON COLUMN squad.space_id IS
    'The single Space that owns this Squad. Issues and Autopilots may use it only in this Space.';
