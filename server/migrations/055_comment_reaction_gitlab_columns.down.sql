DROP INDEX IF EXISTS comment_reaction_gitlab_award_id_unique;
ALTER TABLE comment_reaction
    DROP COLUMN IF EXISTS gitlab_actor_user_id,
    DROP COLUMN IF EXISTS external_updated_at,
    DROP COLUMN IF EXISTS gitlab_award_id;
