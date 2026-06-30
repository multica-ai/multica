-- Explicit write grants for autopilots: lets an autopilot's creator (or a
-- workspace owner/admin) authorize specific workspace members to manage the
-- autopilot, beyond the implicit "creator ∪ owner/admin" set. A member listed
-- here is treated as a writer — they can edit, delete, trigger, replay
-- deliveries, and manage triggers/webhook secrets (MUL-3807).
--
-- No foreign keys or cascades — autopilot existence and workspace membership
-- are enforced in the application layer (the autopilot delete handler removes
-- these rows in the same transaction; membership is checked at the API
-- boundary), per the repo rule. Mirrors autopilot_subscriber.
CREATE TABLE autopilot_collaborator (
    autopilot_id UUID NOT NULL,
    user_type    TEXT NOT NULL CHECK (user_type IN ('member')),
    user_id      UUID NOT NULL,
    granted_by   UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (autopilot_id, user_type, user_id)
);

-- Reverse-lookup index for "which autopilots can this member write?" — the PK
-- can't answer that since autopilot_id is its leading column.
CREATE INDEX idx_autopilot_collaborator_user
    ON autopilot_collaborator (user_type, user_id);
