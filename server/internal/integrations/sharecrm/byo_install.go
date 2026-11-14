package sharecrm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	ErrInvalidAppID         = errors.New("sharecrm: appId is required")
	ErrInvalidAppSecret     = errors.New("sharecrm: appSecret is required")
	ErrCredentialValidation = errors.New("sharecrm: could not validate credentials")
)

// RegisterBYOParams are the inputs for a bring-your-own Gateway install.
type RegisterBYOParams struct {
	WorkspaceID    pgtype.UUID
	AgentID        pgtype.UUID
	InitiatorID    pgtype.UUID
	AppID          string
	AppSecret      string
	GatewayBaseURL string // optional; empty → DefaultGatewayBaseURL
}

// RegisterBYO validates credentials live, encrypts the secret, and persists.
func (s *InstallService) RegisterBYO(ctx context.Context, p RegisterBYOParams) (db.ChannelInstallation, error) {
	appID := strings.TrimSpace(p.AppID)
	appSecret := strings.TrimSpace(p.AppSecret)
	if appID == "" {
		return db.ChannelInstallation{}, ErrInvalidAppID
	}
	if appSecret == "" {
		return db.ChannelInstallation{}, ErrInvalidAppSecret
	}
	baseURL := strings.TrimRight(strings.TrimSpace(p.GatewayBaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultGatewayBaseURL
	}

	validationCtx, cancel := context.WithTimeout(ctx, s.validationTimeout)
	defer cancel()
	if _, _, err := FetchAccessToken(validationCtx, s.httpClient, baseURL, appID, appSecret); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("%w: %v", ErrCredentialValidation, err)
	}

	sealedSecret, err := s.box.Seal([]byte(appSecret))
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encrypt sharecrm app secret: %w", err)
	}
	cfg := installConfig{
		AppID:              appID,
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealedSecret),
	}
	if baseURL != DefaultGatewayBaseURL {
		cfg.GatewayBaseURL = baseURL
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encode sharecrm installation config: %w", err)
	}

	return s.persistInstall(ctx, installPersist{
		wsID:        p.WorkspaceID,
		agentID:     p.AgentID,
		installerID: p.InitiatorID,
		appIDKey:    appID,
		configJSON:  cfgJSON,
	})
}
