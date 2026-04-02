ALTER TABLE agent_runtime ADD COLUMN owner_id UUID REFERENCES "user"(id);
ALTER TABLE agent_runtime ADD COLUMN visibility TEXT NOT NULL DEFAULT 'workspace';
ALTER TABLE agent_runtime ADD CONSTRAINT agent_runtime_visibility_check CHECK (visibility IN ('workspace', 'private'));
