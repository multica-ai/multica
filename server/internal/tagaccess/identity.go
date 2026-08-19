package tagaccess

type IdentityRestrictionKind string

const (
	IdentityRestrictionSessionLogout IdentityRestrictionKind = "session_logged_out"
	IdentityRestrictionAccountBan    IdentityRestrictionKind = "account_banned"
)

type IdentityRestrictionDelivery struct {
	Kind                       IdentityRestrictionKind `json:"kind"`
	EventID                    string                  `json:"eventId"`
	CorrelationID              string                  `json:"correlationId"`
	IdempotencyKey             string                  `json:"idempotencyKey"`
	VIBESUserID                string                  `json:"vibesUserId"`
	VIBESSessionID             string                  `json:"vibesSessionId,omitempty"`
	AccountEpoch               uint64                  `json:"accountEpoch"`
	IdentityRestrictionVersion uint64                  `json:"identityRestrictionVersion"`
	CloseTarget                ConnectionCloseTarget   `json:"closeTarget"`
}

type identityRecord struct {
	userID          string
	version         uint64
	observedVersion uint64
	accountEpoch    uint64
	revokedThrough  uint64
	integrity       projectionIntegrity
	blockedDigest   [32]byte
}

func evolveIdentity(current identityRecord, exists bool, delivery IdentityRestrictionDelivery, digest, observedDigest [32]byte, observed bool) (identityRecord, ApplyResult, bool, bool) {
	version := delivery.IdentityRestrictionVersion
	if !exists {
		current = identityRecord{userID: delivery.VIBESUserID}
	}
	if observed && observedDigest != digest {
		current.integrity = integrityConflict
		current.blockedDigest = digest
		if version > current.observedVersion {
			current.observedVersion = version
		}
		return current, ApplyConflict, true, false
	}
	if current.integrity == integrityConflict {
		return current, ApplyConflict, false, false
	}
	if version < current.version {
		return current, ApplyStale, false, false
	}
	if version == current.version {
		if observed {
			return current, ApplyDuplicate, false, false
		}
		current.integrity = integrityConflict
		current.blockedDigest = digest
		return current, ApplyConflict, true, false
	}
	if version != current.version+1 {
		if version > current.observedVersion {
			current.observedVersion = version
		}
		current.integrity = integrityGap
		current.blockedDigest = digest
		return current, ApplyGap, true, false
	}
	if current.accountEpoch != 0 && delivery.AccountEpoch < current.accountEpoch {
		current.integrity = integrityConflict
		current.blockedDigest = digest
		return current, ApplyConflict, true, false
	}
	if delivery.Kind == IdentityRestrictionAccountBan && current.accountEpoch != 0 && delivery.AccountEpoch <= current.accountEpoch {
		current.integrity = integrityConflict
		current.blockedDigest = digest
		return current, ApplyConflict, true, false
	}
	current.version = version
	if version > current.observedVersion {
		current.observedVersion = version
	}
	current.accountEpoch = delivery.AccountEpoch
	if delivery.Kind == IdentityRestrictionAccountBan {
		current.revokedThrough = delivery.AccountEpoch
	}
	current.blockedDigest = [32]byte{}
	if current.observedVersion > current.version {
		current.integrity = integrityGap
		return current, ApplyApplied, true, true
	}
	current.integrity = integrityHealthy
	return current, ApplyApplied, true, true
}

func validIdentityRestrictionDelivery(delivery IdentityRestrictionDelivery) bool {
	if !validStableID(delivery.EventID) || !validStableID(delivery.CorrelationID) || !validStableID(delivery.IdempotencyKey) ||
		!validStableID(delivery.VIBESUserID) || delivery.AccountEpoch == 0 || delivery.AccountEpoch > maxDatabaseCounter ||
		delivery.IdentityRestrictionVersion == 0 || delivery.IdentityRestrictionVersion > maxDatabaseCounter {
		return false
	}
	target := delivery.CloseTarget
	if target.WorkspaceID != "" || target.VIBESUserID != delivery.VIBESUserID {
		return false
	}
	switch delivery.Kind {
	case IdentityRestrictionSessionLogout:
		return validStableID(delivery.VIBESSessionID) && target.Scope == ConnectionCloseSession && target.VIBESSessionID == delivery.VIBESSessionID
	case IdentityRestrictionAccountBan:
		return delivery.VIBESSessionID == "" && target.Scope == ConnectionCloseAccount && target.VIBESSessionID == ""
	default:
		return false
	}
}
