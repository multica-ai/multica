-- Adds an agent mode column to control whether an agent operates as a
-- coding agent, an operational (business) agent, or a hybrid of both.
-- Coding is the default — all existing agents keep their current behavior.
-- Operational agents receive different prompts (no repo context, no
-- "you are a coding agent" preamble) and workflow instructions that
-- emphasize MCP tools and business task execution.
ALTER TABLE agent ADD COLUMN mode TEXT NOT NULL DEFAULT 'coding';
