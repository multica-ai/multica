package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/identitysync"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Service holds the auto-provisioning logic + settings CRUD. Concrete type
// behind the upstream IdentityProvisionerInvoker seam.
type Service struct {
	cerebro     *cerebrodb.Queries
	upstream    *db.Queries
	groupSyncer workspaceGroupSyncer
}

type workspaceGroupSyncer interface {
	SyncWorkspaceNow(ctx context.Context, workspaceID pgtype.UUID) (identitysync.SyncResult, error)
}

// New constructs a Service. Both query handles must be non-nil — the cerebro
// handle reads the per-workspace settings; the upstream handle creates the
// member row.
func New(cerebro *cerebrodb.Queries, upstream *db.Queries) *Service {
	return &Service{cerebro: cerebro, upstream: upstream}
}

func NewWithGroupSyncer(cerebro *cerebrodb.Queries, upstream *db.Queries, groupSyncer workspaceGroupSyncer) *Service {
	return &Service{cerebro: cerebro, upstream: upstream, groupSyncer: groupSyncer}
}

// Compile-time assertion that *Service satisfies the upstream seam.
var _ handler.IdentityProvisionerInvoker = (*Service)(nil)

// ErrInvalidRole is returned by Update when the caller asks to store a
// default_role outside the {owner, admin, member} set. Surfaced as 400.
var ErrInvalidRole = errors.New("invalid default_role")

// ErrInvalidDomain is returned by Update when a submitted domain is empty,
// has whitespace, or contains an '@'. Surfaced as 400.
var ErrInvalidDomain = errors.New("invalid signup domain")

// Get returns the saved settings for a workspace, or the empty default when
// no row exists. The "empty default" path means the UI can render a clean
// form on first visit without a separate create step.
func (s *Service) Get(ctx context.Context, workspaceID pgtype.UUID) (AuthSettings, error) {
	row, err := s.cerebro.GetCerebroWorkspaceAuthSettings(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return defaultAuthSettings(uuidString(workspaceID)), nil
		}
		return AuthSettings{}, err
	}
	return toAuthSettings(row), nil
}

// Update upserts the workspace's settings. Caller authorization (owner/admin
// only) is enforced by the router middleware.
func (s *Service) Update(
	ctx context.Context,
	workspaceID pgtype.UUID,
	actorID pgtype.UUID,
	req UpdateAuthSettingsRequest,
) (AuthSettings, error) {
	domains, err := normalizeDomains(req.GoogleSignupDomains)
	if err != nil {
		return AuthSettings{}, err
	}
	role := strings.TrimSpace(req.DefaultRole)
	if role == "" {
		role = "member"
	}
	if _, ok := allowedRoles[role]; !ok {
		return AuthSettings{}, ErrInvalidRole
	}
	row, err := s.cerebro.UpsertCerebroWorkspaceAuthSettings(ctx, cerebrodb.UpsertCerebroWorkspaceAuthSettingsParams{
		WorkspaceID:                workspaceID,
		GoogleSignupDomains:        domains,
		DefaultRole:                role,
		GoogleWorkspaceSyncEnabled: req.GoogleWorkspaceSyncEnabled,
		UpdatedByUserID:            actorID,
	})
	if err != nil {
		return AuthSettings{}, err
	}
	return toAuthSettings(row), nil
}

// ProvisionMembershipForNewUser is the upstream-seam method invoked from the
// login path on EVERY successful login (not only first signup) so it doubles
// as a self-heal: a user who logged in before their workspace was configured
// gets reconciled on their next login. It looks up every workspace whose
// google_signup_domains includes the user's email domain, ensures a member row
// (with the per-workspace default role) AND places the user in that
// workspace's configured default group. Idempotent throughout: an existing
// member row or group membership (returning login) does not error.
//
// Best-effort: a per-workspace insert failure is logged via the error return
// for the FIRST error encountered but the loop still tries every other
// matching workspace. Returning only the first error keeps the upstream
// log-line concise; the auto-provisioning hook treats any error as warn-
// level and never propagates to the login response.
func (s *Service) ProvisionMembershipForNewUser(
	ctx context.Context,
	user db.User,
	authSource string,
) (handler.ProvisionMembershipResult, error) {
	email := strings.ToLower(strings.TrimSpace(user.Email))
	domain := emailDomain(email)
	if domain == "" {
		return handler.ProvisionMembershipResult{Skipped: true, SkipReason: "no email domain"}, nil
	}
	matches, err := s.cerebro.ListCerebroWorkspaceAuthSettingsForDomain(ctx, domain)
	if err != nil {
		return handler.ProvisionMembershipResult{}, fmt.Errorf("list workspaces by domain: %w", err)
	}
	if len(matches) == 0 {
		return handler.ProvisionMembershipResult{Skipped: true, SkipReason: "no workspace matches domain"}, nil
	}
	out := handler.ProvisionMembershipResult{}
	var firstErr error
	syncWorkspaces := make([]pgtype.UUID, 0, len(matches))
	for _, m := range matches {
		role := m.DefaultRole
		if _, ok := allowedRoles[role]; !ok {
			// Defensive: a misconfigured row should not block auto-
			// provisioning into other matching workspaces. Fall back to the
			// safest role rather than skipping the workspace.
			role = "member"
		}
		// Ensure workspace membership. A unique-constraint violation means the
		// user is already a member (a returning login re-running this path) —
		// treat that as success so we still reconcile group placement below.
		if _, err := s.upstream.CreateMember(ctx, db.CreateMemberParams{
			WorkspaceID: m.WorkspaceID,
			UserID:      user.ID,
			Role:        role,
		}); err != nil && !isUniqueViolation(err) {
			if firstErr == nil {
				firstErr = fmt.Errorf("create member in workspace %s: %w", uuidString(m.WorkspaceID), err)
			}
			continue
		}
		out.WorkspaceIDs = append(out.WorkspaceIDs, m.WorkspaceID)
		if m.GoogleWorkspaceSyncEnabled {
			syncWorkspaces = append(syncWorkspaces, m.WorkspaceID)
		}

		// Place the user in the workspace's configured default group so they
		// land with working permissions from the first login. Best-effort and
		// idempotent: a workspace with no default group (pgx.ErrNoRows) is
		// simply skipped, and AddCerebroGroupMember swallows the duplicate on a
		// returning login. A failure here must not block other workspaces.
		groupID, err := s.cerebro.GetCerebroDefaultGroupID(ctx, m.WorkspaceID)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) && firstErr == nil {
				firstErr = fmt.Errorf("lookup default group for workspace %s: %w", uuidString(m.WorkspaceID), err)
			}
			continue
		}
		if _, err := s.cerebro.AddCerebroGroupMember(ctx, cerebrodb.AddCerebroGroupMemberParams{
			GroupID: groupID,
			UserID:  user.ID,
			AddedBy: pgtype.UUID{}, // system action — no human actor
		}); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("add default-group member in workspace %s: %w", uuidString(m.WorkspaceID), err)
			}
			continue
		}
		out.GroupIDs = append(out.GroupIDs, groupID)
	}

	// If the user was placed in at least one workspace, mark them onboarded so
	// the post-login router sends them straight into the workspace instead of
	// the first-run onboarding wall. This mirrors the invitation-accept path
	// (handler/invitation.go), which holds the member<->onboarded_at invariant
	// the desktop workspace layout depends on. Idempotent (MarkUserOnboarded
	// COALESCEs onboarded_at, so a returning user keeps their original time).
	if len(out.WorkspaceIDs) > 0 {
		if _, err := s.upstream.MarkUserOnboarded(ctx, user.ID); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("mark user onboarded %s: %w", uuidString(user.ID), err)
		}
	}
	if s.groupSyncer != nil {
		for _, workspaceID := range syncWorkspaces {
			if _, err := s.groupSyncer.SyncWorkspaceNow(ctx, workspaceID); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("sync Google Workspace groups for workspace %s: %w", uuidString(workspaceID), err)
			}
		}
	}
	return out, firstErr
}

// normalizeDomains lowercases, trims, and de-duplicates each requested domain.
// Empty strings and entries containing '@' or whitespace are rejected.
func normalizeDomains(in []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		d := strings.ToLower(strings.TrimSpace(raw))
		if d == "" {
			return nil, ErrInvalidDomain
		}
		if strings.ContainsAny(d, "@ \t\r\n") {
			return nil, ErrInvalidDomain
		}
		if _, dup := seen[d]; dup {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out, nil
}

func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return email[at+1:]
}

// uuidString renders a pgtype.UUID for logging / response use. Keeps the
// service file self-contained instead of pulling in the util package for one
// line.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", u.Bytes[0:4], u.Bytes[4:6], u.Bytes[6:8], u.Bytes[8:10], u.Bytes[10:16])
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505). Used to swallow the race where two parallel
// logins both try to insert the same (workspace, user) pair.
func isUniqueViolation(err error) bool {
	// pgconn.PgError lives in jackc/pgx/v5/pgconn. We compare on the string
	// SQLSTATE to keep the import surface small; the cost is one substring
	// match per error, which only fires on the rare insert-collision path.
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "SQLSTATE 23505")
}
