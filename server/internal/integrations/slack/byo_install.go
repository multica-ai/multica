package slack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrInvalidBotToken / ErrInvalidAppToken are returned by RegisterBYO when a
// pasted token is malformed (wrong prefix, or an app token whose app id cannot
// be parsed). The handler maps them to 400 so the dialog can show a precise hint
// instead of a generic failure.
var (
	ErrInvalidBotToken = errors.New("slack: bot token must start with xoxb-")
	ErrInvalidAppToken = errors.New("slack: app-level token must start with xapp- and embed an app id")
	// ErrTokenAppMismatch is returned when the pasted bot token and app-level
	// token belong to DIFFERENT Slack apps. Persisting that pair would "connect"
	// but be broken: inbound arrives on the app token's socket (routed by its
	// app id) while mention detection + outbound use the bot token's identity.
	ErrTokenAppMismatch = errors.New("slack: the bot token and app-level token are from different Slack apps")
)

// RegisterBYOParams are the inputs for a bring-your-own-app install: the agent
// this bot represents, who is installing, and the two tokens the user pasted
// from their own Slack app.
type RegisterBYOParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
	BotToken    string // xoxb-… — outbound Web API (chat.postMessage)
	AppToken    string // xapp-… — this app's OWN Socket Mode connection (inbound)
}

// RegisterBYO installs a user-supplied ("bring your own") Slack app for an agent.
// The user creates their own Slack app, installs it to their workspace, and
// pastes its bot token (xoxb-) + app-level token (xapp-). There is NO OAuth code
// exchange: we validate the bot token live via auth.test (which also yields the
// team id + bot user id), prove the bot + app tokens belong to the SAME app,
// parse the real Slack app id out of the app-level token, encrypt BOTH tokens at
// rest, and persist the installation.
//
// Because each BYO app is a distinct Slack app — a distinct bot identity — the
// SAME Slack workspace can host several of them, one per agent. The stored
// config carries the real app id for inbound routing; persistInstall keys the
// row by (workspace, agent) and refuses the pair if that app id is already
// connected to another agent/workspace. The dedicated Socket Mode connection
// that consumes the stored app token lives in slack_channel.go; this method
// only persists the installation.
func (s *InstallService) RegisterBYO(ctx context.Context, p RegisterBYOParams) (db.ChannelInstallation, error) {
	botToken := strings.TrimSpace(p.BotToken)
	appToken := strings.TrimSpace(p.AppToken)
	if !strings.HasPrefix(botToken, "xoxb-") {
		return db.ChannelInstallation{}, ErrInvalidBotToken
	}
	appID, err := parseSlackAppID(appToken)
	if err != nil {
		return db.ChannelInstallation{}, err
	}

	// Validate the bot token live and learn the team + bot user id. auth.test
	// authenticates with the bot token and returns the bot's OWN user id, which
	// is the @-mention identity inbound translation strips.
	auth, err := s.authTest(ctx, botToken)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("slack auth.test: %w", err)
	}
	if auth.TeamID == "" || auth.UserID == "" || auth.BotID == "" {
		return db.ChannelInstallation{}, errors.New("slack auth.test: response missing team_id / user_id / bot_id")
	}

	// Prove the two tokens belong to the SAME Slack app: resolve the bot's
	// OWNING app id (bots.info on the bot id auth.test returned) and require it to
	// equal the app id embedded in the app-level token. Without this, pasting app
	// A's bot token with app B's app token would "connect" but be broken —
	// inbound arrives on app B's socket (routed by api_app_id=B) while mention
	// detection + outbound use app A's bot identity / token (Niko review).
	botAppID, err := s.botAppID(ctx, botToken, auth.BotID)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("slack bots.info: %w", err)
	}
	if botAppID != appID {
		return db.ChannelInstallation{}, ErrTokenAppMismatch
	}

	// Validate the app-level token is live (Socket Mode can actually open) so we
	// never persist a token that will silently never receive events.
	if err := s.validateAppToken(ctx, appToken); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("slack apps.connections.open: %w", err)
	}

	sealedBot, err := s.box.Seal([]byte(botToken))
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encrypt slack bot token: %w", err)
	}
	sealedApp, err := s.box.Seal([]byte(appToken))
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encrypt slack app token: %w", err)
	}
	cfgJSON, err := json.Marshal(installConfig{
		AppID:             appID,
		TeamID:            auth.TeamID,
		BotUserID:         auth.UserID,
		BotTokenEncrypted: base64.StdEncoding.EncodeToString(sealedBot),
		AppTokenEncrypted: base64.StdEncoding.EncodeToString(sealedApp),
	})
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encode slack installation config: %w", err)
	}

	// Persist one bot per agent (the row is keyed by workspace + agent). The
	// stored config carries the real app id for inbound routing; persistInstall
	// refuses the pair if that app is already connected to another agent/workspace.
	return s.persistInstall(ctx, installPersist{
		wsID:        p.WorkspaceID,
		agentID:     p.AgentID,
		installerID: p.InitiatorID,
		configJSON:  cfgJSON,
	})
}

// slackOpts builds the slack.Client options shared by the install-time Web API
// calls, honoring the apiURL override so tests can point them at an httptest
// server. The Slack SDK appends the method name to the endpoint, so the base
// must end in a slash. A fresh slice is returned each call (safe to append to).
func (s *InstallService) slackOpts() []slack.Option {
	httpClient := s.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	opts := []slack.Option{slack.OptionHTTPClient(httpClient)}
	if s.apiURL != "" {
		base := s.apiURL
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		opts = append(opts, slack.OptionAPIURL(base))
	}
	return opts
}

// authTest calls Slack auth.test with the bot token: validates it and returns
// the team id, the bot's own user id, and the bot id (for the bots.info lookup).
func (s *InstallService) authTest(ctx context.Context, botToken string) (*slack.AuthTestResponse, error) {
	return slack.New(botToken, s.slackOpts()...).AuthTestContext(ctx)
}

// botAppID resolves the Slack app that OWNS the bot, via bots.info on the bot id
// from auth.test. It is the only token→app_id path for a bot token, so it is how
// we prove the pasted bot + app tokens belong to the same app.
func (s *InstallService) botAppID(ctx context.Context, botToken, botID string) (string, error) {
	bot, err := slack.New(botToken, s.slackOpts()...).GetBotInfoContext(ctx, slack.GetBotInfoParameters{Bot: botID})
	if err != nil {
		return "", err
	}
	return bot.AppID, nil
}

// validateAppToken confirms the app-level token can open a Socket Mode
// connection (apps.connections.open) — a live check that the xapp is valid for
// THIS app, so we never store a token that will silently receive nothing.
func (s *InstallService) validateAppToken(ctx context.Context, appToken string) error {
	api := slack.New("", append(s.slackOpts(), slack.OptionAppLevelToken(appToken))...)
	_, _, err := api.StartSocketModeContext(ctx)
	return err
}

// parseSlackAppID extracts the real Slack app id from an app-level token. The
// token format is `xapp-1-<APP_ID>-<gen>-<secret>` (e.g. xapp-1-A0BCXGVCS7R-…),
// so the app id is the third dash-segment. It is the per-app storage / routing
// key that lets multiple BYO apps coexist in one Slack workspace.
func parseSlackAppID(appToken string) (string, error) {
	if !strings.HasPrefix(appToken, "xapp-") {
		return "", ErrInvalidAppToken
	}
	parts := strings.SplitN(appToken, "-", 5)
	if len(parts) < 4 || parts[2] == "" || !strings.HasPrefix(parts[2], "A") {
		return "", ErrInvalidAppToken
	}
	return parts[2], nil
}
