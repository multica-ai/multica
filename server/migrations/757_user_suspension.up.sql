-- Account suspension (MUL-10296). suspended_at blocks every credential the
-- user holds while it is set, and nothing else: their memberships and data
-- stay, so lifting it restores the account as it was. token_version is the
-- session generation; a session token carrying an older one is refused, so
-- an incident can end every session at once. suspended_by records the
-- operator, not a user id: suspension is run from the maintenance listener.
ALTER TABLE "user"
    ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS suspended_reason TEXT,
    ADD COLUMN IF NOT EXISTS suspended_by TEXT,
    ADD COLUMN IF NOT EXISTS token_version INTEGER NOT NULL DEFAULT 0;

-- One row per suspend, incident response or unsuspend. An incident response
-- records each workspace it has stopped under report.workspaces in the same
-- transaction as that workspace's changes, so a rerun resumes where the last
-- one stopped. No foreign key on user_id.
CREATE TABLE IF NOT EXISTS user_suspension_event (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('suspend', 'incident_response', 'unsuspend')),
    actor TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('running', 'completed')),
    report JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(report) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
