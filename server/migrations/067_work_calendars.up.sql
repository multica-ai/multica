-- Work calendar: stores the parsed schedule for a given year.
-- Each calendar belongs to a workspace and holds its days as a JSONB array,
-- decoupled from whatever PDF template was used to import it.
CREATE TABLE work_calendar (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    year          INT  NOT NULL,
    -- Each element: {"date":"2026-01-01","type":"holiday"|"reduced"|"normal"|"weekend","hours":number,"label":"optional note"}
    days          JSONB NOT NULL DEFAULT '[]',
    -- Monthly summary: [{"month":1,"total_hours":number}, ...]
    monthly_hours JSONB NOT NULL DEFAULT '[]',
    -- Metadata about the import source
    source        TEXT NOT NULL DEFAULT 'manual',  -- 'manual' | 'pdf_import'
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(workspace_id, name, year)
);

CREATE INDEX idx_work_calendar_workspace ON work_calendar (workspace_id);
CREATE INDEX idx_work_calendar_workspace_year ON work_calendar (workspace_id, year);
