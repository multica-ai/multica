package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// welcomeQueries loads installation and binding rows for enter_chat handling.
type welcomeQueries interface {
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	GetChannelUserBindingByUserID(ctx context.Context, arg db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error)
}

// welcomeBinder mints short-lived DM binding tokens at welcome time.
type welcomeBinder interface {
	Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, wecomUserID string) (BindingToken, error)
}

// onWelcome handles enter_chat on the dedicated welcome worker goroutine.
//
// This is the ONLY outbound path that bypasses channel_outbound_queue and the
// local RateGate (spec §5.3.2 welcome exception). The platform enforces a 5s
// reply deadline; Conn's welcome worker applies a 4s internal budget and sends
// through SendWelcome → high-priority writePump. Do not copy this pattern for
// ordinary proactive sends.
func (c *wecomChannel) onWelcome(ctx context.Context, wc WelcomeContext) {
	body, err := parseEventCallbackBody(wc.Frame.Body)
	if err != nil {
		c.logger.WarnContext(ctx, "wecom: enter_chat decode failed",
			"installation_id", c.installationID,
			"error", err,
		)
		return
	}

	if body.ChatType != ChatTypeSingle {
		c.logger.WarnContext(ctx, "wecom: ignoring non-single enter_chat",
			"installation_id", c.installationID,
			"chattype", body.ChatType,
			"msgid", body.MsgID,
		)
		c.metrics.RecordWelcomeSkippedNonSingle()
		return
	}

	wecomUserID := eventSenderUserID(body)
	if wecomUserID == "" {
		c.logger.WarnContext(ctx, "wecom: enter_chat missing sender userid",
			"installation_id", c.installationID,
			"msgid", body.MsgID,
		)
		return
	}

	markdown, err := buildWelcomeMarkdown(ctx, welcomeBuildInput{
		Locale:         c.locale,
		AppURL:         c.outbox.AppURL,
		InstallationID: c.installationID,
		WecomUserID:    wecomUserID,
		Queries:        c.outbox.Queries,
		Binding:        c.outbox.Binding,
	})
	if err != nil {
		c.logger.WarnContext(ctx, "wecom: build welcome failed",
			"installation_id", c.installationID,
			"wecom_user_id", wecomUserID,
			"error", err,
		)
		return
	}

	if err := wc.Reply(ctx, WelcomeMsgBody{
		MsgType:  "markdown",
		Markdown: &MarkdownBody{Content: markdown},
	}); err != nil {
		c.logger.WarnContext(ctx, "wecom: welcome reply failed",
			"installation_id", c.installationID,
			"wecom_user_id", wecomUserID,
			"error", err,
		)
		c.metrics.RecordWelcomeFailure()
		// Platform rate limits are logged only; welcome is never retried.
	}
}

func (c *wecomChannel) buildWelcomeMarkdown(ctx context.Context, wecomUserID string) (string, error) {
	return buildWelcomeMarkdown(ctx, welcomeBuildInput{
		Locale:         c.locale,
		AppURL:         c.outbox.AppURL,
		InstallationID: c.installationID,
		WecomUserID:    wecomUserID,
		Queries:        c.outbox.Queries,
		Binding:        c.outbox.Binding,
	})
}

type welcomeBuildInput struct {
	Locale         string
	AppURL         string
	InstallationID string
	WecomUserID    string
	Queries        welcomeQueries
	Binding        welcomeBinder
}

func buildWelcomeMarkdown(ctx context.Context, in welcomeBuildInput) (string, error) {
	if in.Queries == nil {
		return RenderWelcome(WelcomeInput{Locale: in.Locale, Bound: true})
	}

	installationID := util.MustParseUUID(in.InstallationID)
	_, err := in.Queries.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: installationID,
		ChannelUserID:  in.WecomUserID,
	})
	if err == nil {
		return RenderWelcome(WelcomeInput{Locale: in.Locale, Bound: true})
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("lookup binding: %w", err)
	}

	if in.Binding == nil {
		return "", errors.New("wecom: binding token service not configured")
	}

	inst, err := in.Queries.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          installationID,
		ChannelType: string(TypeWecom),
	})
	if err != nil {
		return "", fmt.Errorf("lookup installation: %w", err)
	}

	tok, err := in.Binding.Mint(ctx, inst.WorkspaceID, installationID, in.WecomUserID)
	if err != nil {
		return "", fmt.Errorf("mint binding token: %w", err)
	}

	return RenderWelcome(WelcomeInput{
		Locale:          in.Locale,
		AppURL:          in.AppURL,
		Bound:           false,
		BindingTokenRaw: tok.Raw,
	})
}

func parseEventCallbackBody(raw json.RawMessage) (EventCallbackBody, error) {
	if len(raw) == 0 {
		return EventCallbackBody{}, errors.New("empty event body")
	}
	var body EventCallbackBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return EventCallbackBody{}, fmt.Errorf("decode event body: %w", err)
	}
	if body.Event.EventType != EventTypeEnterChat {
		return EventCallbackBody{}, fmt.Errorf("unexpected eventtype %q", body.Event.EventType)
	}
	return body, nil
}

func eventSenderUserID(body EventCallbackBody) string {
	if body.From == nil {
		return ""
	}
	return body.From.UserID
}
