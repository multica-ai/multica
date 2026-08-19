package tagaccess

type workspaceRecord struct {
	// This is delivery-integrity evidence for the VIBES global authority stream,
	// not a second Workspace or Membership authority record.
	workspaceID      string
	authorityVersion uint64
	observedVersion  uint64
	integrity        projectionIntegrity
	blockedDigest    [32]byte
}

func evolveWorkspace(current workspaceRecord, exists bool, delivery ProjectionDelivery, digest [32]byte, observedDigest [32]byte, observed bool) (workspaceRecord, ApplyResult, bool, bool) {
	version := delivery.Projections[0].AuthorityVersion
	workspaceID := delivery.Projections[0].WorkspaceID
	if !exists {
		current = workspaceRecord{workspaceID: workspaceID}
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
		if delivery.Kind != DeliveryReconcile || version <= current.observedVersion {
			return current, ApplyConflict, false, false
		}
		return reconciledWorkspace(current, delivery), ApplyApplied, true, true
	}

	switch delivery.Kind {
	case DeliverySnapshot, DeliveryReconcile:
		if version < current.authorityVersion {
			return current, ApplyStale, false, false
		}
		if version < current.observedVersion {
			return current, ApplyGap, false, false
		}
		if version == current.authorityVersion {
			if observed {
				return current, ApplyDuplicate, false, false
			}
			current.integrity = integrityConflict
			current.blockedDigest = digest
			return current, ApplyConflict, true, false
		}
		return reconciledWorkspace(current, delivery), ApplyApplied, true, true
	case DeliveryIncremental:
		if version < current.authorityVersion {
			return current, ApplyStale, false, false
		}
		if version == current.authorityVersion {
			if observed {
				return current, ApplyDuplicate, false, false
			}
			current.integrity = integrityConflict
			current.blockedDigest = digest
			return current, ApplyConflict, true, false
		}
		if delivery.BaselineAuthorityVersion != current.authorityVersion {
			if version > current.observedVersion {
				current.observedVersion = version
			}
			current.integrity = integrityGap
			current.blockedDigest = digest
			return current, ApplyGap, true, false
		}
		current.authorityVersion = version
		if version > current.observedVersion {
			current.observedVersion = version
		}
		current.blockedDigest = [32]byte{}
		if current.observedVersion > current.authorityVersion {
			current.integrity = integrityGap
			return current, ApplyGap, true, true
		}
		current.integrity = integrityHealthy
		return current, ApplyApplied, true, true
	default:
		return current, ApplyConflict, false, false
	}
}

func reconciledWorkspace(current workspaceRecord, delivery ProjectionDelivery) workspaceRecord {
	version := delivery.Projections[0].AuthorityVersion
	current.workspaceID = delivery.Projections[0].WorkspaceID
	current.authorityVersion = version
	if version > current.observedVersion {
		current.observedVersion = version
	}
	current.integrity = integrityHealthy
	current.blockedDigest = [32]byte{}
	return current
}

func validProjectionTransition(current ProjectionEvent, exists bool, next ProjectionEvent) bool {
	if !exists {
		return true
	}
	if next.AuthorityVersion < current.AuthorityVersion || next.AccountEpoch < current.AccountEpoch || next.MembershipGeneration < current.MembershipGeneration {
		return false
	}
	return current.Status != StatusRemoved || next.Status != StatusActive || next.MembershipGeneration > current.MembershipGeneration
}

func omittedProjection(current ProjectionEvent, snapshot ProjectionEvent) ProjectionEvent {
	current.EventID = snapshot.EventID
	current.Status = StatusRemoved
	current.AuthorityVersion = snapshot.AuthorityVersion
	return current
}
