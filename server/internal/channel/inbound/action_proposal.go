package inbound

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

const (
	proposalStatusPending   = "pending"
	proposalStatusConfirmed = "confirmed"
	proposalStatusCancelled = "cancelled"
	proposalStatusExpired   = "expired"
)

type ActionProposalStore interface {
	CreateActionProposal(ctx context.Context, req ActionProposalCreateRequest) (ActionProposal, error)
	FindActionProposal(ctx context.Context, connectionID, chatID, senderID, code string) (ActionProposal, error)
	MarkActionProposalStatus(ctx context.Context, id pgtype.UUID, status string) error
}

type ActionProposalCreateRequest struct {
	ConnectionID   string
	ChatID         string
	SenderID       string
	WorkspaceID    pgtype.UUID
	InboundEventID pgtype.UUID
	Intent         port.InboundIntent
	ExpiresAt      time.Time
}

type ActionProposal struct {
	ID             pgtype.UUID
	Code           string
	ConnectionID   string
	ChatID         string
	SenderID       string
	WorkspaceID    pgtype.UUID
	InboundEventID pgtype.UUID
	Intent         port.InboundIntent
	Status         string
	ExpiresAt      time.Time
}

type DBActionProposalStore struct {
	pool *pgxpool.Pool
}

func NewDBActionProposalStore(pool *pgxpool.Pool) *DBActionProposalStore {
	return &DBActionProposalStore{pool: pool}
}

func (s *DBActionProposalStore) CreateActionProposal(ctx context.Context, req ActionProposalCreateRequest) (ActionProposal, error) {
	if s == nil || s.pool == nil {
		return ActionProposal{}, fmt.Errorf("proposal store is not configured")
	}
	code, err := newProposalCode()
	if err != nil {
		return ActionProposal{}, err
	}
	payload, err := json.Marshal(req.Intent)
	if err != nil {
		return ActionProposal{}, fmt.Errorf("marshal proposal intent: %w", err)
	}
	var p ActionProposal
	var intentJSON []byte
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO channel_action_proposal (
			code, connection_id, chat_id, sender_external_id, workspace_id,
			inbound_event_id, action_kind, intent_payload, status, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', $9)
		ON CONFLICT (inbound_event_id, action_kind)
		DO UPDATE SET updated_at = now()
		RETURNING id, code, connection_id, chat_id, sender_external_id, workspace_id,
		          inbound_event_id, intent_payload, status, expires_at
	`, code, req.ConnectionID, req.ChatID, req.SenderID, req.WorkspaceID,
		req.InboundEventID, string(req.Intent.Kind), payload, req.ExpiresAt).Scan(
		&p.ID,
		&p.Code,
		&p.ConnectionID,
		&p.ChatID,
		&p.SenderID,
		&p.WorkspaceID,
		&p.InboundEventID,
		&intentJSON,
		&p.Status,
		&p.ExpiresAt,
	); err != nil {
		return ActionProposal{}, err
	}
	if err := json.Unmarshal(intentJSON, &p.Intent); err != nil {
		return ActionProposal{}, fmt.Errorf("unmarshal proposal intent: %w", err)
	}
	return p, nil
}

func (s *DBActionProposalStore) FindActionProposal(ctx context.Context, connectionID, chatID, senderID, code string) (ActionProposal, error) {
	if s == nil || s.pool == nil {
		return ActionProposal{}, fmt.Errorf("proposal store is not configured")
	}
	var p ActionProposal
	var intentJSON []byte
	if err := s.pool.QueryRow(ctx, `
		SELECT id, code, connection_id, chat_id, sender_external_id, workspace_id,
		       inbound_event_id, intent_payload, status, expires_at
		FROM channel_action_proposal
		WHERE connection_id = $1
		  AND chat_id = $2
		  AND sender_external_id = $3
		  AND upper(code) = upper($4)
		ORDER BY created_at DESC
		LIMIT 1
	`, connectionID, chatID, senderID, code).Scan(
		&p.ID,
		&p.Code,
		&p.ConnectionID,
		&p.ChatID,
		&p.SenderID,
		&p.WorkspaceID,
		&p.InboundEventID,
		&intentJSON,
		&p.Status,
		&p.ExpiresAt,
	); err != nil {
		return ActionProposal{}, err
	}
	if err := json.Unmarshal(intentJSON, &p.Intent); err != nil {
		return ActionProposal{}, fmt.Errorf("unmarshal proposal intent: %w", err)
	}
	return p, nil
}

func (s *DBActionProposalStore) MarkActionProposalStatus(ctx context.Context, id pgtype.UUID, status string) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("proposal store is not configured")
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE channel_action_proposal
		SET status = $2, updated_at = now()
		WHERE id = $1
	`, id, status)
	return err
}

func newProposalCode() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate proposal code: %w", err)
	}
	return strings.ToUpper(hex.EncodeToString(b[:])), nil
}

var _ ActionProposalStore = (*DBActionProposalStore)(nil)
