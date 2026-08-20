package dingtalk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
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

// RegisterBYOParams are the inputs for a bring-your-own-app install. TargetID is
// the stable Agent or Squad destination; AgentID is the executable Agent (the
// current Leader for a Squad) validated by the handler.
type RegisterBYOParams struct {
	WorkspaceID pgtype.UUID
	TargetType  engine.TargetType
	TargetID    pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
	AppKey      string // client id — robotCode + access-token mint
	AppSecret   string // client secret — access-token mint (encrypted at rest)
}

// RegisterBYO installs a user-supplied ("bring your own") DingTalk robot for an
// target. The user creates their own DingTalk Stream-mode robot and pastes its
// AppKey (client id) + AppSecret (client secret). There is NO OAuth code
// exchange: we validate the credentials live by minting an access_token (which
// proves the AppKey/AppSecret pair is valid), encrypt the AppSecret at rest, and
// persist the installation.
//
// Because each BYO robot is a distinct DingTalk app — a distinct bot identity —
// the SAME DingTalk organization can host several of them, one per agent. The
// stored config carries the AppKey as the routing key (config->>'app_id', equal
// to the inbound event's robotCode for a Stream-mode robot); persistInstall keys
// the row by (workspace, agent), reclaims a DEAD prior owner of that AppKey
// (a revoked placeholder, or an orphan whose workspace/agent was deleted) so the
// robot can move to this agent, and refuses a LIVE owner with an accurate
// conflict sentinel. The dedicated Stream connection that consumes the stored
// credentials lives in dingtalk_channel.go; this method only persists the
// installation.
func (s *InstallService) RegisterBYO(ctx context.Context, p RegisterBYOParams) (Installation, error) {
	appKey := strings.TrimSpace(p.AppKey)
	appSecret := strings.TrimSpace(p.AppSecret)
	if appKey == "" {
		return Installation{}, ErrInvalidAppKey
	}
	if appSecret == "" {
		return Installation{}, ErrInvalidAppSecret
	}

	// Validate the credentials live: a successful access_token mint proves the
	// AppKey/AppSecret pair is real and installed. The robotCode of a Stream-mode
	// robot equals the AppKey, so no separate identity lookup is needed.
	validationCtx, cancel := context.WithTimeout(ctx, s.validationTimeout)
	defer cancel()
	if _, _, err := fetchAccessToken(validationCtx, s.httpClient, s.apiBase, appKey, appSecret); err != nil {
		return Installation{}, fmt.Errorf("%w: %v", ErrCredentialValidation, err)
	}

	sealedSecret, err := s.box.Seal([]byte(appSecret))
	if err != nil {
		return Installation{}, fmt.Errorf("encrypt dingtalk app secret: %w", err)
	}
	cfgJSON, err := json.Marshal(installConfig{
		AppID:              appKey,
		RobotCode:          appKey,
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealedSecret),
	})
	if err != nil {
		return Installation{}, fmt.Errorf("encode dingtalk installation config: %w", err)
	}

	// Persist one installation per default agent (the row is keyed by workspace
	// + agent). Group routes can target other agents without another Stream
	// connection. The
	// stored config carries the AppKey for inbound routing; persistInstall
	// reclaims a DEAD prior owner of that AppKey so the robot can move to this
	// agent, and refuses a LIVE owner with an accurate conflict sentinel.
	return s.persistInstall(ctx, installPersist{
		wsID:        p.WorkspaceID,
		targetType:  p.TargetType,
		targetID:    p.TargetID,
		agentID:     p.AgentID,
		installerID: p.InitiatorID,
		appIDKey:    appKey,
		configJSON:  cfgJSON,
	})
}
