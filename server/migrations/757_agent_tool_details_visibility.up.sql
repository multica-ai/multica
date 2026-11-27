-- Who can read the inputs and outputs of an agent's tool calls in its run logs
-- (MUL-10284). 'everyone' is every member who can see the run, as before.
-- 'restricted' is the agent's owner and workspace owners/admins only; everyone
-- else still sees which tools ran and for how long. Values are checked by the
-- API, not a constraint, so a later value needs no table rewrite.
ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS tool_details_visibility TEXT NOT NULL DEFAULT 'everyone';
