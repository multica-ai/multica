-- Template list of workspace members auto-subscribed to every issue spawned
-- by the autopilot. Members-only for now (broaden the CHECK to expand).
-- No FK on user_id — workspace membership is enforced at the API boundary
-- (isWorkspaceEntity in the handler), matching issue_subscriber.
CREATE TABLE autopilot_subscriber (
    autopilot_id UUID NOT NULL REFERENCES autopilot(id) ON DELETE CASCADE,
    user_type    TEXT NOT NULL CHECK (user_type IN ('member')),
    user_id      UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (autopilot_id, user_type, user_id)
);

-- Reverse-lookup index for "which autopilots auto-subscribe this member?";
-- the PK can't answer that since autopilot_id is its leading column.
CREATE INDEX idx_autopilot_subscriber_user
    ON autopilot_subscriber (user_type, user_id);

ALTER TABLE issue_subscriber DROP CONSTRAINT issue_subscriber_reason_check;
ALTER TABLE issue_subscriber ADD CONSTRAINT issue_subscriber_reason_check
    CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual', 'autopilot'));
