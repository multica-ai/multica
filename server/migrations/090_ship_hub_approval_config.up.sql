-- Phase 7d follow-up — configurable approval requirements per risk tier.
-- Today's hardcoded rules don't fit small teams (2-3 people who can't
-- get distinct approvers for critical-risk hotfixes). The workspace
-- owner now sets the rule per tier.
--
-- Values:
--   "member"   — any workspace member can verify/promote
--   "admin"    — workspace owner or admin role
--   "approver" — release.approver_id (or workspace admin) must verify
--   "two"      — release.approver_id AND release.second_approver_id
--                must both verify (admins still satisfy each slot)
--
-- Defaults preserve the current hardcoded behavior so existing
-- workspaces don't experience a silent behavior change.
ALTER TABLE workspace ADD COLUMN ship_hub_approval_low      TEXT NOT NULL DEFAULT 'member';
ALTER TABLE workspace ADD COLUMN ship_hub_approval_medium   TEXT NOT NULL DEFAULT 'member';
ALTER TABLE workspace ADD COLUMN ship_hub_approval_high     TEXT NOT NULL DEFAULT 'approver';
ALTER TABLE workspace ADD COLUMN ship_hub_approval_critical TEXT NOT NULL DEFAULT 'two';

ALTER TABLE workspace ADD CONSTRAINT ship_hub_approval_low_check
  CHECK (ship_hub_approval_low IN ('member','admin','approver','two'));
ALTER TABLE workspace ADD CONSTRAINT ship_hub_approval_medium_check
  CHECK (ship_hub_approval_medium IN ('member','admin','approver','two'));
ALTER TABLE workspace ADD CONSTRAINT ship_hub_approval_high_check
  CHECK (ship_hub_approval_high IN ('member','admin','approver','two'));
ALTER TABLE workspace ADD CONSTRAINT ship_hub_approval_critical_check
  CHECK (ship_hub_approval_critical IN ('member','admin','approver','two'));

-- Optional: allow PR authors to verify their own releases. When true,
-- a verifier within the release's PR-author set can sign off; when
-- false, separation-of-duties is enforced. Defaults to true (small
-- teams typically self-verify; large teams flip this off).
ALTER TABLE workspace ADD COLUMN ship_hub_approver_can_be_author BOOLEAN NOT NULL DEFAULT TRUE;

-- Two-approver signoff tracking. For workspaces using the "two" rule,
-- each release tracks first-approver + second-approver signoffs as
-- separate rows. Both must exist before stage transitions to verifying.
CREATE TABLE ship_release_signoff (
  release_id    UUID NOT NULL REFERENCES ship_release(id) ON DELETE CASCADE,
  approver_slot TEXT NOT NULL CHECK (approver_slot IN ('first','second')),
  signed_by     UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  signed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  note          TEXT,
  PRIMARY KEY (release_id, approver_slot)
);
CREATE INDEX idx_ship_release_signoff_release ON ship_release_signoff(release_id);
