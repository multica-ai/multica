package tagaccess

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mu                          sync.RWMutex
	projections                 map[projectionKey]ProjectionEvent
	workspaces                  map[string]workspaceRecord
	deliveries                  map[workspaceDeliveryKey][32]byte
	identityStates              map[string]identityRecord
	identityDeliveries          map[identityDeliveryKey][32]byte
	sessionWorkspaceStates      map[sessionWorkspaceKey]sessionWorkspaceRecord
	sessionWorkspaceDeliveries  map[sessionWorkspaceDeliveryKey][32]byte
	sessionWorkspaceEvents      map[string]sessionWorkspaceDeliveryKey
	sessionWorkspaceDeliveryIDs map[string]sessionWorkspaceDeliveryKey
	sessionWorkspaceIdempotency map[string]sessionWorkspaceDeliveryKey
	sessionRevocations          map[identitySessionKey]struct{}
	sessions                    map[string]memorySession
	grants                      map[workspaceGrantKey]memoryGrant
	failure                     error
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
	vibesSessionID             string
	vibesUserID                string
	accountEpoch               uint64
	sessionWorkspaceGeneration uint64
	expiresAt                  time.Time
	revoked                    bool
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
		projections:                 make(map[projectionKey]ProjectionEvent),
		workspaces:                  make(map[string]workspaceRecord),
		deliveries:                  make(map[workspaceDeliveryKey][32]byte),
		identityStates:              make(map[string]identityRecord),
		identityDeliveries:          make(map[identityDeliveryKey][32]byte),
		sessionWorkspaceStates:      make(map[sessionWorkspaceKey]sessionWorkspaceRecord),
		sessionWorkspaceDeliveries:  make(map[sessionWorkspaceDeliveryKey][32]byte),
		sessionWorkspaceEvents:      make(map[string]sessionWorkspaceDeliveryKey),
		sessionWorkspaceDeliveryIDs: make(map[string]sessionWorkspaceDeliveryKey),
		sessionWorkspaceIdempotency: make(map[string]sessionWorkspaceDeliveryKey),
		sessionRevocations:          make(map[identitySessionKey]struct{}),
		sessions:                    make(map[string]memorySession),
		grants:                      make(map[workspaceGrantKey]memoryGrant),
	}
}

func (s *MemoryStore) applySessionWorkspaceSupersession(_ context.Context, delivery SessionWorkspaceSupersededDelivery, digest [32]byte) (ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return "", s.failure
	}
	key := sessionWorkspaceKey{userID: delivery.VIBESUserID, sessionID: delivery.VIBESSessionID}
	deliveryKey := sessionWorkspaceDeliveryKey{
		userID: delivery.VIBESUserID, sessionID: delivery.VIBESSessionID,
		generation: delivery.SessionWorkspaceGeneration,
	}
	observedDigest, observed := s.sessionWorkspaceDeliveries[deliveryKey]
	current, exists := s.sessionWorkspaceStates[key]
	eventKey, eventObserved := s.sessionWorkspaceEvents[delivery.EventID]
	deliveryIDKey, deliveryIDObserved := s.sessionWorkspaceDeliveryIDs[delivery.DeliveryID]
	idempotencyKey, idempotencyObserved := s.sessionWorkspaceIdempotency[delivery.IdempotencyKey]
	if eventObserved && eventKey != deliveryKey || deliveryIDObserved && deliveryIDKey != deliveryKey ||
		idempotencyObserved && idempotencyKey != deliveryKey {
		if !exists {
			current = sessionWorkspaceRecord{userID: delivery.VIBESUserID, sessionID: delivery.VIBESSessionID, accountEpoch: delivery.AccountEpoch, generation: 1}
		}
		current = blockSessionWorkspaceAdvance(current, digest, true)
		if delivery.SessionWorkspaceGeneration > current.observedGeneration {
			current.observedGeneration = delivery.SessionWorkspaceGeneration
		}
		s.sessionWorkspaceStates[key] = current
		return ApplyConflict, nil
	}
	next, result, changed, candidate := evolveSessionWorkspace(current, exists, delivery, digest, observedDigest, observed)
	if !observed {
		s.sessionWorkspaceDeliveries[deliveryKey] = digest
		s.sessionWorkspaceEvents[delivery.EventID] = deliveryKey
		s.sessionWorkspaceDeliveryIDs[delivery.DeliveryID] = deliveryKey
		s.sessionWorkspaceIdempotency[delivery.IdempotencyKey] = deliveryKey
	}
	if result == ApplyDuplicate || result == ApplyStale {
		if s.sessionWorkspaceIdentityPermanentlyRestricted(delivery) {
			next = blockSessionWorkspaceAdvance(next, digest, true)
			result, changed = ApplyConflict, true
		}
	}
	if candidate {
		permanent, ready := s.sessionWorkspaceDependencies(delivery)
		switch {
		case permanent:
			next = blockSessionWorkspaceAdvance(next, digest, true)
			result, changed = ApplyConflict, true
		case !ready:
			next = blockSessionWorkspaceAdvance(next, digest, false)
			result, changed = ApplyGap, true
		default:
			s.applyMemorySessionWorkspaceFence(delivery)
			next = completeSessionWorkspaceAdvance(next, delivery)
			result, changed = ApplyApplied, true
		}
	}
	if changed {
		s.sessionWorkspaceStates[key] = next
	}
	return result, nil
}

func (s *MemoryStore) sessionWorkspaceIdentityPermanentlyRestricted(delivery SessionWorkspaceSupersededDelivery) bool {
	if identity, exists := s.identityStates[delivery.VIBESUserID]; exists &&
		(identity.accountEpoch != 0 && identity.accountEpoch != delivery.AccountEpoch || identity.revokedThrough >= delivery.AccountEpoch) {
		return true
	}
	_, revoked := s.sessionRevocations[identitySessionKey{userID: delivery.VIBESUserID, sessionID: delivery.VIBESSessionID}]
	return revoked
}

func (s *MemoryStore) sessionWorkspaceDependencies(delivery SessionWorkspaceSupersededDelivery) (permanent, ready bool) {
	identity, identityExists := s.identityStates[delivery.VIBESUserID]
	if delivery.IdentityRestrictionVersion > 0 && (!identityExists || identity.version < delivery.IdentityRestrictionVersion) {
		return false, false
	}
	if identityExists {
		if identity.integrity != integrityHealthy {
			return false, false
		}
		if identity.accountEpoch != 0 && identity.accountEpoch != delivery.AccountEpoch {
			return true, false
		}
		if identity.revokedThrough >= delivery.AccountEpoch {
			return true, false
		}
	}
	if s.sessionWorkspaceIdentityPermanentlyRestricted(delivery) {
		return true, false
	}
	workspace, workspaceExists := s.workspaces[delivery.NewWorkspaceID]
	projection, projectionExists := s.projections[projectionKey{userID: delivery.VIBESUserID, workspaceID: delivery.NewWorkspaceID}]
	if !workspaceExists || !projectionExists || workspace.integrity != integrityHealthy ||
		workspace.authorityVersion < delivery.AuthorityVersion || projection.AuthorityVersion < delivery.AuthorityVersion {
		return false, false
	}
	if workspace.authorityVersion != delivery.AuthorityVersion || projection.AuthorityVersion != delivery.AuthorityVersion ||
		projection.AccountEpoch != delivery.AccountEpoch || projection.MembershipGeneration != delivery.MembershipGeneration ||
		projection.Status != StatusActive {
		return true, false
	}
	tagSessionID := BrowserTagSessionID(delivery.VIBESUserID, delivery.VIBESSessionID)
	if session, exists := s.sessions[tagSessionID]; exists {
		if session.revoked || session.vibesUserID != delivery.VIBESUserID || session.vibesSessionID != delivery.VIBESSessionID ||
			session.accountEpoch != delivery.AccountEpoch || session.sessionWorkspaceGeneration > delivery.SessionWorkspaceGeneration {
			return true, false
		}
	}
	return false, true
}

func (s *MemoryStore) applyMemorySessionWorkspaceFence(delivery SessionWorkspaceSupersededDelivery) {
	tagSessionID := BrowserTagSessionID(delivery.VIBESUserID, delivery.VIBESSessionID)
	if session, exists := s.sessions[tagSessionID]; exists {
		session.sessionWorkspaceGeneration = delivery.SessionWorkspaceGeneration
		s.sessions[tagSessionID] = session
		for key := range s.grants {
			if key.tagSessionID == tagSessionID {
				delete(s.grants, key)
			}
		}
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
	if supersession, exists := s.sessionWorkspaceStates[sessionWorkspaceKey{userID: grant.VIBESUserID, sessionID: grant.VIBESSessionID}]; exists &&
		(supersession.integrity != integrityHealthy || supersession.accountEpoch != grant.AccountEpoch ||
			supersession.generation != grant.SessionWorkspaceGeneration || supersession.workspaceID != grant.WorkspaceID) {
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
	if grant.Continuous && !sessionExists {
		return ErrGrantDenied
	}
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
		if grant.SessionWorkspaceGeneration < session.sessionWorkspaceGeneration ||
			(grant.Continuous && grant.SessionWorkspaceGeneration != session.sessionWorkspaceGeneration) {
			return ErrGrantDenied
		}
		if !grant.Continuous && grant.SessionWorkspaceGeneration == session.sessionWorkspaceGeneration {
			for key := range s.grants {
				if key.tagSessionID == grant.TagSessionID && key.workspaceID != grant.WorkspaceID {
					return ErrGrantDenied
				}
			}
		}
		if grant.SessionWorkspaceGeneration > session.sessionWorkspaceGeneration {
			for key := range s.grants {
				if key.tagSessionID == grant.TagSessionID {
					delete(s.grants, key)
				}
			}
			session.sessionWorkspaceGeneration = grant.SessionWorkspaceGeneration
		}
		if !grant.Continuous && grant.SessionExpiresAt.Before(session.expiresAt) {
			session.expiresAt = grant.SessionExpiresAt
		}
	} else {
		session = memorySession{
			vibesSessionID:             grant.VIBESSessionID,
			vibesUserID:                grant.VIBESUserID,
			accountEpoch:               grant.AccountEpoch,
			sessionWorkspaceGeneration: grant.SessionWorkspaceGeneration,
			expiresAt:                  grant.SessionExpiresAt,
		}
	}
	grantKey := workspaceGrantKey{tagSessionID: grant.TagSessionID, workspaceID: grant.WorkspaceID}
	existingGrant, grantExists := s.grants[grantKey]
	if grant.Continuous && !grantExists {
		return ErrGrantDenied
	}
	if grantExists && existingGrant.revoked {
		return ErrGrantDenied
	}
	if grantExists && !grant.Continuous && existingGrant.membershipGeneration != grant.MembershipGeneration {
		return ErrGrantDenied
	}
	grantExpiry := grant.GrantExpiresAt
	if grant.Continuous && grantExists {
		grantExpiry = existingGrant.expiresAt
	}
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
			TagSessionID:               request.TagSessionID,
			VIBESSessionID:             session.vibesSessionID,
			VIBESUserID:                session.vibesUserID,
			WorkspaceID:                request.WorkspaceID,
			AccountEpoch:               session.accountEpoch,
			SessionWorkspaceGeneration: session.sessionWorkspaceGeneration,
			MembershipGeneration:       grant.membershipGeneration,
			AuthorityVersion:           grant.authorityVersion,
			SessionExpiresAt:           session.expiresAt,
			GrantExpiresAt:             grant.expiresAt,
		},
	}, nil
}
