package realtime

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/tagaccess"
)

type PostgresConnectionCloseReceiptStore struct {
	db *pgxpool.Pool
}

func NewPostgresConnectionCloseReceiptStore(db *pgxpool.Pool) *PostgresConnectionCloseReceiptStore {
	return &PostgresConnectionCloseReceiptStore{db: db}
}

func (s *PostgresConnectionCloseReceiptStore) Load(ctx context.Context, command tagaccess.ConnectionCloseCommand) (tagaccess.ConnectionCloseReceipt, bool, error) {
	if s == nil || s.db == nil {
		return tagaccess.ConnectionCloseReceipt{}, false, errors.New("connection-close receipt store is not configured")
	}
	var receipt tagaccess.ConnectionCloseReceipt
	var source string
	var authorityVersion, identityVersion, sessionWorkspaceGeneration int64
	err := s.db.QueryRow(ctx, `
		SELECT receipt_id, source, delivery_id, correlation_id, vibes_workspace_id,
		       authority_version, identity_restriction_version, session_workspace_generation,
		       target_digest, completed_at
		FROM tag_access_connection_close_receipt
		WHERE delivery_id = $1 AND target_digest = $2
	`, command.DeliveryID, command.TargetDigest).Scan(
		&receipt.ReceiptID, &source, &receipt.DeliveryID, &receipt.CorrelationID, &receipt.WorkspaceID,
		&authorityVersion, &identityVersion, &sessionWorkspaceGeneration, &receipt.TargetDigest, &receipt.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return tagaccess.ConnectionCloseReceipt{}, false, nil
	}
	if err != nil {
		return tagaccess.ConnectionCloseReceipt{}, false, err
	}
	if authorityVersion < 0 || identityVersion < 0 || sessionWorkspaceGeneration < 0 {
		return tagaccess.ConnectionCloseReceipt{}, false, errors.New("invalid durable connection-close receipt")
	}
	receipt.Source = tagaccess.ConnectionCloseSource(source)
	receipt.AuthorityVersion = uint64(authorityVersion)
	receipt.IdentityRestrictionVersion = uint64(identityVersion)
	receipt.SessionWorkspaceGeneration = uint64(sessionWorkspaceGeneration)
	return receipt, true, nil
}

func (s *PostgresConnectionCloseReceiptStore) LoadParticipants(ctx context.Context, command tagaccess.ConnectionCloseCommand) ([]string, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, errors.New("connection-close receipt store is not configured")
	}
	var source, deliveryID, correlationID, workspaceID, targetDigest string
	var authorityVersion, identityVersion, sessionWorkspaceGeneration int64
	var participants []string
	err := s.db.QueryRow(ctx, `
		SELECT source, delivery_id, correlation_id, vibes_workspace_id,
		       authority_version, identity_restriction_version, session_workspace_generation,
		       target_digest, participant_instance_ids
		FROM tag_access_connection_close_dispatch
		WHERE command_id = $1
	`, connectionCloseCommandID(command)).Scan(
		&source, &deliveryID, &correlationID, &workspaceID,
		&authorityVersion, &identityVersion, &sessionWorkspaceGeneration, &targetDigest, &participants,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if authorityVersion < 0 || identityVersion < 0 || sessionWorkspaceGeneration < 0 || source != string(command.Source) || deliveryID != command.DeliveryID ||
		correlationID != command.CorrelationID || workspaceID != command.WorkspaceID || uint64(authorityVersion) != command.AuthorityVersion ||
		uint64(identityVersion) != command.IdentityRestrictionVersion || uint64(sessionWorkspaceGeneration) != command.SessionWorkspaceGeneration ||
		targetDigest != command.TargetDigest {
		return nil, false, errors.New("conflicting durable connection-close dispatch evidence")
	}
	participants, err = normalizeParticipantIDs(participants)
	if err != nil {
		return nil, false, errors.New("invalid durable connection-close dispatch evidence")
	}
	return participants, true, nil
}

func (s *PostgresConnectionCloseReceiptStore) ClaimParticipants(ctx context.Context, command tagaccess.ConnectionCloseCommand, participants []string) ([]string, error) {
	participants, err := normalizeParticipantIDs(participants)
	if err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, errors.New("connection-close receipt store is not configured")
	}
	if command.AuthorityVersion > uint64(^uint64(0)>>1) || command.IdentityRestrictionVersion > uint64(^uint64(0)>>1) ||
		command.SessionWorkspaceGeneration > uint64(^uint64(0)>>1) {
		return nil, errors.New("connection-close dispatch version exceeds database range")
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO tag_access_connection_close_dispatch (
			command_id, source, delivery_id, correlation_id, vibes_workspace_id,
			authority_version, identity_restriction_version, session_workspace_generation,
			target_digest, participant_instance_ids
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (command_id) DO NOTHING
	`, connectionCloseCommandID(command), command.Source, command.DeliveryID, command.CorrelationID, command.WorkspaceID,
		int64(command.AuthorityVersion), int64(command.IdentityRestrictionVersion), int64(command.SessionWorkspaceGeneration),
		command.TargetDigest, participants)
	if err != nil {
		return nil, err
	}
	persisted, ok, err := s.LoadParticipants(ctx, command)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("durable connection-close dispatch evidence was not readable")
	}
	return persisted, nil
}

func (s *PostgresConnectionCloseReceiptStore) Save(ctx context.Context, command tagaccess.ConnectionCloseCommand, receipt tagaccess.ConnectionCloseReceipt) error {
	if s == nil || s.db == nil || !receiptMatchesCommand(receipt, command) {
		return errors.New("invalid completed connection-close receipt")
	}
	if receipt.AuthorityVersion > uint64(^uint64(0)>>1) || receipt.IdentityRestrictionVersion > uint64(^uint64(0)>>1) ||
		receipt.SessionWorkspaceGeneration > uint64(^uint64(0)>>1) {
		return errors.New("connection-close receipt version exceeds database range")
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO tag_access_connection_close_receipt (
			receipt_id, source, delivery_id, correlation_id, vibes_workspace_id,
			authority_version, identity_restriction_version, session_workspace_generation, target_digest, completed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (delivery_id, target_digest) DO NOTHING
	`, receipt.ReceiptID, receipt.Source, receipt.DeliveryID, receipt.CorrelationID, receipt.WorkspaceID,
		int64(receipt.AuthorityVersion), int64(receipt.IdentityRestrictionVersion), int64(receipt.SessionWorkspaceGeneration),
		receipt.TargetDigest, receipt.CompletedAt.UTC())
	if err != nil {
		return err
	}
	persisted, ok, err := s.Load(ctx, command)
	if err != nil {
		return err
	}
	if !ok || !receiptMatchesCommand(persisted, command) {
		return errors.New("durable connection-close receipt conflict")
	}
	return nil
}

var _ ConnectionCloseReceiptStore = (*PostgresConnectionCloseReceiptStore)(nil)
