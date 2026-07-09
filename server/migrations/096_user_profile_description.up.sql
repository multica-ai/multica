-- Per-user free-form profile description. Read by the daemon at task
-- start and injected into the agent brief under "## Requesting User" so
-- the agent has cheap, durable context about who is asking (role,
-- stack, preferences). NOT NULL DEFAULT '' so userToResponse never has
-- to coalesce nullable state on the read path.

ALTER TABLE "user"
    ADD COLUMN profile_description TEXT NOT NULL DEFAULT '';
