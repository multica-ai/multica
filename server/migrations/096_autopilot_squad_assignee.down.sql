-- Reverts 096_autopilot_squad_assignee.up.sql.
-- Restoring the agent FK requires every assignee_id to reference a real
-- agent. Squad-assigned autopilots would dangle, so they are deleted here.
-- Operators should drain squad-assigned autopilots before rolling back if
-- they want to preserve the rows.

DROP INDEX IF EXISTS idx_autopilot_run_squad_id;

ALTER TABLE autopilot_run
    DROP COLUMN IF EXISTS squad_id;

DROP INDEX IF EXISTS idx_autopilot_assignee_type_id;

DELETE FROM autopilot WHERE assignee_type = 'squad';

ALTER TABLE autopilot
    DROP COLUMN IF EXISTS assignee_type;

ALTER TABLE autopilot
    ADD CONSTRAINT autopilot_assignee_id_fkey
        FOREIGN KEY (assignee_id) REFERENCES agent(id) ON DELETE CASCADE;
