package tagaccess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidProjection  = errors.New("invalid Tag access projection")
	ErrUnverifiedDelivery = errors.New("unverified Tag authority delivery")
	ErrInvalidGrant       = errors.New("invalid Tag session grant")
	ErrGrantDenied        = errors.New("Tag session grant denied")
	errAccessNotFound     = errors.New("Tag access state not found")
	errGrantNotFound      = errors.New("Tag session grant not found")
)

const (
	maxStableIDLength  = 255
	maxDatabaseCounter = uint64(1<<63 - 1)
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusRemoved  Status = "removed"
	StatusDisabled Status = "disabled"
)

type ProjectionEvent struct {
	EventID              string `json:"eventId"`
	VIBESUserID          string `json:"vibesUserId"`
	WorkspaceID          string `json:"workspaceId"`
	Role                 Role   `json:"role"`
	Status               Status `json:"status"`
	AccountEpoch         uint64 `json:"accountEpoch"`
	MembershipGeneration uint64 `json:"membershipGeneration"`
	AuthorityVersion     uint64 `json:"authorityVersion"`
}

type DeliveryKind string

const (
	DeliveryIncremental DeliveryKind = "incremental"
	DeliverySnapshot    DeliveryKind = "snapshot"
	DeliveryReconcile   DeliveryKind = "reconcile"
)

// ProjectionDelivery is the verified transport contract at the AccessGate
// seam. Incremental deliveries contain exactly one changed Membership and
// advance from BaselineAuthorityVersion. Snapshot and reconcile deliveries are
// complete Workspace Membership snapshots covered by an authority assertion.
type ProjectionDelivery struct {
	Kind                     DeliveryKind      `json:"kind"`
	BaselineAuthorityVersion uint64            `json:"baselineAuthorityVersion"`
	AuthorityAssertionID     string            `json:"authorityAssertionId"`
	Projections              []ProjectionEvent `json:"projections"`
}

type deliveryVerifier interface {
	// Verify returns nil only after authenticating the complete delivery,
	// including kind, baseline, assertion identity, Workspace, version, and
	// Membership payload. AuthorityAssertionID is correlation evidence, not a
	// bearer credential by itself.
	Verify(context.Context, ProjectionDelivery) error
}

type ApplyResult string

const (
	ApplyApplied   ApplyResult = "applied"
	ApplyDuplicate ApplyResult = "duplicate"
	ApplyStale     ApplyResult = "stale"
	ApplyGap       ApplyResult = "gap"
	ApplyConflict  ApplyResult = "conflict"
)

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type AccessRequest struct {
	TagSessionID               string
	VIBESSessionID             string
	VIBESUserID                string
	WorkspaceID                string
	AccountEpoch               uint64
	SessionWorkspaceGeneration uint64
	MembershipGeneration       uint64
	AuthorityVersion           uint64
}

type DenyReason string

const (
	DenyUnknownProjection               DenyReason = "unknown_projection"
	DenyStoreUnavailable                DenyReason = "store_unavailable"
	DenyProjectionGap                   DenyReason = "projection_gap"
	DenyProjectionConflict              DenyReason = "projection_conflict"
	DenyIdentityRestrictionGap          DenyReason = "identity_restriction_gap"
	DenyIdentityRestrictionConflict     DenyReason = "identity_restriction_conflict"
	DenyInactiveMembership              DenyReason = "inactive_membership"
	DenyMissingGrant                    DenyReason = "missing_grant"
	DenyExpiredGrant                    DenyReason = "expired_grant"
	DenyStaleAccountEpoch               DenyReason = "stale_account_epoch"
	DenyStaleSessionWorkspaceGeneration DenyReason = "stale_session_workspace_generation"
	DenyStaleGeneration                 DenyReason = "stale_membership_generation"
	DenyStaleVersion                    DenyReason = "stale_authority_version"
)

type Decision struct {
	Allowed          bool
	Reason           DenyReason
	Role             Role
	AuthorityVersion uint64
}

type SessionGrant struct {
	TagSessionID               string
	VIBESSessionID             string
	VIBESUserID                string
	WorkspaceID                string
	AccountEpoch               uint64
	SessionWorkspaceGeneration uint64
	MembershipGeneration       uint64
	AuthorityVersion           uint64
	SessionExpiresAt           time.Time
	GrantExpiresAt             time.Time
	// Continuous is reserved for a fresh, request-bound gateway assertion. It
	// may refresh projection counters only for an already handoff-bound session
	// and Workspace; it can never create or switch the binding.
	Continuous bool
}

type projectionIntegrity string

const (
	integrityHealthy  projectionIntegrity = "healthy"
	integrityGap      projectionIntegrity = "gap"
	integrityConflict projectionIntegrity = "conflict"
)

type accessState struct {
	projection     ProjectionEvent
	integrity      projectionIntegrity
	identity       identityRecord
	identityExists bool
	session        SessionGrant
}

type store interface {
	applyProjection(context.Context, ProjectionDelivery, [32]byte) (ApplyResult, error)
	applyIdentityRestriction(context.Context, IdentityRestrictionDelivery, [32]byte) (ApplyResult, error)
	applySessionWorkspaceSupersession(context.Context, SessionWorkspaceSupersededDelivery, [32]byte) (ApplyResult, error)
	createGrant(context.Context, SessionGrant, time.Time) error
	loadAccess(context.Context, AccessRequest) (accessState, error)
}

type Gate struct {
	store    store
	clock    Clock
	verifier deliveryVerifier
}

func newGate(adapter store, clock Clock, verifier deliveryVerifier) *Gate {
	return &Gate{store: adapter, clock: clock, verifier: verifier}
}

// ApplyProjection applies one ordinary Workspace-global incremental event. Its
// baseline is the immediately preceding global authority version; it can never
// bootstrap or reconcile a missing version.
func (g *Gate) ApplyProjection(ctx context.Context, event ProjectionEvent) (ApplyResult, error) {
	if event.AuthorityVersion == 0 {
		return "", ErrInvalidProjection
	}
	return g.applyDelivery(ctx, ProjectionDelivery{
		Kind:                     DeliveryIncremental,
		BaselineAuthorityVersion: event.AuthorityVersion - 1,
		Projections:              []ProjectionEvent{event},
	})
}

// ApplyAuthorityDelivery applies an explicitly verified complete Workspace
// snapshot or reconcile delivery. Callers cannot use this seam without the
// verifier injected when the Gate was constructed.
func (g *Gate) ApplyAuthorityDelivery(ctx context.Context, delivery ProjectionDelivery) (ApplyResult, error) {
	if delivery.Kind == DeliveryIncremental {
		return "", ErrInvalidProjection
	}
	return g.applyDelivery(ctx, delivery)
}

func (g *Gate) applyDelivery(ctx context.Context, delivery ProjectionDelivery) (ApplyResult, error) {
	if !validDelivery(delivery) {
		return "", ErrInvalidProjection
	}
	if g.verifier == nil || g.verifier.Verify(ctx, delivery) != nil {
		return "", ErrUnverifiedDelivery
	}
	normalized := normalizedDelivery(delivery)
	payload, err := json.Marshal(deliveryPayload(normalized))
	if err != nil {
		return "", ErrInvalidProjection
	}
	return g.applyVerifiedDelivery(ctx, normalized, sha256.Sum256(payload))
}

func (g *Gate) applyVerifiedDelivery(ctx context.Context, delivery ProjectionDelivery, digest [32]byte) (ApplyResult, error) {
	if !validDelivery(delivery) {
		return "", ErrInvalidProjection
	}
	return g.store.applyProjection(ctx, normalizedDelivery(delivery), digest)
}

// GrantSession records a VIBES-session-bound Tag session and its Workspace
// grant only when the supplied generation and version exactly match projection.
func (g *Gate) GrantSession(ctx context.Context, grant SessionGrant) error {
	now := g.clock.Now()
	if !validStableID(grant.TagSessionID) || !validStableID(grant.VIBESSessionID) || !validStableID(grant.VIBESUserID) || !validStableID(grant.WorkspaceID) ||
		grant.AccountEpoch == 0 || grant.AccountEpoch > maxDatabaseCounter ||
		grant.SessionWorkspaceGeneration == 0 || grant.SessionWorkspaceGeneration > maxDatabaseCounter ||
		grant.MembershipGeneration == 0 || grant.MembershipGeneration > maxDatabaseCounter ||
		grant.AuthorityVersion == 0 || grant.AuthorityVersion > maxDatabaseCounter ||
		!grant.SessionExpiresAt.After(now) || !grant.GrantExpiresAt.After(now) || grant.GrantExpiresAt.After(grant.SessionExpiresAt) {
		return ErrInvalidGrant
	}
	return g.store.createGrant(ctx, grant, now)
}

// Authorize returns an explicit denial rather than an error so every HTTP,
// WebSocket, daemon, and worker adapter shares the same fail-closed behavior.
func (g *Gate) Authorize(ctx context.Context, request AccessRequest) Decision {
	if request.TagSessionID == "" || request.VIBESSessionID == "" || request.VIBESUserID == "" || request.WorkspaceID == "" ||
		request.AccountEpoch == 0 || request.AccountEpoch > maxDatabaseCounter ||
		request.SessionWorkspaceGeneration == 0 || request.SessionWorkspaceGeneration > maxDatabaseCounter ||
		request.MembershipGeneration == 0 || request.MembershipGeneration > maxDatabaseCounter ||
		request.AuthorityVersion == 0 || request.AuthorityVersion > maxDatabaseCounter {
		return Decision{Reason: DenyMissingGrant}
	}
	state, err := g.store.loadAccess(ctx, request)
	if err != nil {
		if errors.Is(err, errAccessNotFound) {
			return Decision{Reason: DenyUnknownProjection}
		}
		if errors.Is(err, errGrantNotFound) {
			return Decision{Reason: DenyMissingGrant}
		}
		return Decision{Reason: DenyStoreUnavailable}
	}
	switch state.integrity {
	case integrityGap:
		return Decision{Reason: DenyProjectionGap}
	case integrityConflict:
		return Decision{Reason: DenyProjectionConflict}
	case integrityHealthy:
	default:
		return Decision{Reason: DenyUnknownProjection}
	}
	if state.identityExists {
		switch state.identity.integrity {
		case integrityGap:
			return Decision{Reason: DenyIdentityRestrictionGap}
		case integrityConflict:
			return Decision{Reason: DenyIdentityRestrictionConflict}
		case integrityHealthy:
		default:
			return Decision{Reason: DenyUnknownProjection}
		}
	}
	if state.projection.Status != StatusActive {
		return Decision{Reason: DenyInactiveMembership}
	}
	if state.projection.Role != RoleOwner && state.projection.Role != RoleAdmin && state.projection.Role != RoleMember {
		return Decision{Reason: DenyUnknownProjection}
	}
	if state.session.TagSessionID != request.TagSessionID || state.session.VIBESUserID != request.VIBESUserID ||
		state.session.WorkspaceID != request.WorkspaceID || state.session.VIBESSessionID != request.VIBESSessionID {
		return Decision{Reason: DenyMissingGrant}
	}
	now := g.clock.Now()
	if !state.session.SessionExpiresAt.After(now) || !state.session.GrantExpiresAt.After(now) {
		return Decision{Reason: DenyExpiredGrant}
	}
	if state.session.SessionWorkspaceGeneration != request.SessionWorkspaceGeneration {
		return Decision{Reason: DenyStaleSessionWorkspaceGeneration}
	}
	if state.session.AccountEpoch != state.projection.AccountEpoch || request.AccountEpoch != state.projection.AccountEpoch {
		return Decision{Reason: DenyStaleAccountEpoch}
	}
	if state.session.MembershipGeneration != state.projection.MembershipGeneration || request.MembershipGeneration != state.projection.MembershipGeneration {
		return Decision{Reason: DenyStaleGeneration}
	}
	if state.session.AuthorityVersion != state.projection.AuthorityVersion || request.AuthorityVersion != state.projection.AuthorityVersion {
		return Decision{Reason: DenyStaleVersion}
	}
	return Decision{Allowed: true, Role: state.projection.Role, AuthorityVersion: state.projection.AuthorityVersion}
}

// BrowserTagSessionID is the stable #299 handoff/HTTP grant key. Workspace
// switches reuse this key so a higher session Workspace generation can
// atomically supersede every earlier Workspace grant for the VIBES session.
func BrowserTagSessionID(vibesUserID, vibesSessionID string) string {
	digest := sha256.Sum256([]byte(vibesUserID + "\x00" + vibesSessionID))
	return "tag-browser-" + hex.EncodeToString(digest[:])
}

// CLITagSessionID keeps a CLI receiver's durable grant separate from the
// browser Gateway session while retaining the exact VIBES session authority.
func CLITagSessionID(vibesUserID, vibesSessionID, receiverID string) string {
	digest := sha256.Sum256([]byte(vibesUserID + "\x00" + vibesSessionID + "\x00" + receiverID))
	return "tag-cli-" + hex.EncodeToString(digest[:])
}

func validProjection(event ProjectionEvent) bool {
	if !validStableID(event.EventID) || !validStableID(event.VIBESUserID) || !validStableID(event.WorkspaceID) ||
		event.AccountEpoch == 0 || event.AccountEpoch > maxDatabaseCounter ||
		event.MembershipGeneration == 0 || event.MembershipGeneration > maxDatabaseCounter ||
		event.AuthorityVersion == 0 || event.AuthorityVersion > maxDatabaseCounter {
		return false
	}
	if event.Role != RoleOwner && event.Role != RoleAdmin && event.Role != RoleMember {
		return false
	}
	return event.Status == StatusActive || event.Status == StatusRemoved || event.Status == StatusDisabled
}

func validDelivery(delivery ProjectionDelivery) bool {
	if len(delivery.Projections) == 0 || delivery.BaselineAuthorityVersion > maxDatabaseCounter {
		return false
	}
	first := delivery.Projections[0]
	seenUsers := make(map[string]struct{}, len(delivery.Projections))
	for _, projection := range delivery.Projections {
		if !validProjection(projection) || projection.WorkspaceID != first.WorkspaceID || projection.AuthorityVersion != first.AuthorityVersion {
			return false
		}
		if _, duplicate := seenUsers[projection.VIBESUserID]; duplicate {
			return false
		}
		seenUsers[projection.VIBESUserID] = struct{}{}
	}
	switch delivery.Kind {
	case DeliveryIncremental:
		return len(delivery.Projections) == 1 && delivery.AuthorityAssertionID == "" &&
			first.AuthorityVersion == delivery.BaselineAuthorityVersion+1
	case DeliverySnapshot, DeliveryReconcile:
		return validStableID(delivery.AuthorityAssertionID) && delivery.BaselineAuthorityVersion == first.AuthorityVersion
	default:
		return false
	}
}

func normalizedDelivery(delivery ProjectionDelivery) ProjectionDelivery {
	normalized := delivery
	normalized.Projections = append([]ProjectionEvent(nil), delivery.Projections...)
	sort.Slice(normalized.Projections, func(left, right int) bool {
		return normalized.Projections[left].VIBESUserID < normalized.Projections[right].VIBESUserID
	})
	return normalized
}

func deliveryPayload(delivery ProjectionDelivery) ProjectionDelivery {
	payload := delivery
	payload.Projections = append([]ProjectionEvent(nil), delivery.Projections...)
	for index := range payload.Projections {
		payload.Projections[index].EventID = ""
	}
	return payload
}

func validStableID(value string) bool {
	return value != "" && len(value) <= maxStableIDLength && !strings.ContainsRune(value, '\x00')
}
