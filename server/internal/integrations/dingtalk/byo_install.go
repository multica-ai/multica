package dingtalk

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

// ErrInvalidAppKey / ErrInvalidAppSecret are returned by RegisterBYO when a
// pasted credential is empty. The handler maps them to 400 so the dialog can
// show a precise hint instead of a generic failure.
var (
	ErrInvalidAppKey    = errors.New("dingtalk: AppKey (client id) is required")
	ErrInvalidAppSecret = errors.New("dingtalk: AppSecret (client secret) is required")
	// ErrCredentialValidation wraps a live access-token mint that rejected the
	// pasted AppKey/AppSecret. It is a user error (bad credentials), so the
	// handler maps it to 400 — unlike an internal encrypt/persist failure, which
	// must surface as 500.
	ErrCredentialValidation = errors.New("dingtalk: could not validate credentials")
)

// RegisterBYOParams are the inputs for a bring-your-own-app install: the agent
// this bot represents, who is installing, and the two credentials the user
// pasted from their own DingTalk Stream-mode robot.
type RegisterBYOParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
	AppKey      string // client id — robotCode + access-token mint
	AppSecret   string // client secret — access-token mint (encrypted at rest)
}

// RegisterBYO authorizes a user-supplied ("bring your own") DingTalk robot in a
// workspace and assigns its default agent. The user creates their own Stream-mode robot and pastes its
// AppKey (client id) + AppSecret (client secret). There is NO OAuth code
// exchange: we validate the credentials live by minting an access_token (which
// proves the AppKey/AppSecret pair is valid), encrypt the AppSecret at rest, and
// persist the global connector and workspace-local grant.
//
// AppKey is the global connector identity. Reusing the same AppKey in another
// workspace adds or reactivates a grant and rotates the shared encrypted
// credentials; it does not create another Stream connection. Group and direct
// routes still resolve to exactly one workspace and agent for each message.
func (s *InstallService) RegisterBYO(ctx context.Context, p RegisterBYOParams) (db.ChannelInstallation, error) {
	appKey := strings.TrimSpace(p.AppKey)
	appSecret := strings.TrimSpace(p.AppSecret)
	if appKey == "" {
		return db.ChannelInstallation{}, ErrInvalidAppKey
	}
	if appSecret == "" {
		return db.ChannelInstallation{}, ErrInvalidAppSecret
	}

	// Validate the credentials live: a successful access_token mint proves the
	// AppKey/AppSecret pair is real and installed. The robotCode of a Stream-mode
	// robot equals the AppKey, so no separate identity lookup is needed.
	validationCtx, cancel := context.WithTimeout(ctx, s.validationTimeout)
	defer cancel()
	if _, _, err := fetchAccessToken(validationCtx, s.httpClient, s.apiBase, appKey, appSecret); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("%w: %v", ErrCredentialValidation, err)
	}

	sealedSecret, err := s.box.Seal([]byte(appSecret))
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encrypt dingtalk app secret: %w", err)
	}
	cfgJSON, err := json.Marshal(installConfig{
		AppID:              appKey,
		RobotCode:          appKey,
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealedSecret),
	})
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encode dingtalk installation config: %w", err)
	}

	// Persist one connector per AppKey and one workspace-local grant. Group
	// routes can target another agent without opening another Stream connection.
	return s.persistInstall(ctx, installPersist{
		wsID:        p.WorkspaceID,
		agentID:     p.AgentID,
		installerID: p.InitiatorID,
		appIDKey:    appKey,
		configJSON:  cfgJSON,
	})
}
