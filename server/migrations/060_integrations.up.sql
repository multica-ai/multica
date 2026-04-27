-- Workspace enables an integration (admin/owner action)
CREATE TABLE workspace_integration (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  provider     TEXT NOT NULL,
  instance_url TEXT NOT NULL,
  settings     JSONB NOT NULL DEFAULT '{}',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, provider)
);

-- Per-user API key for an integration enabled in a workspace
CREATE TABLE user_integration_credential (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  user_id      UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  provider     TEXT NOT NULL,
  api_key      TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, user_id, provider)
);

-- Links a Multica project to an external project
CREATE TABLE project_integration_link (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id          UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  project_id            UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  provider              TEXT NOT NULL,
  external_project_id   TEXT NOT NULL,
  external_project_name TEXT,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, project_id, provider)
);

-- Links a Multica issue to an external issue
CREATE TABLE issue_integration_link (
  id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id         UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  issue_id             UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
  provider             TEXT NOT NULL,
  external_issue_id    TEXT NOT NULL,
  external_issue_url   TEXT,
  external_issue_title TEXT,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, issue_id, provider)
);
