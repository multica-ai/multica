ALTER TABLE code_graph_build ADD COLUMN IF NOT EXISTS cancel_requested boolean NOT NULL DEFAULT false;
ALTER TABLE code_graph_build ADD COLUMN IF NOT EXISTS phase text NOT NULL DEFAULT 'unknown';
ALTER TABLE code_graph_build ADD COLUMN IF NOT EXISTS phase_started_at timestamptz;
ALTER TABLE code_graph_build ALTER COLUMN phase_started_at SET DEFAULT now();
