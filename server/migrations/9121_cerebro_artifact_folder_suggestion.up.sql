-- FIR-2697 part 2: folder suggestions with human accept.
--
-- An agent that files a document/note can PROPOSE an existing folder (any folder
-- other than the automatic Agent Runs structure) instead of moving the artifact
-- outright. The artifact is only moved once a human accepts the proposal, so an
-- agent never relocates a person's document without sign-off. Notes and
-- Documents keep separate folder trees (artifact_folder.kind), so a suggestion
-- records which surface it belongs to and the accept path enforces the match.
--
-- One row per proposal. At most one 'pending' proposal may exist per artifact
-- (a fresh proposal supersedes the previous pending one), enforced by the
-- partial unique index below. Accept/reject moves the row to a terminal status
-- and never deletes it, so the decision trail survives.
CREATE TABLE IF NOT EXISTS cerebro_artifact_folder_suggestion (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    artifact_id       UUID NOT NULL REFERENCES artifact(id) ON DELETE CASCADE,
    folder_id         UUID NOT NULL REFERENCES artifact_folder(id) ON DELETE CASCADE,
    -- 'document' or 'note' — the folder tree this proposal targets. Mirrors
    -- artifact_folder.kind so the accept path can reject a cross-surface move.
    surface           TEXT NOT NULL,
    -- 'pending' | 'accepted' | 'rejected' | 'superseded'
    status            TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'accepted', 'rejected', 'superseded')),
    -- Optional short rationale the agent gives for the placement.
    reason            TEXT NOT NULL DEFAULT '',
    -- Who proposed: 'agent' or 'member'.
    suggested_by_type TEXT NOT NULL,
    suggested_by_id   UUID NOT NULL,
    -- The member who accepted or rejected (NULL while pending/superseded).
    resolved_by_id    UUID,
    resolved_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- At most one live proposal per artifact.
CREATE UNIQUE INDEX IF NOT EXISTS uq_cerebro_afs_pending_artifact
    ON cerebro_artifact_folder_suggestion (artifact_id)
    WHERE status = 'pending';

-- Review-inbox lookups: pending proposals per workspace/surface.
CREATE INDEX IF NOT EXISTS idx_cerebro_afs_workspace_status
    ON cerebro_artifact_folder_suggestion (workspace_id, surface, status);
