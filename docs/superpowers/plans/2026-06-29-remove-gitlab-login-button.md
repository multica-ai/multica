# Remove GitLab Login Button Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the "Sign in with GitLab" OAuth button and its supporting code from the login page, backend, and config store — without touching the GitLab workspace integration.

**Architecture:** Two independent passes — backend first (Go), then frontend (TS). No new files. Pure deletion.

**Tech Stack:** Go, TypeScript/React, Zustand

## Global Constraints

- Do NOT touch `gitlab.go`, workspace OAuth routes, settings tab, MR sidebar, or `workspace_settings.gitlab_enabled`.
- `isGitLabConfigured()` in `gitlab.go` is kept — still used by workspace OAuth.
- Verification: `pnpm typecheck` + `make test`

---

### Task 1: Remove backend handlers, routes, config field, and login test

**Files:**
- Modify: `server/internal/handler/auth.go:647-782`
- Modify: `server/cmd/server/router.go:604-605`
- Modify: `server/internal/handler/config.go:27-30,68`
- Modify: `server/internal/handler/gitlab_test.go:19-28`

**Interfaces:**
- Produces: nothing (pure deletion — no downstream tasks depend on these)

- [ ] **Step 1: Delete `GitLabLogin` and `GitLabCallback` from `auth.go`**

Delete lines 647–782 (both functions, including the blank line before the comment). The file should go from the closing brace of `Logout` (line 645) directly to `func (h *Handler) UpdateMe` (line 784).

```go
// auth.go — remove this entire block (lines 647–782):

// GitLabLogin (GET /auth/gitlab) begins the user login OAuth flow with scope read_user.
func (h *Handler) GitLabLogin(w http.ResponseWriter, r *http.Request) { ... }

// GitLabCallback (GET /auth/gitlab/callback) exchanges the code, finds or creates
// the user by email, and issues a Multica session cookie.
func (h *Handler) GitLabCallback(w http.ResponseWriter, r *http.Request) { ... }
```

- [ ] **Step 2: Delete the two login routes from `router.go`**

Remove lines 604–605:
```go
// DELETE these two lines:
r.With(authRL).Get("/auth/gitlab", h.GitLabLogin)
r.Get("/auth/gitlab/callback", h.GitLabCallback)
```

The surrounding workspace OAuth and webhook routes (`/api/webhooks/gitlab`, `/api/gitlab/setup`, etc.) are unrelated — leave them.

- [ ] **Step 3: Delete `GitLabEnabled` from `config.go`**

Remove the comment + field (lines 27–30):
```go
// DELETE:
// GitLabEnabled is true when GITLAB_URL + GITLAB_APP_ID + GITLAB_APP_SECRET are set.
// Controls whether the frontend shows the "Sign in with GitLab" button and
// the GitLab settings tab.
GitLabEnabled bool `json:"gitlab_enabled,omitempty"`
```

Remove the assignment (line 68):
```go
// DELETE:
config.GitLabEnabled = isGitLabConfigured()
```

- [ ] **Step 4: Delete `TestGitLabLogin_NotConfigured` from `gitlab_test.go`**

Remove lines 19–28:
```go
// DELETE:
func TestGitLabLogin_NotConfigured(t *testing.T) {
	t.Setenv("GITLAB_URL", "")
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/auth/gitlab", nil)
	w := httptest.NewRecorder()
	h.GitLabLogin(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
```

- [ ] **Step 5: Verify**

```bash
make test
```

Expected: all Go tests pass. If `GitLabLogin` is referenced anywhere else the compiler will tell you — fix those too.

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/auth.go \
        server/cmd/server/router.go \
        server/internal/handler/config.go \
        server/internal/handler/gitlab_test.go
git commit -m "feat(gitlab): remove user login OAuth handlers and config flag"
```

---

### Task 2: Remove GitLab login from frontend (config store, schema, auth-initializer, login pages)

**Files:**
- Modify: `packages/core/api/schemas.ts:47`
- Modify: `packages/core/config/index.ts`
- Modify: `packages/core/platform/auth-initializer.tsx:63`
- Modify: `packages/views/auth/login-page.tsx:26,61,110,498-524`
- Modify: `apps/web/app/(auth)/login/page.tsx:62,213-215`
- Modify: `apps/desktop/src/renderer/src/pages/login.tsx:24-29,41`

**Interfaces:**
- Consumes: nothing from Task 1 (independent frontend deletion)
- Produces: nothing (pure deletion)

- [ ] **Step 1: Remove `gitlab_enabled` from the AppConfig schema**

In `packages/core/api/schemas.ts`, delete line 47:
```ts
// DELETE:
gitlab_enabled?: boolean;
```

- [ ] **Step 2: Remove `gitLabEnabled` from the config store**

In `packages/core/config/index.ts`, apply these four deletions:

Delete line 11–15 (the comment + field in the interface):
```ts
// DELETE:
// True when the server has GitLab OAuth configured and the login button
// should be shown. Defaults to false so older servers without the field
// don't surface a non-functional button.
gitLabEnabled: boolean;
```

Delete line 26 (optional field in `setAuthConfig` param type):
```ts
// DELETE:
gitLabEnabled?: boolean;
```

Delete line 40 (initial state):
```ts
// DELETE:
gitLabEnabled: false,
```

Delete line 48–50 (destructure + set):
```ts
// BEFORE:
setAuthConfig: ({
  allowSignup,
  googleClientId = "",
  gitLabEnabled = false,
  workspaceCreationDisabled = false,
}) => set({ allowSignup, googleClientId, gitLabEnabled, workspaceCreationDisabled }),

// AFTER:
setAuthConfig: ({
  allowSignup,
  googleClientId = "",
  workspaceCreationDisabled = false,
}) => set({ allowSignup, googleClientId, workspaceCreationDisabled }),
```

- [ ] **Step 3: Remove `gitLabEnabled` from `auth-initializer.tsx`**

In `packages/core/platform/auth-initializer.tsx`, delete line 63 and its comment (line 62):
```ts
// DELETE:
// Old servers omit this field — default false keeps the button hidden.
gitLabEnabled: cfg.gitlab_enabled === true,
```

- [ ] **Step 4: Remove the GitLab button from `login-page.tsx`**

In `packages/views/auth/login-page.tsx`:

Delete line 26 (import):
```ts
// DELETE:
import { GitLabMark } from "../settings/components/gitlab-mark";
```

Delete line 61 (prop in `LoginPageProps`):
```ts
// DELETE:
onGitLabLogin?: () => void;
```

Delete line 110 (destructure in component):
```ts
// DELETE:
onGitLabLogin,
```

Delete lines 498–524 (the entire `{onGitLabLogin && ...}` block):
```tsx
// DELETE:
{onGitLabLogin && (
  <>
    {!google && !onGoogleLogin && (
      <div className="relative w-full">
        <div className="absolute inset-0 flex items-center">
          <span className="w-full border-t" />
        </div>
        <div className="relative flex justify-center text-xs uppercase">
          <span className="bg-card px-2 text-muted-foreground">
            {t(($) => $.signin.divider)}
          </span>
        </div>
      </div>
    )}
    <Button
      type="button"
      variant="outline"
      className="w-full"
      size="lg"
      onClick={onGitLabLogin}
      disabled={loading}
    >
      <GitLabMark className="mr-2 h-4 w-4" />
      {t(($) => $.signin.gitlab)}
    </Button>
  </>
)}
```

- [ ] **Step 5: Remove GitLab wiring from the web login page**

In `apps/web/app/(auth)/login/page.tsx`:

Delete line 62:
```ts
// DELETE:
const gitLabEnabled = useConfigStore((state) => state.gitLabEnabled);
```

Delete lines 213–215 (the `onGitLabLogin` prop):
```tsx
// DELETE:
onGitLabLogin={
  gitLabEnabled ? () => { window.location.href = "/auth/gitlab"; } : undefined
}
```

- [ ] **Step 6: Remove GitLab wiring from the desktop login page**

In `apps/desktop/src/renderer/src/pages/login.tsx`:

Delete lines 24–29 (`handleGitLabLogin` function):
```ts
// DELETE:
const handleGitLabLogin = () => {
  // Open the GitLab OAuth entry point in the default browser.
  // The server callback sets an auth cookie and redirects to the web app;
  // the user can then open the desktop app separately (same pattern as Google).
  window.desktopAPI.openExternal(`${webUrl}/auth/gitlab`);
};
```

Delete line 41 (prop):
```tsx
// DELETE:
onGitLabLogin={handleGitLabLogin}
```

- [ ] **Step 7: Verify**

```bash
pnpm typecheck
```

Expected: zero errors. If `signin.gitlab` translation key is now unused and flagged, delete it from the locale files too (search for `"gitlab"` in `packages/views/locales/`).

- [ ] **Step 8: Commit**

```bash
git add packages/core/api/schemas.ts \
        packages/core/config/index.ts \
        packages/core/platform/auth-initializer.tsx \
        packages/views/auth/login-page.tsx \
        apps/web/app/\(auth\)/login/page.tsx \
        apps/desktop/src/renderer/src/pages/login.tsx
git commit -m "feat(gitlab): remove login button from frontend"
```
