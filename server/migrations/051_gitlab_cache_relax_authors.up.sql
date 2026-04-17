-- Phase 2a fix-up: synced GitLab notes and award reactions don't yet have a
-- mapping from GitLab user → Multica member (Phase 3 introduces
-- user_gitlab_connection lookup). Relax the NOT NULL constraints so synced
-- rows can leave the Multica refs empty and store the GitLab user id instead
-- for native display. Phase 3 will backfill author_id/actor_id where users
-- have connected.

-- comment: relax NOT NULL on author_type/author_id; add gitlab_author_user_id.
ALTER TABLE comment ALTER COLUMN author_type DROP NOT NULL;
ALTER TABLE comment ALTER COLUMN author_id   DROP NOT NULL;
ALTER TABLE comment ADD COLUMN gitlab_author_user_id BIGINT;

-- issue_reaction: relax NOT NULL on actor_type/actor_id; add gitlab_actor_user_id.
ALTER TABLE issue_reaction ALTER COLUMN actor_type DROP NOT NULL;
ALTER TABLE issue_reaction ALTER COLUMN actor_id   DROP NOT NULL;
ALTER TABLE issue_reaction ADD COLUMN gitlab_actor_user_id BIGINT;
