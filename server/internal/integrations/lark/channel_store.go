package lark

// ChannelStore is the production data layer for the Feishu integration after
// MUL-3515 generalized lark_* into channel_*. It embeds *db.Queries (so every
// generic query — chat_session, chat_message, member, workspace, agent — is
// available unchanged) and SHADOWS the ~two dozen lark_* query methods with
// channel_*-backed equivalents that keep the exact same db.Lark* signatures.
//
// That shadowing is the whole trick of the cutover: the lark package's
// dispatcher/hub/services and their ~20k lines of tests keep calling
// GetLarkInstallationByAppID / ClaimLarkInboundDedup / ... and keep passing and
// receiving db.Lark* structs, so none of that logic (or its fakes) changes.
// Only the production wiring swaps *db.Queries for *ChannelStore, and only this
// file knows that the rows now live in channel_* with the feishu-specific
// columns folded into the JSONB config (encoded/decoded by store.go).
//
// The db.Lark* <-> channel translation here is intentionally throwaway: a later
// commit renames db.Lark* to the lark.* domain types and drops the lark_*
// queries, at which point these converters collapse into the store.go mappers.
// Until then this is a translation layer, NOT a dual-write/compat shim — only
// channel_* is ever read or written.

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
// keep the channel-backed lark methods inside their tx.
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

func (s *ChannelStore) GetLarkInstallationByAppID(ctx context.Context, appID string) (db.LarkInstallation, error) {
	row, err := s.Queries.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: channelTypeFeishu,
		AppID:       appID,
	})
	if err != nil {
		return db.LarkInstallation{}, err
	}
	return channelInstallationToLark(row)
}

func (s *ChannelStore) GetLarkInstallation(ctx context.Context, id pgtype.UUID) (db.LarkInstallation, error) {
	row, err := s.Queries.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          id,
		ChannelType: channelTypeFeishu,
	})
	if err != nil {
		return db.LarkInstallation{}, err
	}
	return channelInstallationToLark(row)
}

func (s *ChannelStore) GetLarkInstallationInWorkspace(ctx context.Context, arg db.GetLarkInstallationInWorkspaceParams) (db.LarkInstallation, error) {
	row, err := s.Queries.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          arg.ID,
		WorkspaceID: arg.WorkspaceID,
		ChannelType: channelTypeFeishu,
	})
	if err != nil {
		return db.LarkInstallation{}, err
	}
	return channelInstallationToLark(row)
}

func (s *ChannelStore) ListLarkInstallationsByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]db.LarkInstallation, error) {
	rows, err := s.Queries.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: workspaceID,
		ChannelType: channelTypeFeishu,
	})
	if err != nil {
		return nil, err
	}
	return channelInstallationsToLark(rows)
}

func (s *ChannelStore) ListActiveLarkInstallations(ctx context.Context) ([]db.LarkInstallation, error) {
	rows, err := s.Queries.ListActiveChannelInstallations(ctx, channelTypeFeishu)
	if err != nil {
		return nil, err
	}
	return channelInstallationsToLark(rows)
}

func (s *ChannelStore) UpsertLarkInstallation(ctx context.Context, arg db.UpsertLarkInstallationParams) (db.LarkInstallation, error) {
	cfg, err := encodeInstallConfig(Installation{
		AppID:              arg.AppID,
		AppSecretEncrypted: arg.AppSecretEncrypted,
		TenantKey:          arg.TenantKey,
		BotOpenID:          arg.BotOpenID,
		BotUnionID:         arg.BotUnionID,
		Region:             arg.Region,
	})
	if err != nil {
		return db.LarkInstallation{}, err
	}
	row, err := s.Queries.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		ChannelType:     channelTypeFeishu,
		Config:          cfg,
		InstallerUserID: arg.InstallerUserID,
	})
	if err != nil {
		return db.LarkInstallation{}, err
	}
	return channelInstallationToLark(row)
}

func (s *ChannelStore) SetLarkInstallationStatus(ctx context.Context, arg db.SetLarkInstallationStatusParams) error {
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
func (s *ChannelStore) SetLarkInstallationBotUnionID(ctx context.Context, arg db.SetLarkInstallationBotUnionIDParams) error {
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

func (s *ChannelStore) AcquireLarkWSLease(ctx context.Context, arg db.AcquireLarkWSLeaseParams) (db.LarkInstallation, error) {
	row, err := s.Queries.AcquireChannelWSLease(ctx, db.AcquireChannelWSLeaseParams{
		NewToken:     arg.NewToken,
		NewExpiresAt: arg.NewExpiresAt,
		ID:           arg.ID,
	})
	if err != nil {
		return db.LarkInstallation{}, err
	}
	return channelInstallationToLark(row)
}

func (s *ChannelStore) ReleaseLarkWSLease(ctx context.Context, arg db.ReleaseLarkWSLeaseParams) error {
	return s.Queries.ReleaseChannelWSLease(ctx, db.ReleaseChannelWSLeaseParams{
		ID:           arg.ID,
		CurrentToken: arg.CurrentToken,
	})
}

// ---- user binding ----

func (s *ChannelStore) GetLarkUserBindingByOpenID(ctx context.Context, arg db.GetLarkUserBindingByOpenIDParams) (db.LarkUserBinding, error) {
	row, err := s.Queries.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: arg.InstallationID,
		ChannelUserID:  arg.LarkOpenID,
	})
	if err != nil {
		return db.LarkUserBinding{}, err
	}
	return channelUserBindingToLark(row)
}

func (s *ChannelStore) CreateLarkUserBinding(ctx context.Context, arg db.CreateLarkUserBindingParams) (db.LarkUserBinding, error) {
	cfg, err := encodeBindingConfig(UserBinding{UnionID: arg.UnionID})
	if err != nil {
		return db.LarkUserBinding{}, err
	}
	row, err := s.Queries.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
		WorkspaceID:    arg.WorkspaceID,
		MulticaUserID:  arg.MulticaUserID,
		InstallationID: arg.InstallationID,
		ChannelType:    channelTypeFeishu,
		ChannelUserID:  arg.LarkOpenID,
		Config:         cfg,
	})
	if err != nil {
		return db.LarkUserBinding{}, err
	}
	return channelUserBindingToLark(row)
}

// ---- chat session binding ----

func (s *ChannelStore) GetLarkChatSessionBinding(ctx context.Context, arg db.GetLarkChatSessionBindingParams) (db.LarkChatSessionBinding, error) {
	row, err := s.Queries.GetChannelChatSessionBinding(ctx, db.GetChannelChatSessionBindingParams{
		InstallationID: arg.InstallationID,
		ChannelChatID:  arg.LarkChatID,
	})
	if err != nil {
		return db.LarkChatSessionBinding{}, err
	}
	return larkChatSessionBinding(row), nil
}

func (s *ChannelStore) GetLarkChatSessionBindingBySession(ctx context.Context, chatSessionID pgtype.UUID) (db.LarkChatSessionBinding, error) {
	row, err := s.Queries.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: chatSessionID,
		ChannelType:   channelTypeFeishu,
	})
	if err != nil {
		return db.LarkChatSessionBinding{}, err
	}
	return larkChatSessionBinding(row), nil
}

func (s *ChannelStore) CreateLarkChatSessionBinding(ctx context.Context, arg db.CreateLarkChatSessionBindingParams) (db.LarkChatSessionBinding, error) {
	row, err := s.Queries.CreateChannelChatSessionBinding(ctx, db.CreateChannelChatSessionBindingParams{
		ChatSessionID:  arg.ChatSessionID,
		InstallationID: arg.InstallationID,
		ChannelType:    channelTypeFeishu,
		ChannelChatID:  arg.LarkChatID,
		ChatType:       arg.LarkChatType,
	})
	if err != nil {
		return db.LarkChatSessionBinding{}, err
	}
	return larkChatSessionBinding(row), nil
}

func (s *ChannelStore) UpdateLarkChatSessionBindingReplyTarget(ctx context.Context, arg db.UpdateLarkChatSessionBindingReplyTargetParams) error {
	return s.Queries.UpdateChannelChatSessionBindingReplyTarget(ctx, db.UpdateChannelChatSessionBindingReplyTargetParams{
		ChatSessionID: arg.ChatSessionID,
		LastMessageID: arg.LastLarkMessageID,
		LastThreadID:  arg.LastLarkThreadID,
	})
}

// ---- inbound dedup ----

func (s *ChannelStore) ClaimLarkInboundDedup(ctx context.Context, arg db.ClaimLarkInboundDedupParams) (db.LarkInboundMessageDedup, error) {
	row, err := s.Queries.ClaimChannelInboundDedup(ctx, db.ClaimChannelInboundDedupParams{
		InstallationID: arg.InstallationID,
		MessageID:      arg.MessageID,
	})
	if err != nil {
		return db.LarkInboundMessageDedup{}, err
	}
	return dedupToLark(row), nil
}

func (s *ChannelStore) MarkLarkInboundDedupProcessed(ctx context.Context, arg db.MarkLarkInboundDedupProcessedParams) (int64, error) {
	return s.Queries.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
		InstallationID: arg.InstallationID,
		MessageID:      arg.MessageID,
		ClaimToken:     arg.ClaimToken,
	})
}

func (s *ChannelStore) ReleaseLarkInboundDedup(ctx context.Context, arg db.ReleaseLarkInboundDedupParams) (int64, error) {
	return s.Queries.ReleaseChannelInboundDedup(ctx, db.ReleaseChannelInboundDedupParams{
		InstallationID: arg.InstallationID,
		MessageID:      arg.MessageID,
		ClaimToken:     arg.ClaimToken,
	})
}

// ---- audit ----

func (s *ChannelStore) RecordLarkInboundDrop(ctx context.Context, arg db.RecordLarkInboundDropParams) error {
	return s.Queries.RecordChannelInboundDrop(ctx, db.RecordChannelInboundDropParams{
		ChannelType:      channelTypeFeishu,
		EventType:        arg.EventType,
		DropReason:       arg.DropReason,
		InstallationID:   arg.InstallationID,
		ChannelChatID:    arg.LarkChatID,
		ChannelEventID:   arg.LarkEventID,
		ChannelMessageID: arg.LarkMessageID,
	})
}

// ---- binding token ----

func (s *ChannelStore) CreateLarkBindingToken(ctx context.Context, arg db.CreateLarkBindingTokenParams) (db.LarkBindingToken, error) {
	row, err := s.Queries.CreateChannelBindingToken(ctx, db.CreateChannelBindingTokenParams{
		TokenHash:      arg.TokenHash,
		WorkspaceID:    arg.WorkspaceID,
		InstallationID: arg.InstallationID,
		ChannelType:    channelTypeFeishu,
		ChannelUserID:  arg.LarkOpenID,
		ExpiresAt:      arg.ExpiresAt,
	})
	if err != nil {
		return db.LarkBindingToken{}, err
	}
	return bindingTokenToLark(row), nil
}

func (s *ChannelStore) ConsumeLarkBindingToken(ctx context.Context, tokenHash string) (db.LarkBindingToken, error) {
	row, err := s.Queries.ConsumeChannelBindingToken(ctx, tokenHash)
	if err != nil {
		return db.LarkBindingToken{}, err
	}
	return bindingTokenToLark(row), nil
}

// ---- outbound card ----

func (s *ChannelStore) GetLarkOutboundCardByTask(ctx context.Context, taskID pgtype.UUID) (db.LarkOutboundCardMessage, error) {
	row, err := s.Queries.GetChannelOutboundCardByTask(ctx, db.GetChannelOutboundCardByTaskParams{
		TaskID:      taskID,
		ChannelType: channelTypeFeishu,
	})
	if err != nil {
		return db.LarkOutboundCardMessage{}, err
	}
	return outboundCardToLark(row), nil
}

func (s *ChannelStore) CreateLarkOutboundCardMessage(ctx context.Context, arg db.CreateLarkOutboundCardMessageParams) (db.LarkOutboundCardMessage, error) {
	row, err := s.Queries.CreateChannelOutboundCardMessage(ctx, db.CreateChannelOutboundCardMessageParams{
		ChatSessionID:        arg.ChatSessionID,
		ChannelType:          channelTypeFeishu,
		ChannelChatID:        arg.LarkChatID,
		ChannelCardMessageID: arg.LarkCardMessageID,
		Status:               arg.Status,
		TaskID:               arg.TaskID,
	})
	if err != nil {
		return db.LarkOutboundCardMessage{}, err
	}
	return outboundCardToLark(row), nil
}

func (s *ChannelStore) UpdateLarkOutboundCardStatus(ctx context.Context, arg db.UpdateLarkOutboundCardStatusParams) error {
	return s.Queries.UpdateChannelOutboundCardStatus(ctx, db.UpdateChannelOutboundCardStatusParams{
		ID:     arg.ID,
		Status: arg.Status,
	})
}

// ---- row converters (channel_* row -> db.Lark* struct) ----

func channelInstallationToLark(row db.ChannelInstallation) (db.LarkInstallation, error) {
	inst, err := installationFromRow(row)
	if err != nil {
		return db.LarkInstallation{}, err
	}
	return db.LarkInstallation{
		ID:                 inst.ID,
		WorkspaceID:        inst.WorkspaceID,
		AgentID:            inst.AgentID,
		AppID:              inst.AppID,
		AppSecretEncrypted: inst.AppSecretEncrypted,
		TenantKey:          inst.TenantKey,
		BotOpenID:          inst.BotOpenID,
		InstallerUserID:    inst.InstallerUserID,
		Status:             inst.Status,
		WsLeaseToken:       inst.WsLeaseToken,
		WsLeaseExpiresAt:   inst.WsLeaseExpiresAt,
		InstalledAt:        inst.InstalledAt,
		CreatedAt:          inst.CreatedAt,
		UpdatedAt:          inst.UpdatedAt,
		BotUnionID:         inst.BotUnionID,
		Region:             inst.Region,
	}, nil
}

func channelInstallationsToLark(rows []db.ChannelInstallation) ([]db.LarkInstallation, error) {
	out := make([]db.LarkInstallation, len(rows))
	for i, row := range rows {
		conv, err := channelInstallationToLark(row)
		if err != nil {
			return nil, err
		}
		out[i] = conv
	}
	return out, nil
}

func channelUserBindingToLark(row db.ChannelUserBinding) (db.LarkUserBinding, error) {
	b, err := userBindingFromRow(row)
	if err != nil {
		return db.LarkUserBinding{}, err
	}
	return db.LarkUserBinding{
		ID:             b.ID,
		WorkspaceID:    b.WorkspaceID,
		MulticaUserID:  b.MulticaUserID,
		InstallationID: b.InstallationID,
		LarkOpenID:     b.ChannelUserID,
		UnionID:        b.UnionID,
		BoundAt:        b.BoundAt,
	}, nil
}

func larkChatSessionBinding(row db.ChannelChatSessionBinding) db.LarkChatSessionBinding {
	b := chatSessionBindingFromRow(row)
	return db.LarkChatSessionBinding{
		ID:                b.ID,
		ChatSessionID:     b.ChatSessionID,
		InstallationID:    b.InstallationID,
		LarkChatID:        b.ChannelChatID,
		LarkChatType:      b.ChatType,
		CreatedAt:         b.CreatedAt,
		LastLarkMessageID: b.LastMessageID,
		LastLarkThreadID:  b.LastThreadID,
	}
}

func dedupToLark(row db.ChannelInboundMessageDedup) db.LarkInboundMessageDedup {
	return db.LarkInboundMessageDedup{
		InstallationID: row.InstallationID,
		MessageID:      row.MessageID,
		ReceivedAt:     row.ReceivedAt,
		ProcessedAt:    row.ProcessedAt,
		ClaimToken:     row.ClaimToken,
	}
}

func bindingTokenToLark(row db.ChannelBindingToken) db.LarkBindingToken {
	return db.LarkBindingToken{
		TokenHash:      row.TokenHash,
		WorkspaceID:    row.WorkspaceID,
		InstallationID: row.InstallationID,
		LarkOpenID:     row.ChannelUserID,
		ExpiresAt:      row.ExpiresAt,
		ConsumedAt:     row.ConsumedAt,
		CreatedAt:      row.CreatedAt,
	}
}

func outboundCardToLark(row db.ChannelOutboundCardMessage) db.LarkOutboundCardMessage {
	return db.LarkOutboundCardMessage{
		ID:                row.ID,
		ChatSessionID:     row.ChatSessionID,
		TaskID:            row.TaskID,
		LarkChatID:        row.ChannelChatID,
		LarkCardMessageID: row.ChannelCardMessageID,
		Status:            row.Status,
		LastPatchedAt:     row.LastPatchedAt,
		CreatedAt:         row.CreatedAt,
	}
}
