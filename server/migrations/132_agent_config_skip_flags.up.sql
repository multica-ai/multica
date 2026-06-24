-- Agent config template skip flags.
--
-- These two columns were originally folded into migration 125 by a later
-- commit, but 125 had already been applied to existing databases in its
-- earlier (two-column) form. golang-migrate never re-runs an applied
-- version, so those databases never received the skip columns while the
-- generated scan code already expected them — ListAgents returned
-- `column "skip_system_template" does not exist` and the agents page
-- failed to load. This standalone migration closes that gap.
--
-- ADD COLUMN IF NOT EXISTS keeps it safe for databases that already have
-- the columns (e.g. freshly seeded from the amended 125) as well as for
-- those that do not.

ALTER TABLE agent ADD COLUMN IF NOT EXISTS skip_system_template BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE agent ADD COLUMN IF NOT EXISTS skip_personal_template BOOLEAN NOT NULL DEFAULT false;
