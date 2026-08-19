package tagaccess

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (s *postgresStore) applyIdentityRestriction(ctx context.Context, delivery IdentityRestrictionDelivery, digest [32]byte) (ApplyResult, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockIdentity(ctx, tx, delivery.VIBESUserID); err != nil {
		return "", err
	}
	current, exists, err := loadIdentityForUpdate(ctx, tx, delivery.VIBESUserID)
	if err != nil {
		return "", err
	}
	observedDigest, observed, err := loadIdentityDelivery(ctx, tx, delivery.VIBESUserID, delivery.IdentityRestrictionVersion)
	if err != nil {
		return "", err
	}
	next, result, changed, apply := evolveIdentity(current, exists, delivery, digest, observedDigest, observed)
	if !observed {
		if err := insertIdentityDelivery(ctx, tx, delivery, digest); err != nil {
			return "", err
		}
	}
	if changed {
		if exists {
			err = updateIdentityState(ctx, tx, next)
		} else {
			err = insertIdentityState(ctx, tx, next)
		}
		if err != nil {
			return "", err
		}
	}
	if apply {
		if err := revokeIdentitySessions(ctx, tx, delivery); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return result, nil
}

func loadIdentityForUpdate(ctx context.Context, tx pgx.Tx, userID string) (identityRecord, bool, error) {
	var version, observed, accountEpoch, revokedThrough int64
	var integrity string
	var blockedDigest []byte
	err := tx.QueryRow(ctx, `
		SELECT identity_restriction_version, observed_identity_restriction_version,
		       account_epoch, revoked_through_account_epoch, integrity_state, blocked_payload_digest
		FROM tag_access_identity_restriction_state
		WHERE vibes_user_id = $1
		FOR UPDATE
	`, userID).Scan(&version, &observed, &accountEpoch, &revokedThrough, &integrity, &blockedDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return identityRecord{}, false, nil
	}
	if err != nil {
		return identityRecord{}, false, err
	}
	if version < 0 || observed < version || accountEpoch < 0 || revokedThrough < 0 {
		return identityRecord{}, false, errors.New("invalid persisted Tag identity restriction state")
	}
	var digest [32]byte
	if len(blockedDigest) != 0 {
		if len(blockedDigest) != sha256.Size {
			return identityRecord{}, false, errors.New("invalid Tag identity restriction blocked digest")
		}
		copy(digest[:], blockedDigest)
	}
	return identityRecord{
		userID: userID, version: uint64(version), observedVersion: uint64(observed), accountEpoch: uint64(accountEpoch),
		revokedThrough: uint64(revokedThrough), integrity: projectionIntegrity(integrity), blockedDigest: digest,
	}, true, nil
}

func loadIdentityDelivery(ctx context.Context, tx pgx.Tx, userID string, version uint64) ([32]byte, bool, error) {
	var persisted []byte
	err := tx.QueryRow(ctx, `
		SELECT payload_digest
		FROM tag_access_identity_restriction_delivery
		WHERE vibes_user_id = $1 AND identity_restriction_version = $2
	`, userID, int64(version)).Scan(&persisted)
	if errors.Is(err, pgx.ErrNoRows) {
		return [32]byte{}, false, nil
	}
	if err != nil {
		return [32]byte{}, false, err
	}
	if len(persisted) != sha256.Size {
		return [32]byte{}, false, errors.New("invalid Tag identity restriction delivery digest")
	}
	var digest [32]byte
	copy(digest[:], persisted)
	return digest, true, nil
}

func insertIdentityDelivery(ctx context.Context, tx pgx.Tx, delivery IdentityRestrictionDelivery, digest [32]byte) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tag_access_identity_restriction_delivery (
			vibes_user_id, identity_restriction_version, restriction_kind,
			vibes_session_id, account_epoch, event_id, correlation_id,
			idempotency_key, payload_digest
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, delivery.VIBESUserID, int64(delivery.IdentityRestrictionVersion), delivery.Kind,
		nullableText(delivery.VIBESSessionID), int64(delivery.AccountEpoch), delivery.EventID,
		delivery.CorrelationID, delivery.IdempotencyKey, digest[:])
	return err
}

func insertIdentityState(ctx context.Context, tx pgx.Tx, record identityRecord) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tag_access_identity_restriction_state (
			vibes_user_id, identity_restriction_version,
			observed_identity_restriction_version, account_epoch, revoked_through_account_epoch,
			integrity_state, blocked_payload_digest
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, record.userID, int64(record.version), int64(record.observedVersion), int64(record.accountEpoch),
		int64(record.revokedThrough), record.integrity, optionalDigest(record.blockedDigest != [32]byte{}, record.blockedDigest))
	return err
}

func updateIdentityState(ctx context.Context, tx pgx.Tx, record identityRecord) error {
	_, err := tx.Exec(ctx, `
		UPDATE tag_access_identity_restriction_state
		SET identity_restriction_version = $2,
		    observed_identity_restriction_version = $3,
		    account_epoch = $4, revoked_through_account_epoch = $5,
		    integrity_state = $6, blocked_payload_digest = $7, updated_at = now()
		WHERE vibes_user_id = $1
	`, record.userID, int64(record.version), int64(record.observedVersion), int64(record.accountEpoch),
		int64(record.revokedThrough), record.integrity, optionalDigest(record.blockedDigest != [32]byte{}, record.blockedDigest))
	return err
}

func revokeIdentitySessions(ctx context.Context, tx pgx.Tx, delivery IdentityRestrictionDelivery) error {
	sessionFilter := ""
	arguments := []any{delivery.VIBESUserID}
	if delivery.Kind == IdentityRestrictionSessionLogout {
		sessionFilter = " AND vibes_session_id = $2"
		arguments = append(arguments, delivery.VIBESSessionID)
	}
	_, err := tx.Exec(ctx, `
		WITH revoked_sessions AS (
			UPDATE tag_access_session
			SET revoked_at = COALESCE(revoked_at, now()), updated_at = now()
			WHERE vibes_user_id = $1`+sessionFilter+`
			RETURNING tag_session_id
		)
		UPDATE tag_session_workspace_grant
		SET revoked_at = COALESCE(revoked_at, now()), updated_at = now()
		WHERE tag_session_id IN (SELECT tag_session_id FROM revoked_sessions)
	`, arguments...)
	return err
}

func identitySessionRevoked(ctx context.Context, tx pgx.Tx, userID, sessionID string) (bool, error) {
	var revoked bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM tag_access_identity_restriction_delivery
			WHERE vibes_user_id = $1 AND restriction_kind = 'session_logged_out'
			  AND vibes_session_id = $2
		)
	`, userID, sessionID).Scan(&revoked)
	return revoked, err
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
