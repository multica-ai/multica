---
title: "Member Display Name Management"
date: 2026-06-15
category: architecture-pattern
module: workspace/auth/admin
problem_type: architecture_pattern
component: authentication
severity: high
applies_when:
  - "Seeding user attributes from a pre-registration record (invitation, waitlist, referral) that must survive an OAuth login"
  - "Implementing super-admin operations that modify other users' data"
  - "Adding cross-layer features touching DB migration, Go API, React frontend, CLI, and Electron simultaneously"
  - "Enforcing i18n parity across multiple locales in a CI-gated monorepo"
  - "Generating Go DB code when sqlc or other code-gen tools are unavailable in the environment"
tags:
  - go
  - postgres
  - invitation
  - super-admin
  - i18n
  - sqlc
  - oauth
  - fullstack
related_components:
  - database
  - development_workflow
  - tooling
---

# Member Display Name Management

## Context

Multica needed two related capabilities: (1) workspace admins can pre-assign a display name to an invited member before they register, and (2) super-admins can rename any user after the fact. The trigger was that Google OAuth users get names pulled from their Google profile — which may be an email alias, nickname, or full legal name the team doesn't use. Without this feature, the invitation workflow had no way to express "we know this person as Alice" before they log in for the first time.

The implementation spanned 9 units across all layers: DB migration, backend API (Go), invitation API, registration name resolution, admin API, frontend invite form, admin UI page, CLI commands, and desktop routing.

## Guidance

### 1. Seeding display name at invite time

Extend the invitation record with an optional `invitee_name` column. Store empty string as NULL using `NULLIF($6, '')`. On new user creation, look up the latest non-expired pending invitation for that email and apply its name.

Critical invariant: introduce a `hadInviteName bool` return value from `findOrCreateUser` to signal whether a name came from an invitation. This allows the OAuth login handler to skip overriding the pre-set name with the provider's profile name.

```go
// findOrCreateUser returns (user, isNew, hadInviteName, err)
func (h *Handler) findOrCreateUser(ctx context.Context, email, providerID string) (db.User, bool, bool, error) {
    // ... on new user path:
    inv, err := h.DB.GetLatestPendingInvitationNameByEmail(ctx, email)
    if err == nil && inv.InviteeName.Valid {
        // set the name from the invitation
        hadInviteName = true
    }
    return user, true, hadInviteName, nil
}

// In GoogleLogin — only override name from OAuth if no invitation name was set:
user, isNew, hadInviteName, err := h.findOrCreateUser(ctx, email, googleID)
if isNew && !hadInviteName {
    user.Name = googleProfile.Name
}
```

The invitation lookup query must guard on `expires_at > now()` and use `ORDER BY created_at DESC, id DESC LIMIT 1` to pick the freshest valid invitation.

```sql
-- GetLatestPendingInvitationNameByEmail
SELECT invitee_name FROM workspace_invitation
WHERE invitee_email = $1
  AND expires_at > now()
ORDER BY created_at DESC, id DESC
LIMIT 1;
```

### 2. Super-admin gating with deny-all default

Identify super-admins by email, configured via `SUPER_ADMIN_EMAILS` env var (comma-separated). **Empty list must deny all** — this is a security invariant, not a convenience default.

```go
func (h *Handler) isSuperAdmin(email string) bool {
    if len(h.Config.SuperAdminEmails) == 0 {
        return false // deny-all when unconfigured — NOT allow-all
    }
    for _, e := range h.Config.SuperAdminEmails {
        if strings.EqualFold(e, email) {
            return true
        }
    }
    return false
}
```

Expose `is_super_admin bool` **only in authenticated endpoints** (`/api/me`). Never include it in unauthenticated endpoints like `/api/config` — that would leak the admin list to anonymous visitors.

Cover the deny-all invariant with an explicit unit test:

```go
func TestSuperAdmin_EmptyList_DenyAll(t *testing.T) {
    h := &Handler{Config: Config{SuperAdminEmails: []string{}}}
    assert.False(t, h.isSuperAdmin("anyone@example.com"))
    assert.False(t, h.isSuperAdmin(""))
}
```

### 3. Admin rename API with rate limiting and audit logging

`PATCH /api/admin/users/{id}` accepts `{"name": "New Name"}`. Key practices:

- Validate non-empty name (a rename to blank would be worse than the current name)
- Use `slog.Info` for an audit line with actor email, target user ID, and new name
- Apply a **separate rate limiter** controlled by `RATE_LIMIT_ADMIN` env var (default 60/min), independent of the main rate limiter
- For user-supplied UUID path parameters, use a dedicated parser that returns 400 Bad Request — not a generic panic

```go
func parseUUIDOrBadRequest(w http.ResponseWriter, s string) (uuid.UUID, bool) {
    id, err := uuid.Parse(s)
    if err != nil {
        http.Error(w, "invalid user id", http.StatusBadRequest)
        return uuid.UUID{}, false
    }
    return id, true
}
```

### 4. i18n parity enforcement

When adding new i18n keys, add them to **all locale files simultaneously**. If the project has a parity test (`parity.test.ts` checking that all locales have identical key sets), run it before committing.

Common failure mode: editing only one file, or accidentally deleting existing keys when copy-pasting a locale block to add new keys. If `old_string` in an edit tool includes sibling keys, those sibling keys disappear. Always verify the surrounding context after each locale edit.

### 5. Hand-writing sqlc output when code-gen is unavailable

If `sqlc` is not installed, study existing generated files in `db/generated/` to understand the exact patterns:

- Struct field order matches the `RETURNING` clause column order exactly
- `pgtype.Text` for nullable `TEXT` columns with pgx/v5 driver
- Scan call arguments match the field order in the struct

Write the query function and scan call by hand, matching that order exactly. Update **all** existing query functions when adding a new column to a `RETURNING` clause.

### 6. Frontend inline-rename state machine

For admin UI rename interactions, use a simple state machine rather than a boolean flag:

```
idle → editing → saving → (success | error) → idle
```

- Esc cancels from `editing` back to `idle` (discard input)
- Enter triggers `saving`
- On error: show message and return to `idle` with value rolled back
- This prevents double-submits and gives clear UX at each stage

### 7. Desktop routing for admin-only pages

Desktop uses `createMemoryRouter` (react-router), independent from Next.js routing. Add admin pages as **top-level sibling routes** (not workspace-scoped):

```tsx
function DesktopAdminRoute() {
  const { user } = useAuthStore();
  if (!user?.is_super_admin) return <Navigate to="/" replace />;
  return <Outlet />;
}

// In appRoutes — sibling of :workspaceSlug, not nested inside it:
{ path: '/admin', element: <DesktopAdminRoute />, children: [
    { index: true, element: <UserManagementPage /> }
]}
```

## Why This Matters

- **Name collision at registration**: Without seeding from invitation, OAuth users get whatever name Google returns. Teams often invite colleagues by their work name, not their Google profile name. The `hadInviteName` guard is the only thing preventing silent overwrites.
- **Security by default**: `SUPER_ADMIN_EMAILS` empty → deny all is the only safe default. An "allow all when unconfigured" fallback would be a privilege escalation on first deploy.
- **Audit trail**: Admin renames affect other users' identities. Logging at the handler level (not just DB triggers) gives an actionable audit line with actor, target, and new value — visible in server logs without DB access.
- **i18n CI gate**: Failing to maintain locale parity causes silent runtime errors in production for non-English users. A `parity.test.ts` CI check turns this into a compile-time failure.

## When to Apply

- Any feature that seeds user attributes from a pre-registration record and must survive an OAuth login that would otherwise overwrite those attributes
- Any admin-only API route that modifies other users' data: use dedicated rate limiter, `parseUUIDOrBadRequest`, and `slog` audit lines
- Any multi-locale frontend feature: enforce i18n parity with a CI test, not by convention
- When code generation tools (sqlc, protoc, etc.) are unavailable: hand-write by reading existing generated files as the authoritative pattern source
- Full-stack features spanning DB + Go API + React + CLI + Electron: implement in dependency order (schema → backend → frontend → CLI → desktop), each unit independently testable

## Examples

**Invite with a display name via CLI:**
```sh
multica workspace member invite \
  --email alice@example.com \
  --role member \
  --name "Alice Chen" \
  --output json
```

**Rename a user via super-admin CLI:**
```sh
multica admin update-user 550e8400-e29b-41d4-a716-446655440000 \
  --name "Alice Chen (Eng)" \
  --output json
```

**DB migration — store NULL for empty, retrieve with expiry guard:**
```sql
-- Migration 119 up
ALTER TABLE workspace_invitation ADD COLUMN invitee_name TEXT;

-- INSERT with NULLIF (empty string → NULL)
INSERT INTO workspace_invitation (..., invitee_name)
VALUES (..., NULLIF($6, ''));

-- Lookup with expiry guard
SELECT invitee_name FROM workspace_invitation
WHERE invitee_email = $1
  AND expires_at > now()
ORDER BY created_at DESC, id DESC
LIMIT 1;
```

**Super-admin conditional sidebar entry:**
```tsx
{user?.is_super_admin && (
  <AppLink href="/admin">
    <Shield size={16} />
    {t('layout:sidebar.admin_user_management')}
  </AppLink>
)}
```

**i18n parity — all 4 locales must have identical keys (`parity.test.ts` CI check):**
```json
// packages/views/locales/en/admin.json (and zh-Hans, ja, ko with same keys)
{
  "page_title": "User Management",
  "search_placeholder": "Search by name or email",
  "table_header_name": "Name",
  "table_header_email": "Email",
  "table_header_actions": "Actions",
  "empty": "No users found.",
  "rename_button": "Rename",
  "rename_save": "Save",
  "rename_cancel": "Cancel",
  "rename_placeholder": "New display name",
  "rename_empty_error": "Display name cannot be empty",
  "rename_success": "Display name updated",
  "rename_error": "Failed to update display name"
}
```

## Related

- GitHub issues searched: none directly related (closest: #3430 "Admin Account transfer", #2559 "Kanban permission customization")
- No existing `docs/solutions/` entries at time of writing (this is the first)
