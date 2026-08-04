-- Directory-based agents: a directory is the whole agent (its CLAUDE.md /
-- skills / MCP config come from that directory). Stores the absolute path the
-- agent's tasks run in. NULL means the agent uses the standard per-task envRoot
-- workdir (or the project-level local_directory when one is attached).
ALTER TABLE agent ADD COLUMN local_directory TEXT;
