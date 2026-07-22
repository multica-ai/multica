CREATE TABLE IF NOT EXISTS cerebro_command (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    command_key text NOT NULL,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    argv text[] NOT NULL,
    created_by_id uuid NOT NULL,
    created_by_type text NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_command_workspace_key_unique UNIQUE (workspace_id, command_key),
    CONSTRAINT cerebro_command_key_nonempty CHECK (command_key <> ''),
    CONSTRAINT cerebro_command_title_nonempty CHECK (title <> ''),
    CONSTRAINT cerebro_command_argv_nonempty CHECK (cardinality(argv) > 0)
);

CREATE INDEX IF NOT EXISTS cerebro_command_workspace_updated_idx
    ON cerebro_command (workspace_id, updated_at DESC);
