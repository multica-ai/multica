-- Fork-specific: Skill ownership, approvers, versioning, change requests, and forks.
-- Implements the model from JEH-216:
--   * Every skill has an owner (separate from creator) and a list of approvers.
--   * Changes proposed by non-owners go through a change-request workflow that
--     must be approved by the owner or an assigned approver.
--   * Each merge produces a snapshot in skill_version (semver-tagged) so we can
--     diff between versions and roll back.
--   * Forks live as full skill rows with a parent pointer (skill_fork), so they
--     get their own owner/approvers/versions and are visible alongside the parent.

ALTER TABLE skill
    ADD COLUMN IF NOT EXISTS owner_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS approver_ids UUID[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS current_version TEXT NOT NULL DEFAULT '1.0.0';

CREATE INDEX IF NOT EXISTS idx_skill_owner ON skill(owner_id);

-- Version history: append-only snapshots of skill content + files at the time
-- of merge. The current_version on the skill table points at the most recent
-- entry here. Files are stored as a JSONB array snapshot to keep the row
-- self-contained — the live skill_file rows can drift after the version is cut.
CREATE TABLE IF NOT EXISTS skill_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id UUID NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    content TEXT NOT NULL,
    files JSONB NOT NULL DEFAULT '[]',
    description TEXT NOT NULL DEFAULT '',
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (skill_id, version)
);

CREATE INDEX IF NOT EXISTS idx_skill_version_skill ON skill_version(skill_id);

-- Change requests: a proposed edit to a skill that is not yet merged. Stores
-- the full proposed content/files so the proposal can be reviewed independently
-- of the live skill. status transitions: pending → approved|rejected, and
-- approved → merged when the owner/approver applies it.
CREATE TABLE IF NOT EXISTS skill_change_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id UUID NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    base_version TEXT NOT NULL,
    proposed_version TEXT NOT NULL,
    proposed_content TEXT NOT NULL,
    proposed_files JSONB NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'merged')),
    proposed_by UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    reviewed_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    review_comment TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_skill_change_request_skill
    ON skill_change_request(skill_id);
CREATE INDEX IF NOT EXISTS idx_skill_change_request_status
    ON skill_change_request(status)
    WHERE status = 'pending';

-- Forks: a directed link from a parent skill to a forked skill. The forked
-- skill is a full skill row with its own owner/version history. The link
-- preserves lineage so the UI can show "forked from X" / "forks: Y, Z".
CREATE TABLE IF NOT EXISTS skill_fork (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_skill_id UUID NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    forked_skill_id UUID NOT NULL UNIQUE REFERENCES skill(id) ON DELETE CASCADE,
    forked_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_skill_fork_parent ON skill_fork(parent_skill_id);

-- Backfill: existing skills should have owner = creator so the ownership rule
-- is enforceable from day one. Skills without a creator (system-imported) are
-- left ownerless — the workspace admin/owner fallback in the permission hook
-- handles that case.
UPDATE skill SET owner_id = created_by WHERE owner_id IS NULL AND created_by IS NOT NULL;

-- Backfill: every existing skill gets an initial v1.0.0 snapshot so the
-- version-history view has something to render and future change requests
-- have a base_version to diff against.
INSERT INTO skill_version (skill_id, version, content, description, created_by, created_at)
SELECT id, current_version, content, description, created_by, created_at
FROM skill
WHERE NOT EXISTS (
    SELECT 1 FROM skill_version WHERE skill_version.skill_id = skill.id
);
