# GitLab Issue Sync Design

**Date:** 2026-06-28  
**Branch:** gitlab-self-hosted  
**Status:** Approved

## Overview

Extend the existing GitLab webhook integration to sync issues and comments between GitLab and Multica. Issue creation is one-way (GitLab → Multica), triggered by an "agent" label. Comments are two-way. Description, assignee, open/close state are kept in sync.

## Scope

**In:**
- GitLab issue labeled "agent" → create Multica issue
- GitLab issue label "agent" removed → delete Multica issue
- GitLab issue closed → mark Multica issue Done
- GitLab issue reopened → mark Multica issue In Progress (or Todo if never started)
- GitLab issue description updated → update Multica issue description
- GitLab issue assignee changed → update stored `gl_assignee_username`
- GitLab comment → create Multica comment (new comments only, no historical backfill)
- Multica comment → post to GitLab via API
- GitLab issue number shown as badge on Multica issue (`paral/repo#42`)
- GitLab assignee shown as read-only field on Multica issue

**Out:**
- Multica → GitLab issue creation
- Historical comment import on initial label sync
- Automatic Multica member assignment from GitLab assignee
- Identifier format change (`PAR-42` stays as-is)

## Data Model

### New table: `gitlab_issue`

```sql
CREATE TABLE gitlab_issue (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    connection_id    UUID NOT NULL REFERENCES gitlab_connection(id) ON DELETE CASCADE,
    project_path     TEXT NOT NULL,
    gl_issue_iid     INTEGER NOT NULL,
    gl_project_id    BIGINT NOT NULL,
    issue_id         UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    gl_assignee_username TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_path, gl_issue_iid)
);

CREATE INDEX idx_gitlab_issue_workspace ON gitlab_issue(workspace_id);
CREATE INDEX idx_gitlab_issue_issue ON gitlab_issue(issue_id);
```

### Modified table: `comment`

```sql
ALTER TABLE comment ADD COLUMN gitlab_note_id BIGINT;
CREATE UNIQUE INDEX idx_comment_gitlab_note ON comment(gitlab_note_id) WHERE gitlab_note_id IS NOT NULL;
```

Used for echo loop prevention: when a Note Hook fires, if `gitlab_note_id` already exists in `comment` we skip it. When Multica posts a comment to GitLab, we store the returned note ID on the comment row.

## Webhook Events

Same endpoint (`POST /api/webhooks/gitlab`), same secret. User must add **Issues events** and **Comments** triggers to the existing GitLab webhook (currently only Merge request events is checked).

### `Issue Hook` → `handleGitLabIssueEvent`

Resolve workspace via `project.namespace` (existing `resolveGitLabConnectionByNamespace`).

| Condition | Action |
|---|---|
| action=`open` or `update`, labels contain "agent", no `gitlab_issue` row | Create Multica issue (title + description), insert `gitlab_issue` row |
| action=`open` or `update`, labels contain "agent", row exists | Sync description + assignee |
| label "agent" removed (labels no longer contain "agent"), row exists | Delete Multica issue + `gitlab_issue` row |
| action=`close`, row exists | Mark Multica issue status = `done` |
| action=`reopen`, row exists | Mark Multica issue status = `in_progress` |

Label detection: GitLab sends the full current label list on every issue event. Check `labels[].title` for `"agent"` rather than relying on action type.

### `Note Hook` → `handleGitLabNoteEvent`

Only handle when `object_attributes.notable_type == "Issue"`. Skip if `object_attributes.system == true` (GitLab system notes).

**GitLab → Multica:**
1. Find `gitlab_issue` by `project_path` + `gl_issue_iid`
2. If not found, skip (issue not synced)
3. Check `comment` table for existing `gitlab_note_id` — skip if found (echo prevention)
4. Create comment on the Multica issue, store `gitlab_note_id`
5. Author display: prefix body with `**{gl_username}** (GitLab):\n` so attribution is visible

**Multica → GitLab:**
Hook into the existing comment creation handler. After a comment is saved on a GitLab-linked issue:
1. Look up `gitlab_issue` by `issue_id`
2. If not found, skip (not a synced issue)
3. POST to `/api/v4/projects/{gl_project_id}/issues/{gl_issue_iid}/notes`
4. Use the connection's stored access token (decrypt from `gitlab_connection.access_token`)
5. Store returned `note.id` as `gitlab_note_id` on the comment row

## Echo Loop Prevention

Both directions populate `gitlab_note_id` on the `comment` row:
- GitLab → Multica: set on create
- Multica → GitLab: set after API response

When Note Hook fires for a note we posted to GitLab, the ID already exists → skip. The unique index enforces this at DB level as a safety net.

## API

### `GET /api/issues/:id/gitlab-issue`

Returns linked GitLab issue info for display in the sidebar.

```json
{
  "gl_issue_iid": 42,
  "project_path": "paral/repo",
  "url": "https://git.paral.no/paral/repo/-/issues/42",
  "gl_assignee_username": "volumet"
}
```

Returns `404` if no `gitlab_issue` row exists for this issue.

## Frontend

### Issue detail sidebar

Two additions (only shown when `gitlab-issue` data exists):

**GitLab issue badge** — clickable link rendered as `paral/repo#42`, opens GitLab issue in new tab. Placed alongside the existing MR cards section.

**GitLab assignee** — read-only row showing `"GitLab: volumet"` below the Multica assignee field. No mapping to Multica members.

## Access Token Usage

The GitLab connection stores an encrypted access token (`gitlab_connection.access_token`, AES-sealed). The Multica → GitLab comment posting decrypts it using the existing `GitLabBox`. Token expiry is stored in `token_expires_at` — if expired, log a warning and skip the post rather than failing hard.

## Error Handling

- Webhook handler always returns `204` regardless of processing errors (same pattern as MR handler). Errors are logged via `slog`.
- GitLab API call failures (comment post) are logged and skipped; the Multica comment is already saved so it is not rolled back.
- Duplicate issue creation is prevented by the `UNIQUE` constraint on `gitlab_issue`; use INSERT ON CONFLICT DO NOTHING for idempotency.

## Testing

- Unit tests for `handleGitLabIssueEvent`: label-add creates issue, label-remove deletes, close marks Done, reopen marks In Progress.
- Unit tests for `handleGitLabNoteEvent`: new note creates comment, duplicate `gitlab_note_id` is skipped.
- Unit test for echo loop: posting a Multica comment sets `gitlab_note_id`; subsequent Note Hook with same ID is a no-op.
- Frontend: `merge-request-list` test pattern extended for GitLab issue badge rendering.

## GitLab Webhook Setup (User Action Required)

In GitLab → Settings → Webhooks → edit existing webhook, enable:
- Issues events ✓
- Comments ✓

(Merge request events was already enabled.)
