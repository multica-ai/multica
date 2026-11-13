-- Targetless actions cannot be represented by the previous schema.
-- Refuse rollback rather than destroy saved actions, including archived ones.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM quick_action WHERE assignee_type = '') THEN
        RAISE EXCEPTION 'cannot roll back 584: quick actions without a target still exist'
            USING HINT = 'Assign an agent or squad to every targetless quick action, including archived actions, before retrying; this migration will not delete user data.';
    END IF;
END
$$;

ALTER TABLE quick_action
    DROP CONSTRAINT IF EXISTS quick_action_assignee_type_check,
    ALTER COLUMN assignee_id SET NOT NULL,
    ADD CONSTRAINT quick_action_assignee_type_check
        CHECK (assignee_type IN ('agent', 'squad'));
