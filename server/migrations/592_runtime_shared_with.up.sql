-- Explicit workspace user grants keep visibility private for older servers.
ALTER TABLE agent_runtime ADD COLUMN shared_with uuid[] NOT NULL DEFAULT '{}';
