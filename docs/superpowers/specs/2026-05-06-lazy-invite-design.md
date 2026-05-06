# Lazy Invite — Design

## Goal

Auto-grant workspace membership to users whose email domain matches an operator-configured allowlist, without going through the existing email-invitation flow. Removes the manual "invite, click link, accept" sequence for trusted internal/partner domains.

## Decisions

| Decision | Choice | Why |
|---|---|---|
| Configuration source | Env var `LAZY_INVITE_RULES` | Matches existing `ALLOW_SIGNUP` / `ALLOWED_EMAIL_DOMAINS` pattern; ops-controlled; no UI surface needed |
| Multi-workspace support | Yes — multiple `domain:slug` pairs | One env var line covers "spendbase.com → spendbase, partner-way.com → spendbase, acme.com → acme-team" |
| Role granted | `member` always | Admin/owner privileges still require explicit grant by an existing admin |
| When it fires | On signup, AND on login if user has zero memberships | Catches new users and users who pre-date the rule. Does NOT re-add users an admin explicitly removed |
| Auth method coverage | Magic-link AND Google | Magic-link verifies email via code; Google verifies via ID token; both are equivalent trust signals |
| Domain matching | Exact only | `alice@spendbase.com` matches `spendbase.com`. `alice@dev.spendbase.com` does NOT |
| Email case | Case-insensitive comparison; rules normalized to lowercase at parse time | RFC 5321 says local-part is case-sensitive in theory but in practice nobody honors it; domain part is unconditionally case-insensitive |
| Override `ALLOW_SIGNUP=false` | Yes | The lazy-invite rule is itself an allowlist; double-gating is redundant |
| Failure mode of insert | Best-effort, log warn, don't block login | Next sign-in retries via the zero-memberships path |
| Failure mode of parse | Fatal at server startup | Operator must fix the env var before users hit it |

## Format

```
LAZY_INVITE_RULES=spendbase.com:spendbase,partner-way.com:spendbase,acme.com:acme-team
```

Empty / unset → feature off, no behavior change.

Constraints validated at parse time:
- Each entry must be `<domain>:<slug>`, non-empty on both sides
- Domain must contain a `.` (rough sanity check)
- No duplicate domains across rules — would imply an ambiguous mapping
- Each `<slug>` must resolve to an existing `workspace.slug` (`Queries.GetWorkspaceBySlug`)

Multiple domains may map to the same workspace. Multiple workspaces may each have their own domains.

## Architecture

```
                 ┌─────────────────────────────┐
                 │  startup (cmd/server)       │
                 │  ParseLazyInviteRules(env)  │
                 │   ↓                         │
                 │  validate + resolve slugs   │
                 │   ↓                         │
                 │  Handler.LazyInvite ←───────┘
                 └─────────────┬───────────────┘
                               │
              ┌────────────────┴────────────────┐
              ↓                                 ↓
   ┌─────────────────────┐         ┌──────────────────────────┐
   │ checkSignupAllowed  │         │ EnsureLazyInviteMembership│
   │  (override gate)    │         │  (after find-or-create)   │
   └─────────────────────┘         └──────────────────────────┘
              │                                 │
              └─────────────────┬───────────────┘
                                ↓
                       VerifyCode + GoogleLogin
```

Two integration points; everything else is one parser file and one helper file.

## Components

### `server/internal/auth/lazy_invite.go`

Pure parser + matcher, no DB writes. Held on `Handler` as a value.

```go
type LazyInviteRule struct {
    Domain        string       // lowercase, no leading "@"
    WorkspaceID   pgtype.UUID
    WorkspaceSlug string       // kept for log readability
}

type LazyInviteRules []LazyInviteRule

// IsAllowedDomain answers the signup-gate question: should signup be
// permitted on the basis of a lazy-invite rule, regardless of global
// signup config?
func (r LazyInviteRules) IsAllowedDomain(email string) bool

// Match returns the first rule whose Domain matches the email's domain
// (case-insensitive, exact). The "first" tiebreak only matters if a
// future change relaxes the no-duplicate-domain rule.
func (r LazyInviteRules) Match(email string) (LazyInviteRule, bool)

// ParseLazyInviteRules reads LAZY_INVITE_RULES, validates structure,
// and resolves each slug to a workspace UUID via the queries handle.
// Returns an empty value when spec is empty (feature off). Returns an
// error with all problems aggregated on bad input.
func ParseLazyInviteRules(ctx context.Context, spec string, queries *db.Queries) (LazyInviteRules, error)
```

### `server/internal/handler/lazy_invite.go`

The DB-touching glue.

```go
// EnsureLazyInviteMembership idempotently adds the user as a member of
// the workspace identified by their email domain in LAZY_INVITE_RULES.
//
// It runs in two situations:
//  1. The user is brand-new (just created in findOrCreateUser*).
//  2. The user is existing but has zero memberships (probably created
//     before the rule was added; we treat them like a new user).
//
// In all other cases it returns nil without touching the DB. Rationale:
// if a user has a membership somewhere, they've made it past the
// invite/auth flow once already, and adding them to the lazy-invite
// workspace would override an admin's decision to (a) place them in a
// specific workspace or (b) remove them from this one.
//
// Failure to insert is logged but not returned to the caller — login
// must not depend on best-effort enrichment.
func (h *Handler) EnsureLazyInviteMembership(ctx context.Context, user db.User, isNew bool)
```

### sqlc query additions (`server/pkg/db/queries/`)

- `GetWorkspaceBySlug` — likely exists; reuse
- `CreateMember` — likely exists for invite-accept flow; reuse
- `CountMembershipsByUser` — *new*: `SELECT COUNT(*) FROM member WHERE user_id = $1`. Cheap (`user_id` is FK-indexed).

### Modifications

| File | Change |
|---|---|
| `handler.go` | Add `LazyInvite auth.LazyInviteRules` field on `Handler` |
| `auth.go` — `checkSignupAllowed` | Add `if h.LazyInvite.IsAllowedDomain(email) { return nil }` as the first allow-clause |
| `auth.go` — `findOrCreateUser` | Call `h.EnsureLazyInviteMembership(ctx, user, isNew)` before returning |
| `auth.go` — `findOrCreateUserByGoogle` | Same — call before returning |
| `cmd/server/router.go` | Parse `LAZY_INVITE_RULES` at startup; fatal on error; attach to `Handler` |
| `.env.example` | Document the new env var with example |

## Data flow

### Startup

1. Server reads `LAZY_INVITE_RULES`. Empty → empty `LazyInviteRules`, feature off.
2. For each rule:
   - Validate `<domain>:<slug>` structure
   - Lowercase the domain, trim whitespace
   - Reject empty halves; reject domains with no `.`
3. Reject any duplicate domain across rules (collected in pre-pass).
4. For each rule, call `Queries.GetWorkspaceBySlug(slug)`. Aggregate any not-found into a single error.
5. Any failure → `log.Fatal` and exit. Operator fixes env, restarts.

### Sign-in (magic link or Google)

1. **Signup gate**: `checkSignupAllowed(email, isNew=true)` runs as today, but the very first clause now reads:
   ```go
   if h.LazyInvite.IsAllowedDomain(email) {
       return nil
   }
   ```
   This sits *before* the existing email allowlist / domain allowlist / `AllowSignup` checks. Existing users (`isNew=false`) take the unchanged early-return path.

2. **find-or-create** runs as today. Returns `(user, isNew, err)`.

3. **EnsureLazyInviteMembership(user, isNew)**:
   - `rule, ok := h.LazyInvite.Match(user.Email)`. If `!ok` → return.
   - Decide whether to act:
     - `isNew == true` → act
     - else: `count, _ := Queries.CountMembershipsByUser(user.ID)` — if `count == 0`, act
     - else → return
   - `Queries.CreateMember(workspace_id=rule.WorkspaceID, user_id=user.ID, role="member")`
   - On unique-violation (race or concurrent sign-ins) → swallow, log debug
   - On other error → log warn, return (don't propagate to caller)

4. **JWT issuance** continues unchanged.

## Error handling

| Class | Handling |
|---|---|
| Malformed env var (parse) | `log.Fatal` at startup. Operator fixes and restarts. |
| Slug doesn't resolve to a workspace at startup | `log.Fatal`. Aggregated across rules so operator sees all bad slugs at once. |
| Duplicate domain across rules | `log.Fatal`. Mapping is ambiguous; refuse to start. |
| `CountMembershipsByUser` query fails | Log warn, skip (treat as "has memberships" — fail closed for safety). |
| `CreateMember` returns unique-violation | Already a member; log debug, return nil. Idempotent. |
| `CreateMember` returns any other error | Log warn, return nil. User still logs in. Next zero-memberships sign-in retries. |
| Empty env var | No rules, no behavior change. The feature is off-by-default. |

## Testing

### Unit (no DB) — `server/internal/auth/lazy_invite_test.go`

- `Parse_ValidRules` — happy path with two rules, slugs resolved
- `Parse_EmptyInput` — returns empty `LazyInviteRules`, no error
- `Parse_MalformedPair` — missing `:` → error
- `Parse_DuplicateDomain` — same domain twice → error
- `Parse_UnknownSlug` — slug doesn't resolve → error mentions the slug
- `Match_ExactCaseInsensitive` — `Alice@SpendBase.COM` matches `spendbase.com`
- `Match_NoSubdomain` — `bob@dev.spendbase.com` does NOT match `spendbase.com`
- `Match_NoMatchReturnsFalse`
- `IsAllowedDomain_DelegatesToMatch`

Mock approach: `Queries` interface in test stubs `GetWorkspaceBySlug` to return a fixed UUID for known slugs and `pgx.ErrNoRows` for others. Reuses the `mockDB` pattern from `auth_signup_test.go` if it generalizes; otherwise a tiny purpose-built fake.

### Handler — `server/internal/handler/lazy_invite_test.go`

Uses the existing real-Postgres test pattern (`TestMain` in `handler_test.go`).

- `NewUserMatchingDomain_GetsMembership`
- `NewUserNonMatchingDomain_NoMembership`
- `ExistingUserZeroMemberships_GetsMembership`
- `ExistingUserHasMemberships_NoExtraMembership`
- `AlreadyMemberOfTargetWorkspace_NoOpNoError`
- `MembershipInsertFails_LoginStillSucceeds` (induce via a closed pool / row-level fail; if too brittle, drop and rely on code review)

### Integration smoke

A single end-to-end test through `VerifyCode`:
- Set up `LAZY_INVITE_RULES=test.example.com:slug-of-test-fixture-workspace`
- POST `/auth/send-code` with `alice@test.example.com`
- POST `/auth/verify-code` with the dev verification code
- Assert: response is 200, the new user row exists, the user is a member of the fixture workspace.

## Out of scope

- Per-workspace UI for owners to self-serve domain rules → would be Approach 2 from the design discussion. Defer until there's a concrete request.
- Audit log entry for the auto-add → can layer onto the existing `slog.Info` line in EnsureLazyInviteMembership later. Not needed for v1.
- Notification email to existing workspace admins ("alice@spendbase.com auto-joined") → defer.
- Lazy-invite via `workspace_invitation` row instead of direct `member` insert → adds complexity for no user-visible benefit; lazy-invite is intentionally invitation-less.
- Removing a user from a lazy-invite workspace because they no longer match the domain (e.g. employee left the company) → out of scope; admins do that through the existing remove-member flow.
