// Package weixin implements the personal Weixin iLink Bot channel.
package weixin

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const TypeWeixin channel.Type = "weixin"
const channelTypeWeixin = string(TypeWeixin)

type InstallationStatus string

const (
	InstallationActive  InstallationStatus = "active"
	InstallationRevoked InstallationStatus = "revoked"
)

// Installation is a personal Weixin bot bound to one Multica agent.
// TokenEncrypted always contains a secretbox-sealed iLink bot token.
type Installation struct {
	ID              pgtype.UUID
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	InstallerUserID pgtype.UUID
	Status          InstallationStatus
	BotID           string
	WeixinUserID    string
	BaseURL         string
	TokenEncrypted  []byte
}

type installConfig struct {
	AppID          string `json:"app_id"`
	BotID          string `json:"bot_id"`
	WeixinUserID   string `json:"weixin_user_id"`
	BaseURL        string `json:"base_url"`
	TokenEncrypted []byte `json:"token_encrypted"`
}

func encodeInstallConfig(inst Installation) ([]byte, error) {
	if inst.BotID == "" || inst.WeixinUserID == "" || inst.BaseURL == "" || len(inst.TokenEncrypted) == 0 {
		return nil, errors.New("weixin: incomplete installation config")
	}
	return json.Marshal(installConfig{
		AppID:          inst.BotID,
		BotID:          inst.BotID,
		WeixinUserID:   inst.WeixinUserID,
		BaseURL:        inst.BaseURL,
		TokenEncrypted: inst.TokenEncrypted,
	})
}

func installationFromRow(row db.ChannelInstallation) (Installation, error) {
	var cfg installConfig
	if err := json.Unmarshal(row.Config, &cfg); err != nil {
		return Installation{}, fmt.Errorf("weixin: decode installation config: %w", err)
	}
	return Installation{
		ID:              row.ID,
		WorkspaceID:     row.WorkspaceID,
		AgentID:         row.AgentID,
		InstallerUserID: row.InstallerUserID,
		Status:          InstallationStatus(row.Status),
		BotID:           cfg.BotID,
		WeixinUserID:    cfg.WeixinUserID,
		BaseURL:         cfg.BaseURL,
		TokenEncrypted:  cfg.TokenEncrypted,
	}, nil
}
