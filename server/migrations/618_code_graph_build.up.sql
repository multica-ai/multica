ALTER TABLE code_graph_repository ADD COLUMN IF NOT EXISTS active_build_id uuid;
ALTER TABLE code_graph_repository ADD COLUMN IF NOT EXISTS symbol_count integer NOT NULL DEFAULT 0;
ALTER TABLE code_graph_repository ADD COLUMN IF NOT EXISTS edge_count integer NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS code_graph_build (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('queued','processing','ready','failed')),
    error text,
    commit_hash text NOT NULL DEFAULT '',
    file_count integer NOT NULL DEFAULT 0,
    symbol_count integer NOT NULL DEFAULT 0,
    edge_count integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz
);
