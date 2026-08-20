package tagaccess

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *postgresStore) applySessionWorkspaceSupersession(ctx context.Context, delivery SessionWorkspaceSupersededDelivery, digest [sha256.Size]byte) (ApplyResult, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockIdentity(ctx, tx, delivery.VIBESUserID); err != nil {
		return "", err
	}
	if err := lockWorkspace(ctx, tx, delivery.NewWorkspaceID); err != nil {
		return "", err
	}
	tagSessionID := BrowserTagSessionID(delivery.VIBESUserID, delivery.VIBESSessionID)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "tag-access-session:"+tagSessionID); err != nil {
		return "", err
	}
	current, exists, err := loadSessionWorkspaceStateForUpdate(ctx, tx, delivery.VIBESUserID, delivery.VIBESSessionID)
	if err != nil {
		return "", err
	}
	identityConflict, err := sessionWorkspaceDeliveryIdentityConflict(ctx, tx, delivery)
	if err != nil {
		return "", err
	}
	if identityConflict {
		if !exists {
			current = sessionWorkspaceRecord{
				userID: delivery.VIBESUserID, sessionID: delivery.VIBESSessionID,
				accountEpoch: delivery.AccountEpoch, generation: 1,
			}
		}
		current = blockSessionWorkspaceAdvance(current, digest, true)
		if delivery.SessionWorkspaceGeneration > current.observedGeneration {
			current.observedGeneration = delivery.SessionWorkspaceGeneration
		}
		if exists {
			err = updateSessionWorkspaceState(ctx, tx, current)
		} else {
			err = insertSessionWorkspaceState(ctx, tx, current)
		}
		if err != nil {
			return "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return ApplyConflict, nil
	}
	observedDigest, observed, err := loadSessionWorkspaceDelivery(ctx, tx, delivery.VIBESUserID, delivery.VIBESSessionID, delivery.SessionWorkspaceGeneration)
	if err != nil {
		return "", err
	}
	next, result, changed, candidate := evolveSessionWorkspace(current, exists, delivery, digest, observedDigest, observed)
	if !observed {
		if err := insertSessionWorkspaceDelivery(ctx, tx, delivery, digest); err != nil {
			return "", err
		}
	}
	if result == ApplyDuplicate || result == ApplyStale {
		permanent, err := sessionWorkspacePostgresIdentityPermanentlyRestricted(ctx, tx, delivery)
		if err != nil {
			return "", err
		}
		if permanent {
			next = blockSessionWorkspaceAdvance(next, digest, true)
			result, changed = ApplyConflict, true
		}
	}
	if candidate {
		permanent, ready, err := sessionWorkspacePostgresDependencies(ctx, tx, delivery, current, exists, tagSessionID)
		if err != nil {
			return "", err
		}
		switch {
		case permanent:
			next = blockSessionWorkspaceAdvance(next, digest, true)
			result, changed = ApplyConflict, true
		case !ready:
			next = blockSessionWorkspaceAdvance(next, digest, false)
			result, changed = ApplyGap, true
		default:
			if _, err := tx.Exec(ctx, `DELETE FROM tag_session_workspace_grant WHERE tag_session_id = $1`, tagSessionID); err != nil {
				return "", err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE tag_access_session
				SET session_workspace_generation = $2, updated_at = now()
				WHERE tag_session_id = $1
			`, tagSessionID, int64(delivery.SessionWorkspaceGeneration)); err != nil {
				return "", err
			}
			next = completeSessionWorkspaceAdvance(next, delivery)
			result, changed = ApplyApplied, true
		}
	}
	if changed {
		if exists {
			err = updateSessionWorkspaceState(ctx, tx, next)
		} else {
			err = insertSessionWorkspaceState(ctx, tx, next)
		}
		if err != nil {
			return "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return result, nil
}

func sessionWorkspacePostgresIdentityPermanentlyRestricted(ctx context.Context, tx pgx.Tx, delivery SessionWorkspaceSupersededDelivery) (bool, error) {
	identity, exists, err := loadIdentityForUpdate(ctx, tx, delivery.VIBESUserID)
	if err != nil {
		return false, err
	}
	if exists && (identity.accountEpoch != 0 && identity.accountEpoch != delivery.AccountEpoch || identity.revokedThrough >= delivery.AccountEpoch) {
		return true, nil
	}
	return identitySessionRevoked(ctx, tx, delivery.VIBESUserID, delivery.VIBESSessionID)
}

func sessionWorkspaceDeliveryIdentityConflict(ctx context.Context, tx pgx.Tx, delivery SessionWorkspaceSupersededDelivery) (bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT vibes_user_id, vibes_session_id, session_workspace_generation
		FROM tag_access_session_workspace_delivery
		WHERE event_id = $1 OR delivery_id = $2 OR idempotency_key = $3
	`, delivery.EventID, delivery.DeliveryID, delivery.IdempotencyKey)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var userID, sessionID string
		var generation int64
		if err := rows.Scan(&userID, &sessionID, &generation); err != nil {
			return false, err
		}
		if userID != delivery.VIBESUserID || sessionID != delivery.VIBESSessionID || generation != int64(delivery.SessionWorkspaceGeneration) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func loadSessionWorkspaceStateForUpdate(ctx context.Context, tx pgx.Tx, userID, sessionID string) (sessionWorkspaceRecord, bool, error) {
	var accountEpoch, generation, observedGeneration int64
	var workspaceID, integrity string
	var blockedDigest []byte
	err := tx.QueryRow(ctx, `
		SELECT account_epoch, session_workspace_generation, observed_session_workspace_generation,
		       current_workspace_id, integrity_state, blocked_payload_digest
		FROM tag_access_session_workspace_state
		WHERE vibes_user_id = $1 AND vibes_session_id = $2
		FOR UPDATE
	`, userID, sessionID).Scan(&accountEpoch, &generation, &observedGeneration, &workspaceID, &integrity, &blockedDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return sessionWorkspaceRecord{}, false, nil
	}
	if err != nil {
		return sessionWorkspaceRecord{}, false, err
	}
	if accountEpoch <= 0 || generation <= 0 || observedGeneration < generation || (workspaceID != "" && !validStableID(workspaceID)) {
		return sessionWorkspaceRecord{}, false, errors.New("invalid persisted Tag session Workspace state")
	}
	var digest [sha256.Size]byte
	if len(blockedDigest) != 0 {
		if len(blockedDigest) != sha256.Size {
			return sessionWorkspaceRecord{}, false, errors.New("invalid Tag session Workspace blocked digest")
		}
		copy(digest[:], blockedDigest)
	}
	return sessionWorkspaceRecord{
		userID: userID, sessionID: sessionID, accountEpoch: uint64(accountEpoch), generation: uint64(generation),
		observedGeneration: uint64(observedGeneration), workspaceID: workspaceID,
		integrity: projectionIntegrity(integrity), blockedDigest: digest,
	}, true, nil
}

func loadSessionWorkspaceDelivery(ctx context.Context, tx pgx.Tx, userID, sessionID string, generation uint64) ([sha256.Size]byte, bool, error) {
	var persisted []byte
	err := tx.QueryRow(ctx, `
		SELECT payload_digest FROM tag_access_session_workspace_delivery
		WHERE vibes_user_id = $1 AND vibes_session_id = $2 AND session_workspace_generation = $3
	`, userID, sessionID, int64(generation)).Scan(&persisted)
	if errors.Is(err, pgx.ErrNoRows) {
		return [sha256.Size]byte{}, false, nil
	}
	if err != nil {
		return [sha256.Size]byte{}, false, err
	}
	if len(persisted) != sha256.Size {
		return [sha256.Size]byte{}, false, errors.New("invalid Tag session Workspace delivery digest")
	}
	var digest [sha256.Size]byte
	copy(digest[:], persisted)
	return digest, true, nil
}

func insertSessionWorkspaceDelivery(ctx context.Context, tx pgx.Tx, delivery SessionWorkspaceSupersededDelivery, digest [sha256.Size]byte) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tag_access_session_workspace_delivery (
			vibes_user_id, vibes_session_id, session_workspace_generation,
			event_id, delivery_id, correlation_id, idempotency_key,
			previous_workspace_id, new_workspace_id, account_epoch,
			identity_restriction_version, authority_version, membership_generation, payload_digest
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, delivery.VIBESUserID, delivery.VIBESSessionID, int64(delivery.SessionWorkspaceGeneration),
		delivery.EventID, delivery.DeliveryID, delivery.CorrelationID, delivery.IdempotencyKey,
		delivery.PreviousWorkspaceID, delivery.NewWorkspaceID, int64(delivery.AccountEpoch),
		int64(delivery.IdentityRestrictionVersion), int64(delivery.AuthorityVersion), int64(delivery.MembershipGeneration), digest[:])
	return err
}

func sessionWorkspacePostgresDependencies(ctx context.Context, tx pgx.Tx, delivery SessionWorkspaceSupersededDelivery, current sessionWorkspaceRecord, exists bool, tagSessionID string) (permanent, ready bool, err error) {
	identity, identityExists, err := loadIdentityForUpdate(ctx, tx, delivery.VIBESUserID)
	if err != nil {
		return false, false, err
	}
	if delivery.IdentityRestrictionVersion > 0 && (!identityExists || identity.version < delivery.IdentityRestrictionVersion) {
		return false, false, nil
	}
	if identityExists {
		if identity.integrity != integrityHealthy {
			return false, false, nil
		}
		if identity.accountEpoch != 0 && identity.accountEpoch != delivery.AccountEpoch || identity.revokedThrough >= delivery.AccountEpoch {
			return true, false, nil
		}
	}
	revoked, err := identitySessionRevoked(ctx, tx, delivery.VIBESUserID, delivery.VIBESSessionID)
	if err != nil {
		return false, false, err
	}
	if revoked {
		return true, false, nil
	}
	workspace, workspaceExists, err := loadWorkspaceForUpdate(ctx, tx, delivery.NewWorkspaceID)
	if err != nil {
		return false, false, err
	}
	projection, projectionExists, err := loadProjectionForUpdate(ctx, tx, delivery.VIBESUserID, delivery.NewWorkspaceID)
	if err != nil {
		return false, false, err
	}
	if !workspaceExists || !projectionExists || workspace.integrity != integrityHealthy ||
		workspace.authorityVersion < delivery.AuthorityVersion || projection.AuthorityVersion < delivery.AuthorityVersion {
		return false, false, nil
	}
	if workspace.authorityVersion != delivery.AuthorityVersion || projection.AuthorityVersion != delivery.AuthorityVersion ||
		projection.AccountEpoch != delivery.AccountEpoch || projection.MembershipGeneration != delivery.MembershipGeneration ||
		projection.Status != StatusActive {
		return true, false, nil
	}
	var sessionUserID, sessionID string
	var sessionEpoch, sessionGeneration int64
	var revokedAt pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
		SELECT vibes_user_id, vibes_session_id, account_epoch, session_workspace_generation, revoked_at
		FROM tag_access_session WHERE tag_session_id = $1 FOR UPDATE
	`, tagSessionID).Scan(&sessionUserID, &sessionID, &sessionEpoch, &sessionGeneration, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	baselineGeneration := uint64(1)
	if exists {
		baselineGeneration = current.generation
	}
	if revokedAt.Valid || sessionUserID != delivery.VIBESUserID || sessionID != delivery.VIBESSessionID ||
		sessionEpoch != int64(delivery.AccountEpoch) || sessionGeneration != int64(baselineGeneration) {
		return true, false, nil
	}
	var conflictingGrant bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM tag_session_workspace_grant
			WHERE tag_session_id = $1 AND vibes_workspace_id <> $2 AND revoked_at IS NULL
		)
	`, tagSessionID, delivery.PreviousWorkspaceID).Scan(&conflictingGrant); err != nil {
		return false, false, err
	}
	if conflictingGrant {
		return true, false, nil
	}
	return false, true, nil
}

func insertSessionWorkspaceState(ctx context.Context, tx pgx.Tx, record sessionWorkspaceRecord) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tag_access_session_workspace_state (
			vibes_user_id, vibes_session_id, account_epoch,
			session_workspace_generation, observed_session_workspace_generation,
			current_workspace_id, integrity_state, blocked_payload_digest
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, record.userID, record.sessionID, int64(record.accountEpoch), int64(record.generation),
		int64(record.observedGeneration), record.workspaceID, record.integrity,
		optionalDigest(record.blockedDigest != [sha256.Size]byte{}, record.blockedDigest))
	return err
}

func updateSessionWorkspaceState(ctx context.Context, tx pgx.Tx, record sessionWorkspaceRecord) error {
	_, err := tx.Exec(ctx, `
		UPDATE tag_access_session_workspace_state
		SET account_epoch = $3, session_workspace_generation = $4,
		    observed_session_workspace_generation = $5, current_workspace_id = $6,
		    integrity_state = $7, blocked_payload_digest = $8, updated_at = now()
		WHERE vibes_user_id = $1 AND vibes_session_id = $2
	`, record.userID, record.sessionID, int64(record.accountEpoch), int64(record.generation),
		int64(record.observedGeneration), record.workspaceID, record.integrity,
		optionalDigest(record.blockedDigest != [sha256.Size]byte{}, record.blockedDigest))
	return err
}
