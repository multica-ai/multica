package lark

// ChannelStore is the production data layer for the Feishu integration after
// MUL-3515 generalized lark_* into channel_*. It embeds *db.Queries (so every
// generic query — chat_session, chat_message, member, workspace, agent — is
// available unchanged) and adds the feishu-specific store methods, each backed
// by a channel_* query and translating at the JSONB-config boundary (store.go).
//
// The methods take and return the package's flat domain types (Installation,
// UserBinding, ChatSessionBinding, InboundMessageDedup, BindingTokenRow,
// OutboundCardMessage) and the *Params types in params.go. This store reads
// and writes only channel_*, never lark_*; queries/lark.sql is deleted. The
// physical lark_* tables are retained one release for rollout/rollback safety
// (see migration 124's ROLLOUT note) and dropped by a later cleanup migration.

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// channelTypeFeishu is the channel_type discriminator for every row this
// Feishu-backed store reads or writes.
const channelTypeFeishu = "feishu"

type ChannelStore struct {
	*db.Queries
}

// NewChannelStore wraps a *db.Queries so the lark package's DB seams resolve to
// channel_* rows.
func NewChannelStore(q *db.Queries) *ChannelStore {
	return &ChannelStore{Queries: q}
}

// WithTx returns a ChannelStore bound to tx. It shadows db.Queries.WithTx (which
// returns *db.Queries) so transactional callers (chat ingest, token redemption)
// keep the channel-backed store methods inside their tx.
func (s *ChannelStore) WithTx(tx pgx.Tx) *ChannelStore {
	return &ChannelStore{Queries: s.Queries.WithTx(tx)}
}

// IsWorkspaceMember reports whether userID is currently a member of
// workspaceID. With the lark_user_binding -> member foreign key removed
// (MUL-3515 §4), a binding row no longer proves membership, so the inbound
// identity step calls this to re-check it explicitly. ErrNoRows -> not a member.
func (s *ChannelStore) IsWorkspaceMember(ctx context.Context, workspaceID, userID pgtype.UUID) (bool, error) {
	_, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ---- installation ----

func (s *ChannelStore) GetLarkInstallationByAppID(ctx context.Context, appID string) (Installation, error) {
	row, err := s.Queries.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: channelTypeFeishu,
		AppID:       appID,
	})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

func (s *ChannelStore) GetLarkInstallation(ctx context.Context, id pgtype.UUID) (Installation, error) {
	row, err := s.Queries.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          id,
		ChannelType: channelTypeFeishu,
	})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

func (s *ChannelStore) GetLarkInstallationInWorkspace(ctx context.Context, arg GetInstallationInWorkspaceParams) (Installation, error) {
	row, err := s.Queries.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          arg.ID,
		WorkspaceID: arg.WorkspaceID,
		ChannelType: channelTypeFeishu,
	})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

func (s *ChannelStore) ListLarkInstallationsByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]Installation, error) {
	rows, err := s.Queries.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: workspaceID,
		ChannelType: channelTypeFeishu,
	})
	if err != nil {
		return nil, err
	}
	return installationsFromRows(rows)
}

func (s *ChannelStore) ListActiveLarkInstallations(ctx context.Context) ([]Installation, error) {
	rows, err := s.Queries.ListActiveChannelInstallations(ctx, channelTypeFeishu)
	if err != nil {
		return nil, err
	}
	return installationsFromRows(rows)
}

func (s *ChannelStore) UpsertLarkInstallation(ctx context.Context, arg UpsertInstallationParams) (Installation, error) {
	cfg, err := encodeInstallConfig(Installation{
		AppID:              arg.AppID,
		AppSecretEncrypted: arg.AppSecretEncrypted,
		TenantKey:          arg.TenantKey,
		BotOpenID:          arg.BotOpenID,
		BotUnionID:         arg.BotUnionID,
		Region:             arg.Region,
	})
	if err != nil {
		return Installation{}, err
	}
	row, err := s.Queries.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		ChannelType:     channelTypeFeishu,
		Config:          cfg,
		InstallerUserID: arg.InstallerUserID,
	})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

func (s *ChannelStore) SetLarkInstallationStatus(ctx context.Context, arg SetInstallationStatusParams) error {
	return s.Queries.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     arg.ID,
		Status: arg.Status,
	})
}

// SetLarkInstallationBotUnionID folds bot_union_id into the JSONB config via a
// read-modify-write through SetChannelInstallationConfig (channel_installation
// has no dedicated union_id column). This is the operator union_id backfill,
// keyed by id and effectively single-writer, so the non-atomic RMW is safe —
// the same shape the channel.sql comment documents for this query.
func (s *ChannelStore) SetLarkInstallationBotUnionID(ctx context.Context, arg SetInstallationBotUnionIDParams) error {
	row, err := s.Queries.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          arg.ID,
		ChannelType: channelTypeFeishu,
	})
	if err != nil {
		return err
	}
	inst, err := installationFromRow(row)
	if err != nil {
		return err
	}
	inst.BotUnionID = arg.BotUnionID
	cfg, err := encodeInstallConfig(inst)
	if err != nil {
		return err
	}
	return s.Queries.SetChannelInstallationConfig(ctx, db.SetChannelInstallationConfigParams{
		ID:     arg.ID,
		Config: cfg,
	})
}

func (s *ChannelStore) BackfillLarkInstallationRegionToLark(ctx context.Context) (int64, error) {
	return s.Queries.BackfillChannelInstallationRegionToFeishuLark(ctx)
}

// ---- WS lease ----

func (s *ChannelStore) AcquireLarkWSLease(ctx context.Context, arg AcquireWSLeaseParams) (Installation, error) {
	row, err := s.Queries.AcquireChannelWSLease(ctx, db.AcquireChannelWSLeaseParams{
		NewToken:     arg.NewToken,
		NewExpiresAt: arg.NewExpiresAt,
		ID:           arg.ID,
	})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

func (s *ChannelStore) ReleaseLarkWSLease(ctx context.Context, arg ReleaseWSLeaseParams) error {
	return s.Queries.ReleaseChannelWSLease(ctx, db.ReleaseChannelWSLeaseParams{
		ID:           arg.ID,
		CurrentToken: arg.CurrentToken,
	})
}

// ---- user binding ----

func (s *ChannelStore) GetLarkUserBindingByOpenID(ctx context.Context, arg GetUserBindingByOpenIDParams) (UserBinding, error) {
	row, err := s.Queries.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: arg.InstallationID,
		ChannelUserID:  arg.ChannelUserID,
	})
	if err != nil {
		return UserBinding{}, err
	}
	return userBindingFromRow(row)
}

func (s *ChannelStore) CreateLarkUserBinding(ctx context.Context, arg CreateUserBindingParams) (UserBinding, error) {
	cfg, err := encodeBindingConfig(UserBinding{UnionID: arg.UnionID})
	if err != nil {
		return UserBinding{}, err
	}
	row, err := s.Queries.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
		WorkspaceID:    arg.WorkspaceID,
		MulticaUserID:  arg.MulticaUserID,
		InstallationID: arg.InstallationID,
		ChannelType:    channelTypeFeishu,
		ChannelUserID:  arg.ChannelUserID,
		Config:         cfg,
	})
	if err != nil {
		return UserBinding{}, err
	}
	return userBindingFromRow(row)
}

// ---- chat session binding ----

func (s *ChannelStore) GetLarkChatSessionBinding(ctx context.Context, arg GetChatSessionBindingParams) (ChatSessionBinding, error) {
	row, err := s.Queries.GetChannelChatSessionBinding(ctx, db.GetChannelChatSessionBindingParams{
		InstallationID: arg.InstallationID,
		ChannelChatID:  arg.ChannelChatID,
	})
	if err != nil {
		return ChatSessionBinding{}, err
	}
	return chatSessionBindingFromRow(row), nil
}

func (s *ChannelStore) GetLarkChatSessionBindingBySession(ctx context.Context, chatSessionID pgtype.UUID) (ChatSessionBinding, error) {
	row, err := s.Queries.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: chatSessionID,
		ChannelType:   channelTypeFeishu,
	})
	if err != nil {
		return ChatSessionBinding{}, err
	}
	return chatSessionBindingFromRow(row), nil
}

func (s *ChannelStore) CreateLarkChatSessionBinding(ctx context.Context, arg CreateChatSessionBindingParams) (ChatSessionBinding, error) {
	row, err := s.Queries.CreateChannelChatSessionBinding(ctx, db.CreateChannelChatSessionBindingParams{
		ChatSessionID:  arg.ChatSessionID,
		InstallationID: arg.InstallationID,
		ChannelType:    channelTypeFeishu,
		ChannelChatID:  arg.ChannelChatID,
		ChatType:       arg.ChatType,
		// Feishu's channel_chat_id is the real chat id, so the key alone routes
		// outbound; config stays the empty object (the column is NOT NULL).
		Config: []byte("{}"),
	})
	if err != nil {
		return ChatSessionBinding{}, err
	}
	return chatSessionBindingFromRow(row), nil
}

func (s *ChannelStore) UpdateLarkChatSessionBindingReplyTarget(ctx context.Context, arg UpdateChatSessionBindingReplyTargetParams) error {
	return s.Queries.UpdateChannelChatSessionBindingReplyTarget(ctx, db.UpdateChannelChatSessionBindingReplyTargetParams{
		ChatSessionID: arg.ChatSessionID,
		LastMessageID: arg.LastMessageID,
		LastThreadID:  arg.LastThreadID,
	})
}

// ---- inbound dedup ----

func (s *ChannelStore) ClaimLarkInboundDedup(ctx context.Context, arg ClaimInboundDedupParams) (InboundMessageDedup, error) {
	row, err := s.Queries.ClaimChannelInboundDedup(ctx, db.ClaimChannelInboundDedupParams{
		InstallationID: arg.InstallationID,
		MessageID:      arg.MessageID,
	})
	if err != nil {
		return InboundMessageDedup{}, err
	}
	return dedupFromRow(row), nil
}

func (s *ChannelStore) MarkLarkInboundDedupProcessed(ctx context.Context, arg MarkInboundDedupProcessedParams) (int64, error) {
	return s.Queries.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
		InstallationID: arg.InstallationID,
		MessageID:      arg.MessageID,
		ClaimToken:     arg.ClaimToken,
	})
}

func (s *ChannelStore) ReleaseLarkInboundDedup(ctx context.Context, arg ReleaseInboundDedupParams) (int64, error) {
	return s.Queries.ReleaseChannelInboundDedup(ctx, db.ReleaseChannelInboundDedupParams{
		InstallationID: arg.InstallationID,
		MessageID:      arg.MessageID,
		ClaimToken:     arg.ClaimToken,
	})
}

// ---- audit ----

func (s *ChannelStore) RecordLarkInboundDrop(ctx context.Context, arg RecordInboundDropParams) error {
	return s.Queries.RecordChannelInboundDrop(ctx, db.RecordChannelInboundDropParams{
		ChannelType:      channelTypeFeishu,
		EventType:        arg.EventType,
		DropReason:       arg.DropReason,
		InstallationID:   arg.InstallationID,
		ChannelChatID:    arg.ChannelChatID,
		ChannelEventID:   arg.ChannelEventID,
		ChannelMessageID: arg.ChannelMessageID,
	})
}

// ---- binding token ----

func (s *ChannelStore) CreateLarkBindingToken(ctx context.Context, arg CreateBindingTokenParams) (BindingTokenRow, error) {
	row, err := s.Queries.CreateChannelBindingToken(ctx, db.CreateChannelBindingTokenParams{
		TokenHash:      arg.TokenHash,
		WorkspaceID:    arg.WorkspaceID,
		InstallationID: arg.InstallationID,
		ChannelType:    channelTypeFeishu,
		ChannelUserID:  arg.ChannelUserID,
		ExpiresAt:      arg.ExpiresAt,
	})
	if err != nil {
		return BindingTokenRow{}, err
	}
	return bindingTokenFromRow(row), nil
}

func (s *ChannelStore) ConsumeLarkBindingToken(ctx context.Context, tokenHash string) (BindingTokenRow, error) {
	row, err := s.Queries.ConsumeChannelBindingToken(ctx, tokenHash)
	if err != nil {
		return BindingTokenRow{}, err
	}
	return bindingTokenFromRow(row), nil
}

// ---- outbound card ----

func (s *ChannelStore) GetLarkOutboundCardByTask(ctx context.Context, taskID pgtype.UUID) (OutboundCardMessage, error) {
	row, err := s.Queries.GetChannelOutboundCardByTask(ctx, db.GetChannelOutboundCardByTaskParams{
		TaskID:      taskID,
		ChannelType: channelTypeFeishu,
	})
	if err != nil {
		return OutboundCardMessage{}, err
	}
	return outboundCardFromRow(row), nil
}

func (s *ChannelStore) CreateLarkOutboundCardMessage(ctx context.Context, arg CreateOutboundCardMessageParams) (OutboundCardMessage, error) {
	row, err := s.Queries.CreateChannelOutboundCardMessage(ctx, db.CreateChannelOutboundCardMessageParams{
		ChatSessionID:        arg.ChatSessionID,
		ChannelType:          channelTypeFeishu,
		ChannelChatID:        arg.ChannelChatID,
		ChannelCardMessageID: arg.ChannelCardMessageID,
		Status:               arg.Status,
		TaskID:               arg.TaskID,
	})
	if err != nil {
		return OutboundCardMessage{}, err
	}
	return outboundCardFromRow(row), nil
}

func (s *ChannelStore) UpdateLarkOutboundCardStatus(ctx context.Context, arg UpdateOutboundCardStatusParams) error {
	return s.Queries.UpdateChannelOutboundCardStatus(ctx, db.UpdateChannelOutboundCardStatusParams{
		ID:     arg.ID,
		Status: arg.Status,
	})
}

// installationsFromRows maps a slice of channel_installation rows to domain
// Installations, surfacing the first config-decode error.
func installationsFromRows(rows []db.ChannelInstallation) ([]Installation, error) {
	out := make([]Installation, len(rows))
	for i, row := range rows {
		inst, err := installationFromRow(row)
		if err != nil {
			return nil, err
		}
		out[i] = inst
	}
	return out, nil
}
