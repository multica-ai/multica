-- MUL-9284: a private project can also be hidden from workspace admins who are
-- not its members. Workspace owners always keep access, so a project can never
-- be left with nobody able to reach it. Only read while visibility is
-- 'private'; a workspace-visible project keeps the flag so flipping it back to
-- private restores the same access.
--
-- The constant default makes this a catalog-only change.
ALTER TABLE project ADD COLUMN IF NOT EXISTS hide_from_admins BOOLEAN NOT NULL DEFAULT false;
