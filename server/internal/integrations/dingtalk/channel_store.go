package dingtalk

// Channel-backed store for DingTalk bot installations.
//
// The generalized channel_* tables (migration 124) carry a channel_type
// discriminator plus a JSONB `config` blob for the platform-specific
// credentials, so DingTalk rows live alongside feishu/slack ones with no
// schema change. This file owns the one boundary where that JSONB is
// (de)serialized; the rest of the package works with the flat
// Installation domain struct.
//
// The dingtalk config blob:
//
//	installation: app_id (the client_id / AppKey the device flow minted),
//	              app_secret_encrypted (secretbox ciphertext, base64)
//
// `app_id` deliberately reuses the cross-channel key name (feishu and
// slack store their primary app identity under the same key) so the
// shared GetChannelInstallationByAppID query's config->>'app_id' lookup
// works uniformly.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// channelTypeDingTalk is the channel_type discriminator for every row
// this store reads or writes.
const channelTypeDingTalk = "dingtalk"

// Installation is the flat, dingtalk-shaped view of a
// channel_installation row. ClientID is what DingTalk's device flow
// calls client_id (the app's AppKey); it is stored under the config key
// "app_id" — see the file comment.
type Installation struct {
	ID                 pgtype.UUID
	WorkspaceID        pgtype.UUID
	AgentID            pgtype.UUID
	ClientID           string
	AppSecretEncrypted []byte
	InstallerUserID    pgtype.UUID
	Status             string
	InstalledAt        pgtype.Timestamptz
	CreatedAt          pgtype.Timestamptz
	UpdatedAt          pgtype.Timestamptz
}

// InstallationStatus mirrors the channel_installation status column
// values this package writes.
type InstallationStatus string

const (
	InstallationActive  InstallationStatus = "active"
	InstallationRevoked InstallationStatus = "revoked"
)

// ChannelStore wraps *db.Queries so the dingtalk package's DB seams
// resolve to channel_* rows scoped to channel_type = 'dingtalk'.
type ChannelStore struct {
	*db.Queries
}

// NewChannelStore wraps a *db.Queries.
func NewChannelStore(q *db.Queries) *ChannelStore {
	return &ChannelStore{Queries: q}
}

// dingtalkInstallConfig is the JSON shape of channel_installation.config
// for the dingtalk channel.
type dingtalkInstallConfig struct {
	AppID              string `json:"app_id"`
	AppSecretEncrypted string `json:"app_secret_encrypted,omitempty"`
}

// UpsertInstallationParams is the write shape for
// UpsertDingTalkInstallation. AppSecretEncrypted is secretbox
// ciphertext — sealing happens at the InstallationService boundary.
type UpsertInstallationParams struct {
	WorkspaceID        pgtype.UUID
	AgentID            pgtype.UUID
	ClientID           string
	AppSecretEncrypted []byte
	InstallerUserID    pgtype.UUID
}

func (s *ChannelStore) UpsertDingTalkInstallation(ctx context.Context, arg UpsertInstallationParams) (Installation, error) {
	cfg, err := encodeInstallConfig(Installation{
		ClientID:           arg.ClientID,
		AppSecretEncrypted: arg.AppSecretEncrypted,
	})
	if err != nil {
		return Installation{}, err
	}
	row, err := s.Queries.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		ChannelType:     channelTypeDingTalk,
		Config:          cfg,
		InstallerUserID: arg.InstallerUserID,
	})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

func (s *ChannelStore) GetDingTalkInstallationInWorkspace(ctx context.Context, id, workspaceID pgtype.UUID) (Installation, error) {
	row, err := s.Queries.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: workspaceID,
		ChannelType: channelTypeDingTalk,
	})
	if err != nil {
		return Installation{}, err
	}
	return installationFromRow(row)
}

func (s *ChannelStore) ListDingTalkInstallationsByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]Installation, error) {
	rows, err := s.Queries.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: workspaceID,
		ChannelType: channelTypeDingTalk,
	})
	if err != nil {
		return nil, err
	}
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

func (s *ChannelStore) SetDingTalkInstallationStatus(ctx context.Context, id pgtype.UUID, status InstallationStatus) error {
	return s.Queries.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     id,
		Status: string(status),
	})
}

// installationFromRow decodes a channel_installation row (flat columns +
// JSONB config) into the flat Installation domain struct.
func installationFromRow(row db.ChannelInstallation) (Installation, error) {
	var cfg dingtalkInstallConfig
	if len(row.Config) > 0 {
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			return Installation{}, fmt.Errorf("decode installation config: %w", err)
		}
	}
	secret, err := decodeSecret(cfg.AppSecretEncrypted)
	if err != nil {
		return Installation{}, fmt.Errorf("decode app_secret_encrypted: %w", err)
	}
	return Installation{
		ID:                 row.ID,
		WorkspaceID:        row.WorkspaceID,
		AgentID:            row.AgentID,
		ClientID:           cfg.AppID,
		AppSecretEncrypted: secret,
		InstallerUserID:    row.InstallerUserID,
		Status:             row.Status,
		InstalledAt:        row.InstalledAt,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}, nil
}

// encodeInstallConfig builds the channel_installation.config JSONB from
// the dingtalk fields of an Installation. The secret is emitted as
// unwrapped base64.
func encodeInstallConfig(inst Installation) ([]byte, error) {
	cfg := dingtalkInstallConfig{AppID: inst.ClientID}
	if len(inst.AppSecretEncrypted) > 0 {
		cfg.AppSecretEncrypted = base64.StdEncoding.EncodeToString(inst.AppSecretEncrypted)
	}
	return json.Marshal(cfg)
}

// decodeSecret base64-decodes the stored app secret ciphertext,
// tolerating MIME-wrapped input for symmetry with the lark store (rows
// written by Go are always unwrapped). An empty string yields nil.
func decodeSecret(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	if strings.ContainsAny(s, "\n\r \t") {
		s = strings.Map(func(r rune) rune {
			switch r {
			case '\n', '\r', ' ', '\t':
				return -1
			default:
				return r
			}
		}, s)
	}
	return base64.StdEncoding.DecodeString(s)
}
