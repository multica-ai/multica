package tagaccess

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mu                 sync.RWMutex
	projections        map[projectionKey]ProjectionEvent
	workspaces         map[string]workspaceRecord
	deliveries         map[workspaceDeliveryKey][32]byte
	identityStates     map[string]identityRecord
	identityDeliveries map[identityDeliveryKey][32]byte
	sessionRevocations map[identitySessionKey]struct{}
	sessions           map[string]memorySession
	grants             map[workspaceGrantKey]memoryGrant
	failure            error
}

type projectionKey struct {
	userID      string
	workspaceID string
}

type workspaceDeliveryKey struct {
	workspaceID      string
	authorityVersion uint64
}

type workspaceGrantKey struct {
	tagSessionID string
	workspaceID  string
}

type identityDeliveryKey struct {
	userID  string
	version uint64
}

type identitySessionKey struct {
	userID    string
	sessionID string
}

type memorySession struct {
	vibesSessionID string
	vibesUserID    string
	accountEpoch   uint64
	expiresAt      time.Time
	revoked        bool
}

type memoryGrant struct {
	membershipGeneration uint64
	authorityVersion     uint64
	expiresAt            time.Time
	revoked              bool
}

func (s *MemoryStore) SetFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failure = err
}

// NewMemoryStore creates a concurrency-safe fixture adapter with the same
// ordering and grant rules as the private PostgreSQL adapter.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		projections:        make(map[projectionKey]ProjectionEvent),
		workspaces:         make(map[string]workspaceRecord),
		deliveries:         make(map[workspaceDeliveryKey][32]byte),
		identityStates:     make(map[string]identityRecord),
		identityDeliveries: make(map[identityDeliveryKey][32]byte),
		sessionRevocations: make(map[identitySessionKey]struct{}),
		sessions:           make(map[string]memorySession),
		grants:             make(map[workspaceGrantKey]memoryGrant),
	}
}

func (s *MemoryStore) applyIdentityRestriction(_ context.Context, delivery IdentityRestrictionDelivery, digest [32]byte) (ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return "", s.failure
	}
	key := identityDeliveryKey{userID: delivery.VIBESUserID, version: delivery.IdentityRestrictionVersion}
	observedDigest, observed := s.identityDeliveries[key]
	current, exists := s.identityStates[delivery.VIBESUserID]
	next, result, changed, apply := evolveIdentity(current, exists, delivery, digest, observedDigest, observed)
	if !observed {
		s.identityDeliveries[key] = digest
	}
	if changed {
		s.identityStates[delivery.VIBESUserID] = next
	}
	if apply {
		s.applyMemoryIdentityRevocation(delivery)
	}
	return result, nil
}

func (s *MemoryStore) applyMemoryIdentityRevocation(delivery IdentityRestrictionDelivery) {
	if delivery.Kind == IdentityRestrictionSessionLogout {
		s.sessionRevocations[identitySessionKey{userID: delivery.VIBESUserID, sessionID: delivery.VIBESSessionID}] = struct{}{}
	}
	for tagSessionID, session := range s.sessions {
		matches := session.vibesUserID == delivery.VIBESUserID
		if delivery.Kind == IdentityRestrictionSessionLogout {
			matches = matches && session.vibesSessionID == delivery.VIBESSessionID
		}
		if !matches {
			continue
		}
		session.revoked = true
		s.sessions[tagSessionID] = session
		for key, grant := range s.grants {
			if key.tagSessionID == tagSessionID {
				grant.revoked = true
				s.grants[key] = grant
			}
		}
	}
}

func (s *MemoryStore) applyProjection(_ context.Context, delivery ProjectionDelivery, digest [32]byte) (ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return "", s.failure
	}
	first := delivery.Projections[0]
	current, ok := s.workspaces[first.WorkspaceID]
	deliveryKey := workspaceDeliveryKey{workspaceID: first.WorkspaceID, authorityVersion: first.AuthorityVersion}
	observedDigest, observed := s.deliveries[deliveryKey]
	next, result, changed, applyMembers := evolveWorkspace(current, ok, delivery, digest, observedDigest, observed)
	if applyMembers {
		for _, projection := range delivery.Projections {
			key := projectionKey{userID: projection.VIBESUserID, workspaceID: projection.WorkspaceID}
			persisted, exists := s.projections[key]
			if !validProjectionTransition(persisted, exists, projection) {
				next.integrity = integrityConflict
				next.blockedDigest = digest
				result = ApplyConflict
				applyMembers = false
				changed = true
				break
			}
		}
	}
	if changed {
		s.workspaces[first.WorkspaceID] = next
	}
	if applyMembers {
		if delivery.Kind != DeliveryIncremental {
			included := make(map[string]struct{}, len(delivery.Projections))
			for _, projection := range delivery.Projections {
				included[projection.VIBESUserID] = struct{}{}
			}
			for key := range s.projections {
				if key.workspaceID == first.WorkspaceID {
					if _, present := included[key.userID]; !present {
						s.projections[key] = omittedProjection(s.projections[key], first)
					}
				}
			}
		}
		for _, projection := range delivery.Projections {
			s.projections[projectionKey{userID: projection.VIBESUserID, workspaceID: projection.WorkspaceID}] = projection
		}
	}
	if !observed {
		s.deliveries[deliveryKey] = digest
	}
	return result, nil
}

func (s *MemoryStore) createGrant(_ context.Context, grant SessionGrant, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return s.failure
	}
	if !grant.SessionExpiresAt.After(now) || !grant.GrantExpiresAt.After(now) {
		return ErrGrantDenied
	}
	workspace, workspaceExists := s.workspaces[grant.WorkspaceID]
	projection, ok := s.projections[projectionKey{userID: grant.VIBESUserID, workspaceID: grant.WorkspaceID}]
	if !workspaceExists || workspace.integrity != integrityHealthy || !ok || projection.Status != StatusActive ||
		projection.AccountEpoch != grant.AccountEpoch ||
		projection.MembershipGeneration != grant.MembershipGeneration ||
		projection.AuthorityVersion != grant.AuthorityVersion {
		return ErrGrantDenied
	}
	session, sessionExists := s.sessions[grant.TagSessionID]
	if identity, exists := s.identityStates[grant.VIBESUserID]; exists &&
		(identity.integrity != integrityHealthy || grant.AccountEpoch < identity.accountEpoch || grant.AccountEpoch <= identity.revokedThrough) {
		return ErrGrantDenied
	}
	if _, revoked := s.sessionRevocations[identitySessionKey{userID: grant.VIBESUserID, sessionID: grant.VIBESSessionID}]; revoked {
		return ErrGrantDenied
	}
	if sessionExists {
		if session.revoked || session.vibesSessionID != grant.VIBESSessionID || session.vibesUserID != grant.VIBESUserID || session.accountEpoch != grant.AccountEpoch {
			return ErrGrantDenied
		}
		if grant.SessionExpiresAt.Before(session.expiresAt) {
			session.expiresAt = grant.SessionExpiresAt
		}
	} else {
		session = memorySession{
			vibesSessionID: grant.VIBESSessionID,
			vibesUserID:    grant.VIBESUserID,
			accountEpoch:   grant.AccountEpoch,
			expiresAt:      grant.SessionExpiresAt,
		}
	}
	grantKey := workspaceGrantKey{tagSessionID: grant.TagSessionID, workspaceID: grant.WorkspaceID}
	existingGrant, grantExists := s.grants[grantKey]
	if grantExists && (existingGrant.revoked || existingGrant.membershipGeneration != grant.MembershipGeneration || existingGrant.authorityVersion > grant.AuthorityVersion) {
		return ErrGrantDenied
	}
	grantExpiry := grant.GrantExpiresAt
	if grantExpiry.After(session.expiresAt) {
		grantExpiry = session.expiresAt
	}
	s.sessions[grant.TagSessionID] = session
	s.grants[grantKey] = memoryGrant{
		membershipGeneration: grant.MembershipGeneration,
		authorityVersion:     grant.AuthorityVersion,
		expiresAt:            grantExpiry,
	}
	return nil
}

func (s *MemoryStore) loadAccess(_ context.Context, request AccessRequest) (accessState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.failure != nil {
		return accessState{}, s.failure
	}
	projection, ok := s.projections[projectionKey{userID: request.VIBESUserID, workspaceID: request.WorkspaceID}]
	if !ok {
		return accessState{}, errAccessNotFound
	}
	workspace, ok := s.workspaces[request.WorkspaceID]
	if !ok {
		return accessState{}, errAccessNotFound
	}
	session, ok := s.sessions[request.TagSessionID]
	if !ok || session.revoked || session.vibesUserID != request.VIBESUserID {
		return accessState{}, errGrantNotFound
	}
	grant, ok := s.grants[workspaceGrantKey{tagSessionID: request.TagSessionID, workspaceID: request.WorkspaceID}]
	if !ok || grant.revoked {
		return accessState{}, errGrantNotFound
	}
	identity, identityExists := s.identityStates[request.VIBESUserID]
	return accessState{
		projection:     projection,
		integrity:      workspace.integrity,
		identity:       identity,
		identityExists: identityExists,
		session: SessionGrant{
			TagSessionID:         request.TagSessionID,
			VIBESSessionID:       session.vibesSessionID,
			VIBESUserID:          session.vibesUserID,
			WorkspaceID:          request.WorkspaceID,
			AccountEpoch:         session.accountEpoch,
			MembershipGeneration: grant.membershipGeneration,
			AuthorityVersion:     grant.authorityVersion,
			SessionExpiresAt:     session.expiresAt,
			GrantExpiresAt:       grant.expiresAt,
		},
	}, nil
}
