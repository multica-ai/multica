package tagaccess

import "crypto/sha256"

type SessionWorkspaceSupersessionKind string

const SessionWorkspaceSuperseded SessionWorkspaceSupersessionKind = "session_workspace_superseded"

type SessionWorkspaceSupersededDelivery struct {
	Kind                       SessionWorkspaceSupersessionKind `json:"kind"`
	EventID                    string                           `json:"eventId"`
	DeliveryID                 string                           `json:"deliveryId"`
	CorrelationID              string                           `json:"correlationId"`
	IdempotencyKey             string                           `json:"idempotencyKey"`
	VIBESUserID                string                           `json:"vibesUserId"`
	VIBESSessionID             string                           `json:"vibesSessionId"`
	PreviousWorkspaceID        string                           `json:"previousWorkspaceId"`
	NewWorkspaceID             string                           `json:"newWorkspaceId"`
	SessionWorkspaceGeneration uint64                           `json:"sessionWorkspaceGeneration"`
	AccountEpoch               uint64                           `json:"accountEpoch"`
	IdentityRestrictionVersion uint64                           `json:"identityRestrictionVersion"`
	AuthorityVersion           uint64                           `json:"authorityVersion"`
	MembershipGeneration       uint64                           `json:"membershipGeneration"`
	CloseTarget                ConnectionCloseTarget            `json:"closeTarget"`
}

type sessionWorkspaceKey struct {
	userID    string
	sessionID string
}

type sessionWorkspaceDeliveryKey struct {
	userID     string
	sessionID  string
	generation uint64
}

type sessionWorkspaceRecord struct {
	userID             string
	sessionID          string
	accountEpoch       uint64
	generation         uint64
	observedGeneration uint64
	workspaceID        string
	integrity          projectionIntegrity
	blockedDigest      [sha256.Size]byte
}

func validSessionWorkspaceSupersession(delivery SessionWorkspaceSupersededDelivery) bool {
	target := delivery.CloseTarget
	return delivery.Kind == SessionWorkspaceSuperseded &&
		validStableID(delivery.EventID) && validStableID(delivery.DeliveryID) &&
		validStableID(delivery.CorrelationID) && validStableID(delivery.IdempotencyKey) &&
		validStableID(delivery.VIBESUserID) && validStableID(delivery.VIBESSessionID) &&
		validStableID(delivery.PreviousWorkspaceID) && validStableID(delivery.NewWorkspaceID) &&
		delivery.PreviousWorkspaceID != delivery.NewWorkspaceID &&
		delivery.SessionWorkspaceGeneration >= 2 && delivery.SessionWorkspaceGeneration <= maxDatabaseCounter &&
		delivery.AccountEpoch > 0 && delivery.AccountEpoch <= maxDatabaseCounter &&
		delivery.IdentityRestrictionVersion <= maxDatabaseCounter &&
		delivery.AuthorityVersion > 0 && delivery.AuthorityVersion <= maxDatabaseCounter &&
		delivery.MembershipGeneration > 0 && delivery.MembershipGeneration <= maxDatabaseCounter &&
		target.Scope == ConnectionCloseSessionWorkspace && target.VIBESUserID == delivery.VIBESUserID &&
		target.VIBESSessionID == delivery.VIBESSessionID && target.WorkspaceID == delivery.PreviousWorkspaceID
}

func evolveSessionWorkspace(current sessionWorkspaceRecord, exists bool, delivery SessionWorkspaceSupersededDelivery, digest, observedDigest [sha256.Size]byte, observed bool) (sessionWorkspaceRecord, ApplyResult, bool, bool) {
	if !exists {
		current = sessionWorkspaceRecord{
			userID: delivery.VIBESUserID, sessionID: delivery.VIBESSessionID,
			accountEpoch: delivery.AccountEpoch, generation: 1,
			integrity: integrityHealthy,
		}
	}
	generation := delivery.SessionWorkspaceGeneration
	if observed && observedDigest != digest {
		current.integrity = integrityConflict
		current.blockedDigest = digest
		if generation > current.observedGeneration {
			current.observedGeneration = generation
		}
		return current, ApplyConflict, true, false
	}
	if current.integrity == integrityConflict {
		return current, ApplyConflict, false, false
	}
	if current.userID != delivery.VIBESUserID || current.sessionID != delivery.VIBESSessionID ||
		current.accountEpoch != delivery.AccountEpoch {
		current.integrity = integrityConflict
		current.blockedDigest = digest
		return current, ApplyConflict, true, false
	}
	if generation < current.generation {
		if observed {
			return current, ApplyStale, false, false
		}
		current.integrity = integrityConflict
		current.blockedDigest = digest
		return current, ApplyConflict, true, false
	}
	if generation == current.generation {
		if observed {
			return current, ApplyDuplicate, false, false
		}
		current.integrity = integrityConflict
		current.blockedDigest = digest
		return current, ApplyConflict, true, false
	}
	if generation != current.generation+1 {
		if generation > current.observedGeneration {
			current.observedGeneration = generation
		}
		current.integrity = integrityGap
		current.blockedDigest = digest
		return current, ApplyGap, true, false
	}
	if current.workspaceID != "" && current.workspaceID != delivery.PreviousWorkspaceID {
		current.integrity = integrityConflict
		current.blockedDigest = digest
		return current, ApplyConflict, true, false
	}
	if generation > current.observedGeneration {
		current.observedGeneration = generation
	}
	return current, ApplyApplied, false, true
}

func completeSessionWorkspaceAdvance(current sessionWorkspaceRecord, delivery SessionWorkspaceSupersededDelivery) sessionWorkspaceRecord {
	current.accountEpoch = delivery.AccountEpoch
	current.generation = delivery.SessionWorkspaceGeneration
	current.workspaceID = delivery.NewWorkspaceID
	current.blockedDigest = [sha256.Size]byte{}
	if current.observedGeneration > current.generation {
		current.integrity = integrityGap
	} else {
		current.integrity = integrityHealthy
	}
	return current
}

func blockSessionWorkspaceAdvance(current sessionWorkspaceRecord, digest [sha256.Size]byte, permanent bool) sessionWorkspaceRecord {
	current.blockedDigest = digest
	if permanent {
		current.integrity = integrityConflict
	} else {
		current.integrity = integrityGap
	}
	return current
}
