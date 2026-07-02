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

// RegisterBYO installs a user-supplied ("bring your own") DingTalk robot for an
// agent. The user creates their own DingTalk Stream-mode robot and pastes its
// AppKey (client id) + AppSecret (client secret). There is NO OAuth code
// exchange: we validate the credentials live by minting an access_token (which
// proves the AppKey/AppSecret pair is valid), encrypt the AppSecret at rest, and
// persist the installation.
//
// Because each BYO robot is a distinct DingTalk app — a distinct bot identity —
// the SAME DingTalk organization can host several of them, one per agent. The
// stored config carries the AppKey as the routing key (config->>'app_id', equal
// to the inbound event's robotCode for a Stream-mode robot); persistInstall keys
// the row by (workspace, agent), reclaims a DEAD prior binding of that AppKey
// (orphaned, or revoked in the SAME workspace) so the robot can move to this
// agent, and refuses a LIVE owner — an active install, an archived agent, or
// another workspace's revoked binding. The corp_id is left empty here and
// backfilled from the first inbound event (chatbotCorpId), since the
// AppKey/AppSecret alone do not reveal it. The dedicated Stream connection that
// consumes the stored credentials lives in dingtalk_channel.go; this method only
// persists the installation.
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
	if _, _, err := fetchAccessToken(ctx, s.httpClient, s.apiBase, appKey, appSecret); err != nil {
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

	// Persist one bot per agent (the row is keyed by workspace + agent). The
	// stored config carries the AppKey for inbound routing; persistInstall reclaims
	// a DEAD prior binding of that AppKey (orphaned, or revoked in the SAME
	// workspace) so the robot can move to this agent, and refuses a LIVE owner.
	return s.persistInstall(ctx, installPersist{
		wsID:        p.WorkspaceID,
		agentID:     p.AgentID,
		installerID: p.InitiatorID,
		appID:       appKey,
		configJSON:  cfgJSON,
	})
}
