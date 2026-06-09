package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// OutcomeReplier reacts to the Dispatcher's verdict by posting the
// appropriate Lark-side reply card. This is the outbound half of the
// `EventEmitter` contract in hub.go: NeedsBinding sends the binding
// prompt to the sender's open_id, AgentOffline / AgentArchived send
// a status notice into the chat. OutcomeIngested is owned by the
// Patcher (task lifecycle); OutcomeDropped is silent.
//
// Reply is best-effort by design: a transient Lark outage MUST NOT
// fail the inbound pipeline (the message is already durable in
// chat_session by the time we get here for OutcomeIngested, and for
// the other outcomes there is no durable side effect to undo). Errors
// are logged and swallowed; the next inbound message for the same
// user retries the reply on its own.
type OutcomeReplier interface {
	Reply(ctx context.Context, inst db.LarkInstallation, msg InboundMessage, res DispatchResult)
}

// OutcomeReplierQueries is the narrow subset of *db.Queries the
// replier needs. Pinned via an interface so tests substitute a fake.
type OutcomeReplierQueries interface {
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
}

// noopReplier is the safe default when Lark is wired without an
// outbound APIClient (stub) or without a BindingTokenService. It
// logs each outcome that would have produced a reply so an operator
// can see the gap in production logs.
type noopReplier struct {
	log *slog.Logger
}

func (n *noopReplier) Reply(ctx context.Context, inst db.LarkInstallation, msg InboundMessage, res DispatchResult) {
	switch res.Outcome {
	case OutcomeNeedsBinding, OutcomeAgentOffline, OutcomeAgentArchived:
		n.log.Warn("lark outcome replier: outbound reply skipped (replier not wired)",
			"outcome", string(res.Outcome),
			"installation_id", uuidString(inst.ID),
			"chat_id", string(msg.ChatID),
			"open_id", string(msg.SenderOpenID),
		)
	}
}

// NewNoopOutcomeReplier returns the no-op replier. Used as the
// fallback when the production wiring is incomplete (e.g. stub
// APIClient, no binding token service).
func NewNoopOutcomeReplier(log *slog.Logger) OutcomeReplier {
	if log == nil {
		log = slog.Default()
	}
	return &noopReplier{log: log}
}

// LarkOutcomeReplier is the production OutcomeReplier. It composes:
//
//   - APIClient — to send the binding prompt card (open_id-targeted)
//     and the offline/archived notice cards (chat_id-targeted).
//   - BindingTokenService — to mint a one-shot binding token for the
//     NeedsBinding flow.
//   - CredentialsResolver — to decrypt app_secret per call (the
//     plaintext secret never lives on the in-memory installation row).
//   - OutcomeReplierQueries — for the agent name shown on cards.
//
// The replier is constructed once at boot and shared across the Hub's
// supervisor goroutines; all dependencies must be goroutine-safe
// (the standard implementations are).
type LarkOutcomeReplier struct {
	client       APIClient
	bindingSvc   *BindingTokenService
	credentials  CredentialsResolver
	queries      OutcomeReplierQueries
	publicURL    string // e.g. https://multica.example, trailing slash trimmed
	bindingPath  string // path component of the binding URL, default "/lark/bind"
	noticeHeader string // header text used by the offline/archived cards
	log          *slog.Logger
}

// OutcomeReplierConfig wires the production replier. PublicURL is the
// Multica HTTP host the user clicks into to redeem the binding token
// (e.g. https://multica.example); empty means the binding flow can
// only log the open_id, not produce a clickable card. The other
// fields default at construction.
type OutcomeReplierConfig struct {
	APIClient   APIClient
	BindingSvc  *BindingTokenService
	Credentials CredentialsResolver
	Queries     OutcomeReplierQueries
	PublicURL   string
	BindingPath string
	Logger      *slog.Logger
}

// NewLarkOutcomeReplier validates the configuration and returns the
// production replier. Missing dependencies fall back to noop so the
// boot path stays robust on partially-configured deployments.
func NewLarkOutcomeReplier(cfg OutcomeReplierConfig) OutcomeReplier {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	if cfg.APIClient == nil || cfg.BindingSvc == nil || cfg.Credentials == nil || cfg.Queries == nil {
		return NewNoopOutcomeReplier(log)
	}
	if !cfg.APIClient.IsConfigured() {
		log.Warn("lark outcome replier: APIClient.IsConfigured()=false; downgrading to noop replier")
		return NewNoopOutcomeReplier(log)
	}
	if cfg.PublicURL == "" {
		log.Warn("lark outcome replier: MULTICA_PUBLIC_URL not set; binding prompt CTA will not work")
	}
	bindingPath := cfg.BindingPath
	if bindingPath == "" {
		bindingPath = "/lark/bind"
	}
	if !strings.HasPrefix(bindingPath, "/") {
		bindingPath = "/" + bindingPath
	}
	return &LarkOutcomeReplier{
		client:       cfg.APIClient,
		bindingSvc:   cfg.BindingSvc,
		credentials:  cfg.Credentials,
		queries:      cfg.Queries,
		publicURL:    strings.TrimRight(cfg.PublicURL, "/"),
		bindingPath:  bindingPath,
		noticeHeader: "Multica",
		log:          log,
	}
}

// Reply implements OutcomeReplier. Reads carefully — the switch is
// the SOURCE OF TRUTH for which outcomes generate a reply, and a
// missing branch silently drops the user-visible side effect.
func (r *LarkOutcomeReplier) Reply(ctx context.Context, inst db.LarkInstallation, msg InboundMessage, res DispatchResult) {
	switch res.Outcome {
	case OutcomeNeedsBinding:
		if err := r.sendBindingPrompt(ctx, inst, res); err != nil {
			r.log.Warn("lark outcome replier: binding prompt failed",
				"installation_id", uuidString(inst.ID),
				"open_id", string(res.SenderOpenID),
				"err", err.Error(),
			)
		}
	case OutcomeAgentOffline:
		if err := r.sendChatNotice(ctx, inst, msg, agentOfflineCopy); err != nil {
			r.log.Warn("lark outcome replier: offline notice failed",
				"installation_id", uuidString(inst.ID),
				"chat_id", string(msg.ChatID),
				"err", err.Error(),
			)
		}
	case OutcomeAgentArchived:
		if err := r.sendChatNotice(ctx, inst, msg, agentArchivedCopy); err != nil {
			r.log.Warn("lark outcome replier: archived notice failed",
				"installation_id", uuidString(inst.ID),
				"chat_id", string(msg.ChatID),
				"err", err.Error(),
			)
		}
	case OutcomeIngested:
		// The agent's chat reply itself goes through the Patcher (text
		// message on ChatDone). But /issue does NOT block on the
		// agent — the user expects an immediate "Created [MUL-42]"
		// confirmation as soon as the issue row commits, separate
		// from whatever the agent eventually replies. Without this,
		// the user types `/issue fix login bug` and just sees the
		// agent's eventual response, with no clear signal that the
		// command itself was understood. Gate on IssueID.Valid so a
		// plain chat message (no /issue) stays silent here.
		if res.IssueID.Valid {
			if err := r.sendIssueCreated(ctx, inst, msg, res); err != nil {
				r.log.Warn("lark outcome replier: issue-created confirmation failed",
					"installation_id", uuidString(inst.ID),
					"chat_id", string(msg.ChatID),
					"issue_id", uuidString(res.IssueID),
					"err", err.Error(),
				)
			}
		}
	case OutcomeDropped:
		// OutcomeDropped is informational; no user-visible reply.
	}
}

func (r *LarkOutcomeReplier) sendBindingPrompt(ctx context.Context, inst db.LarkInstallation, res DispatchResult) error {
	if res.SenderOpenID == "" {
		return errors.New("missing sender open_id")
	}
	if r.publicURL == "" {
		return errors.New("public_url not configured")
	}
	token, err := r.bindingSvc.Mint(ctx, inst.WorkspaceID, inst.ID, res.SenderOpenID)
	if err != nil {
		return fmt.Errorf("mint binding token: %w", err)
	}
	bindURL := r.publicURL + r.bindingPath + "?token=" + url.QueryEscape(token.Raw)
	creds, err := r.installationCredentials(inst)
	if err != nil {
		return err
	}
	return r.client.SendBindingPromptCard(ctx, BindingPromptParams{
		InstallationID: creds,
		OpenID:         res.SenderOpenID,
		BindURL:        bindURL,
	})
}

// sendIssueCreated posts the "Created [MUL-42] <title>" confirmation
// as a plain text message. We deliberately send text rather than an
// interactive card so the confirmation flows inline with the rest of
// the Lark conversation — consistent with how chat replies render
// after MUL-2671's plain-text refactor. The link to Multica is
// included on its own line so Lark's auto-linker turns it into a
// tappable URL.
func (r *LarkOutcomeReplier) sendIssueCreated(ctx context.Context, inst db.LarkInstallation, msg InboundMessage, res DispatchResult) error {
	if msg.ChatID == "" {
		return errors.New("missing chat_id")
	}
	creds, err := r.installationCredentials(inst)
	if err != nil {
		return err
	}
	text := issueCreatedText(res, r.publicURL)
	if _, err := r.client.SendTextMessage(ctx, SendTextParams{
		InstallationID: creds,
		ChatID:         msg.ChatID,
		Text:           text,
	}); err != nil {
		return fmt.Errorf("send issue-created text: %w", err)
	}
	return nil
}

// issueCreatedText composes the user-facing confirmation. Identifier
// always wins over a bare number — DispatchResult.IssueIdentifier
// already encodes the workspace prefix when available. PublicURL is
// optional: when empty (self-host operators who haven't configured
// MULTICA_PUBLIC_URL) the message still confirms the issue, just
// without a deep link the user can tap.
func issueCreatedText(res DispatchResult, publicURL string) string {
	identifier := res.IssueIdentifier
	if identifier == "" {
		identifier = fmt.Sprintf("#%d", res.IssueNumber)
	}
	title := strings.TrimSpace(res.IssueTitle)
	var line string
	if title == "" {
		line = fmt.Sprintf("Created %s", identifier)
	} else {
		line = fmt.Sprintf("Created %s — %s", identifier, title)
	}
	if publicURL == "" {
		return line
	}
	return line + "\n" + strings.TrimRight(publicURL, "/") + "/issues/" + identifier
}

func (r *LarkOutcomeReplier) sendChatNotice(ctx context.Context, inst db.LarkInstallation, msg InboundMessage, body string) error {
	if msg.ChatID == "" {
		return errors.New("missing chat_id")
	}
	creds, err := r.installationCredentials(inst)
	if err != nil {
		return err
	}
	header := r.noticeHeader
	if agent, aerr := r.queries.GetAgent(ctx, inst.AgentID); aerr == nil && agent.Name != "" {
		header = agent.Name
	}
	cardJSON, err := renderNoticeCard(header, body)
	if err != nil {
		return fmt.Errorf("render notice card: %w", err)
	}
	if _, err := r.client.SendInteractiveCard(ctx, SendCardParams{
		InstallationID: creds,
		ChatID:         msg.ChatID,
		CardJSON:       cardJSON,
	}); err != nil {
		return fmt.Errorf("send notice card: %w", err)
	}
	return nil
}

func (r *LarkOutcomeReplier) installationCredentials(inst db.LarkInstallation) (InstallationCredentials, error) {
	secret, err := r.credentials.DecryptAppSecret(inst)
	if err != nil {
		return InstallationCredentials{}, fmt.Errorf("decrypt app_secret: %w", err)
	}
	creds := InstallationCredentials{
		AppID:     inst.AppID,
		AppSecret: secret,
		Region:    RegionOrDefault(inst.Region),
	}
	if inst.TenantKey.Valid {
		creds.TenantKey = inst.TenantKey.String
	}
	return creds, nil
}

// renderNoticeCard produces a minimal text-only interactive card for
// the offline / archived dispatch outcomes. Lark requires
// update_multi=true on every card we may patch later; these notice
// cards are one-shot, so update_multi is left false (the card stays
// as-is). Header / body match the Chinese voice used elsewhere in
// the integration.
func renderNoticeCard(header, body string) (string, error) {
	doc := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": "grey",
			"title":    map[string]any{"tag": "plain_text", "content": header},
		},
		"elements": []any{
			map[string]any{
				"tag": "div",
				"text": map[string]any{
					"tag":     "plain_text",
					"content": body,
				},
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// agentOfflineCopy and agentArchivedCopy are the user-visible Chinese
// strings for the two daemon/agent unavailability outcomes. They
// match the §4.6 design: an offline agent will run when the daemon
// comes back; an archived agent needs operator action.
const (
	agentOfflineCopy  = "Agent 当前离线，消息已记录。下次 daemon 上线后会自动继续处理。"
	agentArchivedCopy = "这个 Agent 已被归档，无法继续处理消息。请联系工作区管理员恢复或重新绑定。"
)
