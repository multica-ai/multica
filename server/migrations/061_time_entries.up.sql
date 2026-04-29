CREATE TABLE time_entry (
  id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id           UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  issue_id               UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
  user_id                UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  duration_minutes       INT NOT NULL CHECK (duration_minutes > 0),
  activity_name          TEXT,
  redmine_activity_id    INT,
  comment                TEXT NOT NULL DEFAULT '',
  spent_on               DATE NOT NULL DEFAULT CURRENT_DATE,
  external_time_entry_id TEXT,
  sync_status            TEXT NOT NULL DEFAULT 'pending'
                         CHECK (sync_status IN ('pending','synced','failed','not_linked')),
  timer_started_at       TIMESTAMPTZ,
  timer_stopped_at       TIMESTAMPTZ,
  created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_time_entry_workspace_issue ON time_entry (workspace_id, issue_id);
CREATE INDEX idx_time_entry_workspace_user_date ON time_entry (workspace_id, user_id, spent_on);
