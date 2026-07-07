-- Space ownership additive rollout.
--
-- This migration deliberately stops before the numbering cutover:
-- issue.space_id and autopilot.space_id are nullable here, and
-- uq_issue_workspace_number remains in place. Migration B can only run after
-- all write and resolver paths are Space-aware.

-- Deterministically normalize each workspace's legacy issue_prefix into a Space
-- key that satisfies the workspace_space CHECK (^[A-Z][A-Z0-9]{0,6}$). Legacy
-- data legitimately contains 8-10 char prefixes (the old UI allowed
-- slice(0, 10)) and digit-first prefixes, so aborting the deploy on those would
-- brick upgrades. Instead we normalize: uppercase, strip non-[A-Z0-9],
-- truncate to 7; if the result is digit-first, prefix 'T' and re-truncate to 7;
-- if nothing usable remains, fall back to 'SPACE'. The default-space backfill and
-- every other prefix consumer in this migration use the same function so the
-- seeded key can never diverge from what is emitted below.
CREATE FUNCTION pg_temp.normalize_space_key(prefix text) RETURNS text AS $$
DECLARE
    cleaned text;
BEGIN
    cleaned := left(regexp_replace(upper(coalesce(prefix, '')), '[^A-Z0-9]', '', 'g'), 7);
    IF cleaned ~ '^[0-9]' THEN
        cleaned := left('T' || cleaned, 7);
    END IF;
    IF cleaned = '' THEN
        cleaned := 'SPACE';
    END IF;
    RETURN cleaned;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

DO $$
DECLARE
    rec record;
    counter_regression_count integer;
BEGIN
    -- Surface every prefix that had to be rewritten so operators can reconcile
    -- external references to the old key. Informational only, never fatal.
    FOR rec IN
        SELECT w.id AS workspace_id,
               upper(btrim(w.issue_prefix)) AS old_key,
               pg_temp.normalize_space_key(w.issue_prefix) AS new_key
        FROM workspace w
        WHERE pg_temp.normalize_space_key(w.issue_prefix) <> upper(btrim(w.issue_prefix))
        ORDER BY w.id
    LOOP
        RAISE NOTICE 'workspace_space: normalized issue_prefix for workspace % (% -> %)',
            rec.workspace_id, rec.old_key, rec.new_key;
    END LOOP;

    SELECT count(*) INTO counter_regression_count
    FROM workspace w
    WHERE w.issue_counter < (
        SELECT COALESCE(max(i.number), 0)
        FROM issue i
        WHERE i.workspace_id = w.id
    );

    IF counter_regression_count > 0 THEN
        RAISE EXCEPTION 'workspace_space preflight failed: % workspace.issue_counter values are below max(issue.number)', counter_regression_count;
    END IF;
END $$;

CREATE TABLE workspace_space (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    key TEXT NOT NULL CHECK (key ~ '^[A-Z][A-Z0-9]{0,6}$'),
    description TEXT NOT NULL DEFAULT '',
    icon TEXT,
    issue_counter INT NOT NULL DEFAULT 0 CHECK (issue_counter >= 0),
    is_default BOOLEAN NOT NULL DEFAULT false,
    archived_at TIMESTAMPTZ,
    archived_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, id)
);

CREATE UNIQUE INDEX uq_workspace_space_workspace_key_lower
    ON workspace_space(workspace_id, lower(key));
CREATE UNIQUE INDEX uq_workspace_space_default
    ON workspace_space(workspace_id)
    WHERE is_default;
CREATE INDEX idx_workspace_space_active
    ON workspace_space(workspace_id)
    WHERE archived_at IS NULL;

CREATE TABLE workspace_space_member (
    workspace_id UUID NOT NULL,
    space_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('lead', 'member')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (space_id, user_id),
    FOREIGN KEY (workspace_id, space_id) REFERENCES workspace_space(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, user_id) REFERENCES member(workspace_id, user_id) ON DELETE CASCADE
);

ALTER TABLE project
    ADD CONSTRAINT uq_project_workspace_id UNIQUE (workspace_id, id);

CREATE TABLE project_space (
    workspace_id UUID NOT NULL,
    project_id UUID NOT NULL,
    space_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, space_id),
    FOREIGN KEY (workspace_id, project_id) REFERENCES project(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, space_id) REFERENCES workspace_space(workspace_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_project_space_workspace_space
    ON project_space(workspace_id, space_id);

ALTER TABLE issue
    ADD COLUMN space_id UUID;

ALTER TABLE issue
    ADD CONSTRAINT fk_issue_workspace_space
    FOREIGN KEY (workspace_id, space_id) REFERENCES workspace_space(workspace_id, id);

CREATE INDEX idx_issue_workspace_space_status_position
    ON issue(workspace_id, space_id, status, position);
CREATE INDEX idx_issue_workspace_space_created_at
    ON issue(workspace_id, space_id, created_at DESC);
CREATE INDEX idx_issue_project_space
    ON issue(workspace_id, project_id, space_id)
    WHERE project_id IS NOT NULL;

ALTER TABLE autopilot
    ADD COLUMN space_id UUID;

ALTER TABLE autopilot
    ADD CONSTRAINT fk_autopilot_workspace_space
    FOREIGN KEY (workspace_id, space_id) REFERENCES workspace_space(workspace_id, id);

CREATE INDEX idx_autopilot_workspace_space
    ON autopilot(workspace_id, space_id);

INSERT INTO workspace_space (workspace_id, name, key, issue_counter, is_default, created_by)
SELECT
    w.id,
    w.name,
    pg_temp.normalize_space_key(w.issue_prefix),
    w.issue_counter,
    true,
    (
        SELECT m.user_id
        FROM member m
        WHERE m.workspace_id = w.id
        ORDER BY (m.role = 'owner') DESC, m.created_at ASC
        LIMIT 1
    )
FROM workspace w;

INSERT INTO workspace_space_member (workspace_id, space_id, user_id, role)
SELECT wt.workspace_id, wt.id, m.user_id,
       CASE WHEN m.role IN ('owner', 'admin') THEN 'lead' ELSE 'member' END
FROM workspace_space wt
JOIN member m ON m.workspace_id = wt.workspace_id
WHERE wt.is_default;

UPDATE issue i
SET space_id = wt.id
FROM workspace_space wt
WHERE wt.workspace_id = i.workspace_id
  AND wt.is_default
  AND i.space_id IS NULL;

UPDATE autopilot a
SET space_id = wt.id
FROM workspace_space wt
WHERE wt.workspace_id = a.workspace_id
  AND wt.is_default
  AND a.space_id IS NULL;

INSERT INTO project_space (workspace_id, project_id, space_id)
SELECT p.workspace_id, p.id, wt.id
FROM project p
JOIN workspace_space wt ON wt.workspace_id = p.workspace_id AND wt.is_default;

DROP FUNCTION pg_temp.normalize_space_key(text);
