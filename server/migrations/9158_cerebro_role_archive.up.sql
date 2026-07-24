-- FIR-3388: Roles are durable, versioned permission profiles.
--
-- Archiving preserves the exact profile that explained historical access while
-- removing it from every new effective-access calculation. Existing task
-- mandates remain immutable snapshots and can still only be tightened by the
-- live policy gate.

ALTER TABLE cerebro_role
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_cerebro_role_workspace_active
    ON cerebro_role (workspace_id, lower(name))
    WHERE archived_at IS NULL;

-- Role APIs and the permission resolver identify a member by the shared user
-- id. Older workspace-copy runs wrote the workspace-local member id instead,
-- which made the copied Role visible but ineffective. Canonicalise those rows
-- before Roles become an operator-facing permission profile. If a canonical
-- assignment already exists, retain it and remove only its legacy duplicate.
DELETE FROM cerebro_role_assignment legacy
USING member m
WHERE legacy.subject_type = 'member'
  AND legacy.subject_id = m.id
  AND EXISTS (
      SELECT 1
      FROM cerebro_role_assignment canonical
      WHERE canonical.role_id = legacy.role_id
        AND canonical.subject_type = 'member'
        AND canonical.subject_id = m.user_id
  );

UPDATE cerebro_role_assignment assignment
SET subject_id = m.user_id
FROM member m
WHERE assignment.subject_type = 'member'
  AND assignment.subject_id = m.id;
