# Remove GitLab Login Button

**Date:** 2026-06-29
**Branch:** feat/gitlab-integration

## Problem

A "Sign in with GitLab" button was added to the login page as part of the GitLab integration branch. The OAuth user-login flow is incomplete and untested. The button is not wanted.

## Scope

Remove the GitLab user-login OAuth flow from the login page and backend. This is distinct from the GitLab workspace integration (issue sync, MR sidebar, webhooks, settings tab), which is unaffected.

The two `gitlab_enabled` names are different things:
- `AppConfig.gitlab_enabled` — server config flag gating the login button. **Deleted.**
- `workspace_settings.gitlab_enabled` — per-workspace toggle for the integration. **Untouched.**

## What Changes

### Backend (`server/`)

| File | Change |
|---|---|
| `handler/auth.go` | Delete `GitLabLogin` and `GitLabCallback` handler functions |
| `cmd/server/router.go` | Delete `GET /auth/gitlab` and `GET /auth/gitlab/callback` route registrations |
| `handler/config.go` | Delete `GitLabEnabled` field from `AppConfig` struct and its assignment |
| `handler/gitlab_test.go` | Delete `TestGitLabLogin_NotConfigured` test |

`isGitLabConfigured()` in `gitlab.go` is kept — still used by workspace OAuth.

### Frontend (`packages/`, `apps/`)

| File | Change |
|---|---|
| `packages/core/api/schemas.ts` | Remove `gitlab_enabled?` from AppConfig schema |
| `packages/core/config/index.ts` | Remove `gitLabEnabled` from store state, initializer, and setter |
| `packages/core/platform/auth-initializer.tsx` | Remove `gitLabEnabled: cfg.gitlab_enabled === true` line |
| `packages/views/auth/login-page.tsx` | Remove `onGitLabLogin` prop, button JSX block, `GitLabMark` import |
| `apps/web/app/(auth)/login/page.tsx` | Remove `gitLabEnabled` state selector and `onGitLabLogin` prop |
| `apps/desktop/src/renderer/src/pages/login.tsx` | Remove `handleGitLabLogin` function and `onGitLabLogin` prop |

## What Does Not Change

- `gitlab.go`, `gitlab_test.go` workspace OAuth handlers
- `packages/views/settings/components/gitlab-tab.tsx`
- `packages/views/issues/` MR sidebar components
- `packages/core/gitlab/` query hooks and settings
- DB schema, sqlc queries, daemon clone token injection
- `workspace_settings.gitlab_enabled`

## Verification

```bash
pnpm typecheck
make test
```
