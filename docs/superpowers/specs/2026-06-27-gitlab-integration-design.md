# GitLab Self-Hosted Integration Design

**Date:** 2026-06-27  
**Status:** Approved  
**Scope:** MR tracking on issues, OAuth login via GitLab, HTTPS agent clone with token injection

---

## Context

Multica has a GitHub integration (GitHub Apps, PR mirroring, check suites, issue↔PR linking). The company uses a GitLab self-hosted instance behind a Cloudflare proxy. SSH is not available through the Cloudflare domain; HTTPS is the only reliable path. SSH on port 2222 works only via VPN (direct IP). OAuth Application is the auth mechanism — one GitLab instance, server-wide config.

**In scope:**
- GitLab MR tracking (mirror state, link to issues via closing keywords, auto-advance on merge)
- OAuth login (sign in to Multica with a GitLab account)
- HTTPS clone with auto-injected OAuth token for agents/daemon

**Out of scope for this iteration:**
- Pipeline/CI status (equivalent to GitHub check suites)
- Per-workspace GitLab instance URLs

---

## Approach

Parallel GitLab tables alongside the existing GitHub tables. GitHub code is untouched. The closing-keyword regex is extracted from `github.go` into a shared `gitutil.go` in the same handler package.

---

## Database Schema

One migration file adds three tables.

```sql
CREATE TABLE gitlab_connection (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    namespace        TEXT NOT NULL,
    namespace_type   TEXT NOT NULL CHECK (namespace_type IN ('group', 'user')),
    avatar_url       TEXT,
    access_token     TEXT NOT NULL,        -- encrypted via secretbox
    token_expires_at TIMESTAMPTZ,
    connected_by_id  UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, namespace)
);

CREATE INDEX idx_gitlab_connection_workspace ON gitlab_connection(workspace_id);

CREATE TABLE gitlab_merge_request (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    connection_id     UUID NOT NULL REFERENCES gitlab_connection(id) ON DELETE CASCADE,
    project_path      TEXT NOT NULL,       -- "group/repo"
    mr_iid            INTEGER NOT NULL,    -- GitLab project-scoped IID
    title             TEXT NOT NULL,
    state             TEXT NOT NULL CHECK (state IN ('open', 'closed', 'merged', 'locked')),
    html_url          TEXT NOT NULL,
    source_branch     TEXT,
    author_username   TEXT,
    author_avatar_url TEXT,
    merged_at         TIMESTAMPTZ,
    closed_at         TIMESTAMPTZ,
    mr_created_at     TIMESTAMPTZ NOT NULL,
    mr_updated_at     TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_path, mr_iid)
);

CREATE INDEX idx_gitlab_merge_request_workspace ON gitlab_merge_request(workspace_id);

CREATE TABLE issue_merge_request (
    issue_id         UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    merge_request_id UUID NOT NULL REFERENCES gitlab_merge_request(id) ON DELETE CASCADE,
    close_intent     BOOLEAN NOT NULL DEFAULT false,
    linked_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, merge_request_id)
);

CREATE INDEX idx_issue_merge_request_mr ON issue_merge_request(merge_request_id);
```

---

## Server Configuration

All optional. GitLab features are disabled (routes return 404) when `GITLAB_URL` is unset.

| Env var | Purpose |
|---|---|
| `GITLAB_URL` | Base URL, e.g. `https://gitlab.company.com` |
| `GITLAB_APP_ID` | OAuth app client ID |
| `GITLAB_APP_SECRET` | OAuth app client secret |
| `GITLAB_WEBHOOK_SECRET` | Verified via `X-Gitlab-Token` header on incoming webhooks |

Access tokens stored encrypted via the existing `secretbox` package.

---

## Backend

### New files

| File | Purpose |
|---|---|
| `server/internal/handler/gitlab.go` | All GitLab handler funcs |
| `server/internal/handler/gitutil.go` | Shared `closingIdentifierRe` + extraction helper (moved from `github.go`) |
| `server/pkg/db/queries/gitlab.sql` | sqlc queries for the three new tables |

### Routes

```
POST   /api/webhooks/gitlab                              public, X-Gitlab-Token verified
GET    /api/gitlab/connect                               OAuth entry point (workspace admin)
GET    /api/gitlab/setup                                 OAuth callback
GET    /api/auth/gitlab                                  User login entry point
GET    /api/auth/gitlab/callback                         User login callback
GET    /api/ws/{wsId}/gitlab/connections                 List connections (admin+)
DELETE /api/ws/{wsId}/gitlab/connections/{connectionId}  Disconnect
```

### Webhook handler

1. Verify `X-Gitlab-Token == GITLAB_WEBHOOK_SECRET`; 401 otherwise.
2. Route by `X-Gitlab-Event: Merge Request Hook`.
3. Resolve workspace: match `project_path` namespace against `gitlab_connection.namespace`.
4. Upsert `gitlab_merge_request`.
5. Parse closing keywords from MR description using shared `closingIdentifierRe`.
6. Upsert `issue_merge_request` links with `close_intent`.
7. On `action=merge` + `close_intent=true` + no other open MRs for the issue → auto-advance issue status (same gate as GitHub).
8. Publish realtime invalidation event so frontend reacts.

### Workspace OAuth flow (connect a namespace)

Scope: `api`

```
GET /api/gitlab/connect?ws_id={wsId}
  set state cookie (wsId + nonce) → redirect to {GITLAB_URL}/oauth/authorize

GET /api/gitlab/setup?code=...&state=...
  verify state → exchange code → GET {GITLAB_URL}/api/v4/user
  upsert gitlab_connection → redirect to workspace settings
```

### User login OAuth flow

Scope: `read_user`

```
GET /auth/gitlab
  set state cookie → redirect to {GITLAB_URL}/oauth/authorize

GET /auth/gitlab/callback?code=...&state=...
  exchange code → GET {GITLAB_URL}/api/v4/user
  find-or-create user by email → issue session → redirect to app
```

Two new handler funcs in `auth.go` (~60 lines). Mirrors `GoogleLogin`.

### Closing keyword extractor (gitutil.go)

`closingIdentifierRe` and its extraction helper move from `github.go` to `gitutil.go` in the same package. Both `github.go` and `gitlab.go` use it. No interface, no abstraction — one `var` and one function.

---

## Agent HTTPS Clone

GitLab HTTPS URLs require credentials. The daemon workspace config response gains one optional field:

```go
GitLabAccessToken *string `json:"gitlab_access_token,omitempty"`
```

Server populates it from `gitlab_connection.access_token` (decrypted) for the workspace.

Daemon injects credentials before cloning:

```go
func injectGitLabCreds(rawURL, token string) string {
    u, _ := url.Parse(rawURL)
    u.User = url.UserPassword("oauth2", token)
    return u.String()
}
```

Applied when the clone URL has HTTPS scheme and host matching `GITLAB_URL`. Users paste plain `https://gitlab.company.com/group/repo.git` — the token never appears in the UI.

SSH via VPN (direct IP, port 2222) continues to work unchanged — those URLs don't match `GITLAB_URL` so no rewrite happens.

---

## Frontend

### New files in `packages/core/`

| File | Purpose |
|---|---|
| `types/gitlab.ts` | `GitLabConnection`, `GitLabMergeRequest`, `GitLabMRState` types + zod schemas |
| `gitlab/queries.ts` | `issueMergeRequestsOptions`, `gitlabConnectionsOptions` |
| `gitlab/settings.ts` | Connect/disconnect mutations |
| `gitlab/index.ts` | Re-exports |

### New files in `packages/views/`

| File | Purpose |
|---|---|
| `issues/components/merge-request-list.tsx` | MR cards in issue detail sidebar (~180 lines) |
| `settings/components/gitlab-tab.tsx` | Workspace settings: list connections, connect, disconnect |
| `settings/components/gitlab-mark.tsx` | GitLab fox SVG |

### Existing files modified

| File | Change |
|---|---|
| `packages/core/github/github.go` | Remove `closingIdentifierRe` (moved to `gitutil.go`) |
| `packages/views/issues/components/issue-detail.tsx` | Render `<MergeRequestList>` alongside `<PullRequestList>` |
| `packages/views/settings/components/settings-page.tsx` | Add GitLab tab |
| Login page | Add "Sign in with GitLab" button (shown only when server signals GitLab is configured) |
| `packages/views/locales/en/settings.json` | New GitLab strings |
| `packages/views/locales/en/issues.json` | New MR strings |

Other locale files (`zh-Hans`, `ja`, `ko`) get English fallback strings for now.

### Feature gating

GitLab tab and login button render only when the server advertises GitLab is configured. A `gitlab_enabled: boolean` field is added to the existing server config response (the same endpoint the frontend already fetches for feature flags / auth methods). Avoids showing a connect button that would 404.

---

## Testing

| What | Where |
|---|---|
| Webhook signature verification | `server/internal/handler/gitlab_test.go` |
| Closing-keyword extraction (shared util) | `server/internal/handler/gitutil_test.go` |
| MR upsert + issue link + auto-advance | `server/internal/handler/gitlab_test.go` |
| Malformed webhook payload | `server/internal/handler/gitlab_test.go` |
| Credential injection in daemon | `server/internal/daemon/` unit test |
| MR list component | `packages/views/issues/components/merge-request-list.test.tsx` |
| GitLab settings tab | `packages/views/settings/components/gitlab-tab.test.tsx` |
