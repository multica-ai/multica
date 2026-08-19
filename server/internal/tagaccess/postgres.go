package tagaccess

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type postgresDB interface {
	Begin(context.Context) (pgx.Tx, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresStore struct {
	db postgresDB
}

// NewPostgresStore creates the production adapter. Both pgxpool.Pool and
// pgx.Conn satisfy its transaction and query interface.
func NewPostgresStore(db postgresDB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) ApplyProjection(ctx context.Context, delivery ProjectionDelivery, digest [32]byte) (ApplyResult, error) {
	first := delivery.Projections[0]
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockWorkspace(ctx, tx, first.WorkspaceID); err != nil {
		return "", err
	}
	current, exists, err := loadWorkspaceForUpdate(ctx, tx, first.WorkspaceID)
	if err != nil {
		return "", err
	}
	observedDigest, observed, err := loadProjectionDelivery(ctx, tx, first.WorkspaceID, first.AuthorityVersion)
	if err != nil {
		return "", err
	}
	next, result, changed, applyMembers := evolveWorkspace(current, exists, delivery, digest, observedDigest, observed)
	if applyMembers {
		for _, projection := range delivery.Projections {
			persisted, projectionExists, err := loadProjectionForUpdate(ctx, tx, projection.VIBESUserID, projection.WorkspaceID)
			if err != nil {
				return "", err
			}
			if !validProjectionTransition(persisted, projectionExists, projection) {
				next.integrity = integrityConflict
				next.blockedDigest = digest
				result = ApplyConflict
				applyMembers = false
				changed = true
				break
			}
		}
	}
	if !observed {
		if err := insertProjectionDelivery(ctx, tx, delivery, digest); err != nil {
			return "", err
		}
	}
	if changed {
		if exists {
			err = updateWorkspace(ctx, tx, next)
		} else {
			err = insertWorkspace(ctx, tx, next)
		}
		if err != nil {
			return "", err
		}
	}
	if applyMembers {
		if delivery.Kind != DeliveryIncremental {
			userIDs := make([]string, 0, len(delivery.Projections))
			for _, projection := range delivery.Projections {
				userIDs = append(userIDs, projection.VIBESUserID)
			}
			if _, err := tx.Exec(ctx, `
				UPDATE tag_access_projection
				SET status = 'removed', authority_version = $2, last_event_id = $3,
				    last_payload_digest = $4, updated_at = now()
				WHERE vibes_workspace_id = $1 AND NOT (vibes_user_id = ANY($5::text[]))
			`, first.WorkspaceID, int64(first.AuthorityVersion), first.EventID, digest[:], userIDs); err != nil {
				return "", err
			}
		}
		for _, projection := range delivery.Projections {
			if err := upsertProjection(ctx, tx, projection, digest); err != nil {
				return "", err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return result, nil
}

func loadProjectionDelivery(ctx context.Context, tx pgx.Tx, workspaceID string, version uint64) ([32]byte, bool, error) {
	var persisted []byte
	err := tx.QueryRow(ctx, `
		SELECT payload_digest
		FROM tag_access_projection_delivery
		WHERE vibes_workspace_id = $1 AND authority_version = $2
	`, workspaceID, int64(version)).Scan(&persisted)
	if errors.Is(err, pgx.ErrNoRows) {
		return [32]byte{}, false, nil
	}
	if err != nil {
		return [32]byte{}, false, err
	}
	if len(persisted) != sha256.Size {
		return [32]byte{}, false, errors.New("invalid Tag access projection delivery digest")
	}
	var digest [32]byte
	copy(digest[:], persisted)
	return digest, true, nil
}

func insertProjectionDelivery(ctx context.Context, tx pgx.Tx, delivery ProjectionDelivery, digest [32]byte) error {
	first := delivery.Projections[0]
	_, err := tx.Exec(ctx, `
		INSERT INTO tag_access_projection_delivery (
			vibes_workspace_id, authority_version, delivery_kind,
			authority_assertion_id, payload_digest
		) VALUES ($1, $2, $3, $4, $5)
	`, first.WorkspaceID, int64(first.AuthorityVersion), delivery.Kind, delivery.AuthorityAssertionID, digest[:])
	return err
}

func (s *PostgresStore) CreateGrant(ctx context.Context, grant SessionGrant, now time.Time) error {
	if !grant.SessionExpiresAt.After(now) || !grant.GrantExpiresAt.After(now) {
		return ErrGrantDenied
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockWorkspace(ctx, tx, grant.WorkspaceID); err != nil {
		return err
	}
	workspace, workspaceExists, err := loadWorkspaceForUpdate(ctx, tx, grant.WorkspaceID)
	if err != nil {
		return err
	}
	projection, exists, err := loadProjectionForUpdate(ctx, tx, grant.VIBESUserID, grant.WorkspaceID)
	if err != nil {
		return err
	}
	if !workspaceExists || workspace.integrity != integrityHealthy || !exists || projection.Status != StatusActive ||
		projection.AccountEpoch != grant.AccountEpoch ||
		projection.MembershipGeneration != grant.MembershipGeneration ||
		projection.AuthorityVersion != grant.AuthorityVersion {
		return ErrGrantDenied
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "tag-access-session:"+grant.TagSessionID); err != nil {
		return err
	}

	effectiveSessionExpiry, err := upsertBoundSession(ctx, tx, grant)
	if err != nil {
		return err
	}
	if grant.GrantExpiresAt.After(effectiveSessionExpiry) {
		grant.GrantExpiresAt = effectiveSessionExpiry
	}
	if err := upsertBoundWorkspaceGrant(ctx, tx, grant); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) LoadAccess(ctx context.Context, request AccessRequest) (accessState, error) {
	row := s.db.QueryRow(ctx, `
		SELECT p.role, p.status, p.account_epoch, p.membership_generation,
		       p.authority_version, w.authority_version, w.observed_authority_version, w.integrity_state,
		       s.tag_session_id, s.vibes_session_id, s.vibes_user_id, s.account_epoch,
		       s.expires_at, s.revoked_at,
		       g.membership_generation, g.authority_version, g.expires_at, g.revoked_at
		FROM tag_access_projection p
		JOIN tag_access_workspace_state w ON w.vibes_workspace_id = p.vibes_workspace_id
		LEFT JOIN tag_access_session s
		  ON s.tag_session_id = $3 AND s.vibes_user_id = p.vibes_user_id
		LEFT JOIN tag_session_workspace_grant g
		  ON g.tag_session_id = s.tag_session_id AND g.vibes_workspace_id = p.vibes_workspace_id
		WHERE p.vibes_user_id = $1 AND p.vibes_workspace_id = $2
	`, request.VIBESUserID, request.WorkspaceID, request.TagSessionID)
	var (
		role, status, integrity                                              string
		accountEpoch, generation, version, workspaceVersion, observedVersion int64
		sessionID, vibesSessionID, sessionUserID                             pgtype.Text
		sessionAccountEpoch, grantGeneration, grantVersion                   pgtype.Int8
		sessionExpiresAt, grantExpiresAt, sessionRevokedAt, grantRevokedAt   pgtype.Timestamptz
	)
	if err := row.Scan(
		&role, &status, &accountEpoch, &generation, &version, &workspaceVersion, &observedVersion, &integrity,
		&sessionID, &vibesSessionID, &sessionUserID, &sessionAccountEpoch, &sessionExpiresAt, &sessionRevokedAt,
		&grantGeneration, &grantVersion, &grantExpiresAt, &grantRevokedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accessState{}, errAccessNotFound
		}
		return accessState{}, err
	}
	if !sessionID.Valid || !vibesSessionID.Valid || !sessionUserID.Valid || !sessionAccountEpoch.Valid || !sessionExpiresAt.Valid ||
		!grantGeneration.Valid || !grantVersion.Valid || !grantExpiresAt.Valid || sessionRevokedAt.Valid || grantRevokedAt.Valid {
		return accessState{}, errGrantNotFound
	}
	if accountEpoch < 0 || generation < 0 || version < 0 || workspaceVersion < version || observedVersion < workspaceVersion || sessionAccountEpoch.Int64 < 0 || grantGeneration.Int64 < 0 || grantVersion.Int64 < 0 {
		return accessState{}, errors.New("invalid persisted Tag access state")
	}
	return accessState{
		projection: ProjectionEvent{
			VIBESUserID:          request.VIBESUserID,
			WorkspaceID:          request.WorkspaceID,
			Role:                 Role(role),
			Status:               Status(status),
			AccountEpoch:         uint64(accountEpoch),
			MembershipGeneration: uint64(generation),
			AuthorityVersion:     uint64(version),
		},
		integrity: projectionIntegrity(integrity),
		session: SessionGrant{
			TagSessionID:         sessionID.String,
			VIBESSessionID:       vibesSessionID.String,
			VIBESUserID:          sessionUserID.String,
			WorkspaceID:          request.WorkspaceID,
			AccountEpoch:         uint64(sessionAccountEpoch.Int64),
			MembershipGeneration: uint64(grantGeneration.Int64),
			AuthorityVersion:     uint64(grantVersion.Int64),
			SessionExpiresAt:     sessionExpiresAt.Time,
			GrantExpiresAt:       grantExpiresAt.Time,
		},
	}, nil
}

func lockWorkspace(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	lockKey := "tag-access-workspace:" + workspaceID
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey)
	return err
}

func loadWorkspaceForUpdate(ctx context.Context, tx pgx.Tx, workspaceID string) (workspaceRecord, bool, error) {
	var (
		version, observed int64
		integrity         string
		blockedDigest     []byte
	)
	err := tx.QueryRow(ctx, `
		SELECT authority_version, observed_authority_version, integrity_state, blocked_payload_digest
		FROM tag_access_workspace_state
		WHERE vibes_workspace_id = $1
		FOR UPDATE
	`, workspaceID).Scan(&version, &observed, &integrity, &blockedDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return workspaceRecord{}, false, nil
	}
	if err != nil {
		return workspaceRecord{}, false, err
	}
	if version < 0 || observed < version {
		return workspaceRecord{}, false, errors.New("invalid persisted Tag access Workspace state")
	}
	var blockedDigestArray [32]byte
	if len(blockedDigest) != 0 {
		if len(blockedDigest) != len(blockedDigestArray) {
			return workspaceRecord{}, false, errors.New("invalid blocked Tag access Workspace digest")
		}
		copy(blockedDigestArray[:], blockedDigest)
	}
	return workspaceRecord{
		workspaceID:      workspaceID,
		authorityVersion: uint64(version),
		observedVersion:  uint64(observed),
		integrity:        projectionIntegrity(integrity),
		blockedDigest:    blockedDigestArray,
	}, true, nil
}

func insertWorkspace(ctx context.Context, tx pgx.Tx, record workspaceRecord) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tag_access_workspace_state (
			vibes_workspace_id, authority_version, observed_authority_version,
			integrity_state, blocked_payload_digest
		) VALUES ($1, $2, $3, $4, $5)
	`, record.workspaceID, int64(record.authorityVersion), int64(record.observedVersion),
		record.integrity, optionalDigest(record.blockedDigest != [32]byte{}, record.blockedDigest))
	return err
}

func updateWorkspace(ctx context.Context, tx pgx.Tx, record workspaceRecord) error {
	_, err := tx.Exec(ctx, `
		UPDATE tag_access_workspace_state
		SET authority_version = $2, observed_authority_version = $3,
		    integrity_state = $4, blocked_payload_digest = $5, updated_at = now()
		WHERE vibes_workspace_id = $1
	`, record.workspaceID, int64(record.authorityVersion), int64(record.observedVersion),
		record.integrity, optionalDigest(record.blockedDigest != [32]byte{}, record.blockedDigest))
	return err
}

func loadProjectionForUpdate(ctx context.Context, tx pgx.Tx, userID, workspaceID string) (ProjectionEvent, bool, error) {
	var role, status, eventID string
	var accountEpoch, generation, version int64
	err := tx.QueryRow(ctx, `
		SELECT role, status, account_epoch, membership_generation, authority_version, last_event_id
		FROM tag_access_projection
		WHERE vibes_user_id = $1 AND vibes_workspace_id = $2
		FOR UPDATE
	`, userID, workspaceID).Scan(&role, &status, &accountEpoch, &generation, &version, &eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectionEvent{}, false, nil
	}
	if err != nil {
		return ProjectionEvent{}, false, err
	}
	if accountEpoch < 0 || generation < 0 || version < 0 {
		return ProjectionEvent{}, false, errors.New("invalid persisted Tag access projection")
	}
	return ProjectionEvent{
		EventID:              eventID,
		VIBESUserID:          userID,
		WorkspaceID:          workspaceID,
		Role:                 Role(role),
		Status:               Status(status),
		AccountEpoch:         uint64(accountEpoch),
		MembershipGeneration: uint64(generation),
		AuthorityVersion:     uint64(version),
	}, true, nil
}

func upsertProjection(ctx context.Context, tx pgx.Tx, projection ProjectionEvent, digest [32]byte) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tag_access_projection (
			vibes_user_id, vibes_workspace_id, role, status, account_epoch,
			membership_generation, authority_version, last_event_id, last_payload_digest
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (vibes_user_id, vibes_workspace_id) DO UPDATE SET
			role = EXCLUDED.role, status = EXCLUDED.status,
			account_epoch = EXCLUDED.account_epoch,
			membership_generation = EXCLUDED.membership_generation,
			authority_version = EXCLUDED.authority_version,
			last_event_id = EXCLUDED.last_event_id,
			last_payload_digest = EXCLUDED.last_payload_digest,
			updated_at = now()
	`, projection.VIBESUserID, projection.WorkspaceID, projection.Role, projection.Status,
		int64(projection.AccountEpoch), int64(projection.MembershipGeneration), int64(projection.AuthorityVersion),
		projection.EventID, digest[:])
	return err
}

func upsertBoundSession(ctx context.Context, tx pgx.Tx, grant SessionGrant) (time.Time, error) {
	var vibesSessionID, userID string
	var accountEpoch int64
	var expiresAt time.Time
	var revokedAt pgtype.Timestamptz
	err := tx.QueryRow(ctx, `
		SELECT vibes_session_id, vibes_user_id, account_epoch, expires_at, revoked_at
		FROM tag_access_session WHERE tag_session_id = $1 FOR UPDATE
	`, grant.TagSessionID).Scan(&vibesSessionID, &userID, &accountEpoch, &expiresAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `
			INSERT INTO tag_access_session (
				tag_session_id, vibes_session_id, vibes_user_id, account_epoch, expires_at
			) VALUES ($1, $2, $3, $4, $5)
		`, grant.TagSessionID, grant.VIBESSessionID, grant.VIBESUserID, int64(grant.AccountEpoch), grant.SessionExpiresAt)
		return grant.SessionExpiresAt, err
	}
	if err != nil {
		return time.Time{}, err
	}
	if revokedAt.Valid || vibesSessionID != grant.VIBESSessionID || userID != grant.VIBESUserID || accountEpoch != int64(grant.AccountEpoch) {
		return time.Time{}, ErrGrantDenied
	}
	if grant.SessionExpiresAt.Before(expiresAt) {
		expiresAt = grant.SessionExpiresAt
		if _, err := tx.Exec(ctx, `UPDATE tag_access_session SET expires_at = $2, updated_at = now() WHERE tag_session_id = $1`, grant.TagSessionID, expiresAt); err != nil {
			return time.Time{}, err
		}
	}
	return expiresAt, nil
}

func upsertBoundWorkspaceGrant(ctx context.Context, tx pgx.Tx, grant SessionGrant) error {
	var generation, version int64
	var expiresAt time.Time
	var revokedAt pgtype.Timestamptz
	err := tx.QueryRow(ctx, `
		SELECT membership_generation, authority_version, expires_at, revoked_at
		FROM tag_session_workspace_grant
		WHERE tag_session_id = $1 AND vibes_workspace_id = $2
		FOR UPDATE
	`, grant.TagSessionID, grant.WorkspaceID).Scan(&generation, &version, &expiresAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `
			INSERT INTO tag_session_workspace_grant (
				tag_session_id, vibes_workspace_id, membership_generation, authority_version, expires_at
			) VALUES ($1, $2, $3, $4, $5)
		`, grant.TagSessionID, grant.WorkspaceID, int64(grant.MembershipGeneration), int64(grant.AuthorityVersion), grant.GrantExpiresAt)
		return err
	}
	if err != nil {
		return err
	}
	if revokedAt.Valid || generation != int64(grant.MembershipGeneration) || version > int64(grant.AuthorityVersion) {
		return ErrGrantDenied
	}
	if grant.GrantExpiresAt.After(expiresAt) {
		grant.GrantExpiresAt = expiresAt
	}
	_, err = tx.Exec(ctx, `
		UPDATE tag_session_workspace_grant
		SET authority_version = $3, expires_at = $4, updated_at = now()
		WHERE tag_session_id = $1 AND vibes_workspace_id = $2
	`, grant.TagSessionID, grant.WorkspaceID, int64(grant.AuthorityVersion), grant.GrantExpiresAt)
	return err
}

func optionalDigest(present bool, digest [32]byte) []byte {
	if !present {
		return []byte{}
	}
	return digest[:]
}
