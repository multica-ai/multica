-- Move history is retained independently of ordinary issue/workspace deletion.
-- Resolver queries join live owners; orphan history cannot resolve a location.
CREATE TABLE IF NOT EXISTS issue_key_alias (
 source_workspace_id uuid NOT NULL,
 prefix text NOT NULL,
 number integer NOT NULL,
 issue_id uuid NOT NULL,
 moved_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS project_move (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 project_id uuid NOT NULL,
 source_workspace_id uuid NOT NULL,
 target_workspace_id uuid NOT NULL,
 moved_by uuid NOT NULL,
 moved_at timestamptz NOT NULL DEFAULT now(),
 summary jsonb NOT NULL
);
