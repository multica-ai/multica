-- Per-property display and create rules. always_visible keeps the
-- property's row on every issue even while empty; required makes creating an
-- issue without a value fail. required applies to select properties only.
ALTER TABLE issue_property
    ADD COLUMN IF NOT EXISTS required BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS always_visible BOOLEAN NOT NULL DEFAULT false;
