-- Add GitLab tracking columns to comment_reaction, mirroring what migration
-- 050 added to issue_reaction. Needed for Phase 3d write-through of
-- comment reactions (award_emoji on GitLab notes).

ALTER TABLE comment_reaction
    ADD COLUMN gitlab_award_id BIGINT,
    ADD COLUMN external_updated_at TIMESTAMPTZ,
    ADD COLUMN gitlab_actor_user_id BIGINT;

-- Unique partial index so webhook sync (or future write-through) can
-- idempotently upsert by GitLab's award_id without stepping on Multica-
-- native (pre-connection) reactions that have NULL gitlab_award_id.
CREATE UNIQUE INDEX comment_reaction_gitlab_award_id_unique
    ON comment_reaction (gitlab_award_id)
    WHERE gitlab_award_id IS NOT NULL;
