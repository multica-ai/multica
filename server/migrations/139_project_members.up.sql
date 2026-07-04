CREATE TABLE project_member (
  project_id  UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  member_id   UUID NOT NULL REFERENCES member(id) ON DELETE CASCADE,
  role        TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin', 'editor', 'viewer')),
  invited_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  invited_by  UUID REFERENCES member(id),
  PRIMARY KEY (project_id, member_id)
);
