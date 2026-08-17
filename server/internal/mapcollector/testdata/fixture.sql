CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE SCHEMA map_fixture;

CREATE TABLE map_fixture.workspace (
  id text PRIMARY KEY,
  visibility text NOT NULL,
  created_at timestamptz NOT NULL,
  display_name text NOT NULL
);
CREATE TABLE map_fixture.member (
  id text PRIMARY KEY,
  workspace_id text NOT NULL,
  role text NOT NULL,
  email text NOT NULL
);
CREATE TABLE map_fixture.agent (
  id text PRIMARY KEY,
  workspace_id text NOT NULL,
  owner_member_id text NOT NULL,
  permission_mode text NOT NULL,
  instructions text NOT NULL,
  secret_config text NOT NULL
);
CREATE TABLE map_fixture.agent_target (
  id text PRIMARY KEY,
  agent_id text NOT NULL,
  target_type text NOT NULL,
  target_id text NOT NULL,
  scope text NOT NULL,
  action text NOT NULL,
  inheritance text NOT NULL
);
CREATE TABLE map_fixture.issue (
  id text PRIMARY KEY,
  workspace_id text NOT NULL,
  parent_id text,
  status text NOT NULL,
  position integer NOT NULL,
  title text NOT NULL,
  body text NOT NULL
);
CREATE TABLE map_fixture.comment (
  id text PRIMARY KEY,
  workspace_id text NOT NULL,
  issue_id text NOT NULL,
  parent_id text,
  body text NOT NULL
);
CREATE TABLE map_fixture.task (
  id text PRIMARY KEY,
  workspace_id text NOT NULL,
  agent_id text NOT NULL,
  status text NOT NULL,
  originator_member_id text,
  accountable_member_id text,
  result text NOT NULL,
  error_text text NOT NULL
);
CREATE TABLE map_fixture.attachment (
  id text PRIMARY KEY,
  workspace_id text NOT NULL,
  issue_id text NOT NULL,
  uploader_member_id text NOT NULL,
  storage_type text NOT NULL,
  storage_key text NOT NULL,
  size_bytes bigint NOT NULL,
  byte_sha256 text NOT NULL,
  filename text NOT NULL,
  source_url text NOT NULL
);
CREATE TABLE map_fixture.usage (
  id text PRIMARY KEY,
  task_id text NOT NULL,
  input_tokens bigint NOT NULL,
  output_tokens bigint NOT NULL,
  cost_microusd bigint NOT NULL,
  provider_payload text NOT NULL
);

INSERT INTO map_fixture.workspace VALUES
  ('workspace-a', 'private', '2026-01-01T00:00:00Z', 'sensitive workspace alpha'),
  ('workspace-b', 'public', '2026-01-02T00:00:00Z', 'sensitive workspace beta');
INSERT INTO map_fixture.member VALUES
  ('member-owner-a', 'workspace-a', 'owner', 'owner-a@example.invalid'),
  ('member-admin-a', 'workspace-a', 'admin', 'admin-a@example.invalid'),
  ('member-owner-b', 'workspace-b', 'owner', 'owner-b@example.invalid'),
  ('member-user-b', 'workspace-b', 'member', 'user-b@example.invalid');
INSERT INTO map_fixture.agent VALUES
  ('agent-private', 'workspace-a', 'member-owner-a', 'private', 'private instructions', 'token-do-not-output'),
  ('agent-public', 'workspace-a', 'member-owner-a', 'public_to', 'public instructions', 'secret-do-not-output');
INSERT INTO map_fixture.agent_target VALUES
  ('target-workspace', 'agent-public', 'workspace', 'workspace-a', 'invocation', 'invoke', 'none'),
  ('target-member', 'agent-public', 'member', 'member-admin-a', 'invocation', 'invoke', 'none');
INSERT INTO map_fixture.issue VALUES
  ('issue-root', 'workspace-a', NULL, 'done', 1, 'sensitive title root', 'sensitive body root'),
  ('issue-child', 'workspace-a', 'issue-root', 'cancelled', 2, 'sensitive title child', 'sensitive body child'),
  ('issue-other', 'workspace-b', NULL, 'backlog', 1, 'sensitive title other', 'sensitive body other'),
  ('issue-todo', 'workspace-a', NULL, 'todo', 3, 'sensitive title todo', 'sensitive body todo'),
  ('issue-progress', 'workspace-a', NULL, 'in_progress', 4, 'sensitive title progress', 'sensitive body progress'),
  ('issue-review', 'workspace-a', NULL, 'in_review', 5, 'sensitive title review', 'sensitive body review'),
  ('issue-blocked', 'workspace-a', NULL, 'blocked', 6, 'sensitive title blocked', 'sensitive body blocked');
INSERT INTO map_fixture.comment VALUES
  ('comment-root', 'workspace-a', 'issue-root', NULL, 'sensitive comment root'),
  ('comment-reply', 'workspace-a', 'issue-root', 'comment-root', 'sensitive comment reply');
INSERT INTO map_fixture.task VALUES
  ('task-done', 'workspace-a', 'agent-private', 'completed', 'member-owner-a', 'member-owner-a', 'sensitive result', ''),
  ('task-failed', 'workspace-a', 'agent-public', 'failed', 'member-admin-a', 'member-admin-a', '', 'sensitive failure'),
  ('task-cancelled', 'workspace-a', 'agent-private', 'cancelled', NULL, NULL, '', '');
INSERT INTO map_fixture.attachment VALUES
  ('attachment-a', 'workspace-a', 'issue-root', 'member-owner-a', 'local', 'objects/object-a.bin', 17, '11a94902119a24aebe4fa998e6a647a27328f23cc256af89909695db24bce211', 'secret-name.txt', 'https://example.invalid/private/object'),
  ('attachment-b', 'workspace-a', 'issue-child', 'member-admin-a', 'object', 'objects/object-a.bin', 17, '11a94902119a24aebe4fa998e6a647a27328f23cc256af89909695db24bce211', 'another-secret.bin', 'https://example.invalid/private/object-b');
INSERT INTO map_fixture.usage VALUES
  ('usage-a', 'task-done', 100, 20, 12345, '{"secret":"provider"}'),
  ('usage-b', 'task-failed', 10, 5, 456, '{"secret":"provider"}');
