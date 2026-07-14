package lark

import (
	"context"
	"errors"
	"log/slog"
)

// APIClient is the narrow surface this package needs from the Lark Open
// Platform HTTP API. It is intentionally defined here (rather than
// taken from a vendor SDK) so the rest of the package can be built and
// unit-tested without dragging Lark's transport into every test, and
// so we can swap implementations (real SDK, stub, fake) without
// touching call sites.
//
// All methods are scoped to a single installation — the caller has
// already authenticated the installation row and decrypted its
// app_secret. The client never reads `lark_installation` itself.
type APIClient interface {
	// IsConfigured reports whether this APIClient can reach Lark over
	// the network. It is the "HTTP outbound is wired" signal: the stub
	// returns false; the real Lark HTTP client returns true once
	// instantiated. Handlers consult this when deciding whether to
	// surface install / management UI that needs to talk to Lark.
	IsConfigured() bool

	// SendInteractiveCard posts an interactive card into a Lark chat
	// and returns Lark's message_id for the card. The patcher persists
	// this id in lark_outbound_card_message so subsequent patches can
	// target the same card.
	SendInteractiveCard(ctx context.Context, p SendCardParams) (string, error)

	// PatchInteractiveCard replaces the body of a previously-sent card.
	// The throttling decision belongs to the caller; this method just
	// performs the network call.
	PatchInteractiveCard(ctx context.Context, p PatchCardParams) error

	// SendTextMessage posts a plain text message into a Lark chat.
	// Used for the agent's chat reply when the body has no markdown
	// syntax — short prose / acknowledgments / pings. A plain text
	// bubble feels like a normal IM message; we deliberately keep
	// this path even after adding the markdown card variant because
	// wrapping a one-liner "Hello!" inside a card just adds visual
	// chrome the user doesn't want.
	SendTextMessage(ctx context.Context, p SendTextParams) (string, error)

	// SendDirectTextMessage posts a plain text message directly to a
	// Lark user open_id. Inbox notification delivery uses this path so
	// personal notifications do not have to land in a shared chat.
	SendDirectTextMessage(ctx context.Context, p SendDirectTextParams) (string, error)

	// SendMarkdownCard posts the agent's reply as a Lark interactive
	// card (schema 2.0) with a single `tag: "markdown"` body element.
	// This is the path the chat-reply router takes when the body
	// contains markdown syntax (fenced code blocks, headings, lists,
	// tables, etc.) — Lark renders the markdown into formatted text
	// rather than leaving raw `**bold**` / `# heading` characters in
	// the user's transcript. Returns the card's message_id.
	SendMarkdownCard(ctx context.Context, p SendMarkdownCardParams) (string, error)

	// SendBindingPromptCard is the dedicated "you need to bind"
	// outbound. Kept separate from SendInteractiveCard so the
	// abstraction stays stable when the production card template
	// changes — call sites in identity check don't have to know about
	// Lark's card schema.
	SendBindingPromptCard(ctx context.Context, p BindingPromptParams) error

	// GetBotInfo returns the Bot's per-installation `open_id` (the
	// `bot_open_id` we persist on lark_installation). RegistrationService
	// is the only caller — after the device-flow registration returns
	// fresh `client_id` / `client_secret`, the service mints a
	// tenant_access_token with those creds and calls
	// /open-apis/bot/v3/info to learn the Bot's identity. The result
	// is then frozen into lark_installation alongside the app_id /
	// app_secret in the same transaction as the installer-bind.
	GetBotInfo(ctx context.Context, creds InstallationCredentials) (BotInfo, error)

	// GetMessage fetches a message by id via
	// GET /open-apis/im/v1/messages/{message_id}. Lark always returns an
	// ARRAY (data.items[]): for a normal message exactly one element;
	// for a `merge_forward` message the first element is the forward
	// sentinel and the remaining elements are the bundled child messages
	// (each a normal typed message linked back by upper_message_id). The
	// inbound enricher relies on both shapes: items[0] for a quoted-reply
	// parent, items[1:] for a forwarded transcript. Returning the raw
	// slice keeps this method a thin transport adapter — flattening and
	// block assembly are the enricher's job.
	GetMessage(ctx context.Context, creds InstallationCredentials, messageID string) ([]LarkMessage, error)

	// ListChatMessages fetches the most recent messages in a single chat
	// via GET /open-apis/im/v1/messages?container_id_type=chat. It powers
	// the group-context prefetch: when a user @-mentions the Bot in a busy
	// group, the enricher pulls a bounded window of surrounding messages
	// so the agent sees the conversation, not just the one @-ed line.
	//
	// Results come back newest-first (sort_type=ByCreateTimeDesc), capped
	// at p.PageSize (Lark hard-caps a page at 50); the caller orders and
	// trims for rendering. Only a single page is fetched — pagination is
	// deliberately not exposed so the inbound ACK path's HTTP fan-out
	// stays a single round-trip. Like GetMessage, this is a thin transport
	// adapter: flattening and block assembly are the enricher's job.
	ListChatMessages(ctx context.Context, creds InstallationCredentials, p ListMessagesParams) ([]LarkMessage, error)

	// BatchGetUsers resolves a set of user open_ids to their display names
	// via GET /open-apis/contact/v3/users/batch. The enricher uses it to
	// label recent-context / quoted / forwarded speakers (and the sender
	// who @-mentioned the Bot) with real names instead of positional
	// "User 1 / User 2". Returns an open_id -> name map; ids the API does
	// not return (restricted contact scope, deactivated user, …) are
	// simply absent from the map, and the caller falls back to a
	// positional label. openIDs beyond Lark's 50-per-call cap are dropped
	// by the client.
	BatchGetUsers(ctx context.Context, creds InstallationCredentials, openIDs []string) (map[string]string, error)

	// AddMessageReaction adds an emoji reaction to an existing message.
	// The standard use-case is the "Typing" indicator that signals the
	// Bot is processing the user's message. Returns the reaction_id Lark
	// assigns so it can be removed later.
	AddMessageReaction(ctx context.Context, p AddReactionParams) (string, error)

	// DeleteMessageReaction removes a previously-added reaction from a
	// message. This is the cleanup half of the typing-indicator lifecycle.
	DeleteMessageReaction(ctx context.Context, p DeleteReactionParams) error
}

// ListMessagesParams selects a bounded, recent window of messages in a
// single Lark chat for the group-context prefetch. Only the fields the
// enricher needs today are exposed (ChatID, PageSize, EndTime);
// start_time and page_token are intentionally omitted until a caller
// needs them.
type ListMessagesParams struct {
	ChatID ChatID
	// PageSize is how many of the most-recent messages to fetch. The
	// client clamps it into Lark's valid 1..50 range.
	PageSize int
	// EndTime, when > 0, caps the window to messages created at or before
	// this Unix timestamp in SECONDS (Lark's end_time is second-, not
	// millisecond-, granularity). The enricher sets it to the trigger
	// message's time so the prefetch is anchored to the @-mention moment
	// rather than whatever is newest by the time the fetch runs.
	EndTime int64
}

// LarkMessage is the normalized slice of an IM v1 message item the
// enricher needs. Body.content is passed through raw (still the
// JSON-encoded, msg_type-specific string Lark double-encodes) so the
// flattener — not the transport client — owns content interpretation.
type LarkMessage struct {
	MessageID      string
	MessageType    string // Lark `msg_type`: text / post / image / merge_forward / …
	Content        string // raw body.content (a JSON-encoded string)
	SenderID       string // sender.id (open_id for users, app_id for apps)
	SenderType     string // sender.sender_type: user / app / anonymous / …
	CreateTime     string // epoch milliseconds, as Lark returns it (a string)
	ParentID       string
	RootID         string
	UpperMessageID string // the merge_forward parent a child hangs under
	Deleted        bool
	Mentions       []LarkMessageMention
}

// LarkMessageMention mirrors a mentions[] entry on the IM REST item
// shape. Note this differs from the WS receive event's mention shape:
// here `id` is a bare open_id string, not a nested {open_id, union_id,
// user_id} object.
type LarkMessageMention struct {
	Key  string // e.g. "@_user_1"
	ID   string // open_id
	Name string // display name (may be empty)
}

// BotInfo is the slice of /open-apis/bot/v3/info (+ a follow-up
// /open-apis/contact/v3/users lookup for the union_id) we care about:
// the Bot's per-installation `open_id` and its stable `union_id`.
//
// Both identifiers are persisted on lark_installation:
//
//   - `open_id` is the per-app Lark identifier; it is what /bot/v3/info
//     returns and what the OUTBOUND send paths use to address a user.
//
//   - `union_id` is the cross-app stable identifier scoped to the Lark
//     tenant. It is the only field that is consistent across the two
//     WS perspectives in a multi-bot group chat — see MUL-2671 group-
//     @-mention triage. The decoder matches inbound `mentions[].id`
//     against `union_id` so the right bot's supervisor handles the
//     event when several bots are bound to the same group.
//
// Everything else /bot/v3/info returns (display name, avatar,
// activate_status, ip_white_list) is intentionally dropped — those
// can be re-fetched downstream from the bot_open_id if a UI needs
// them, and freezing them in our schema would create a drift surface
// every time the operator edits the Bot on Lark's side.
type BotInfo struct {
	OpenID  OpenID
	UnionID string
}

// SendCardParams is the input shape for posting a fresh card.
type SendCardParams struct {
	InstallationID InstallationCredentials
	ChatID         ChatID
	// CardJSON is the raw Lark interactive card JSON body. We pass it
	// through opaque so the card-template package can evolve without
	// dragging this transport interface along.
	CardJSON string
}

// PatchCardParams is the input shape for updating an existing card.
type PatchCardParams struct {
	InstallationID    InstallationCredentials
	LarkCardMessageID string
	CardJSON          string
}

// SendTextParams is the input shape for posting a plain text message.
// Text is sent verbatim to Lark; the client handles JSON encoding of
// the `{"text": "..."}` content envelope Lark requires.
type SendTextParams struct {
	InstallationID InstallationCredentials
	ChatID         ChatID
	Text           string
}

// SendDirectTextParams is the input shape for posting a plain text direct
// message to a Lark user open_id.
type SendDirectTextParams struct {
	InstallationID InstallationCredentials
	OpenID         OpenID
	Text           string
}

// SendMarkdownCardParams is the input shape for posting an agent
// reply as a Lark interactive card with a markdown body element.
// Markdown is forwarded to Lark verbatim; the client builds the
// schema-2.0 card envelope around it.
type SendMarkdownCardParams struct {
	InstallationID InstallationCredentials
	ChatID         ChatID
	// Markdown is the body. Lark schema-2.0 markdown supports GFM-ish:
	// **bold**, *italic*, `inline code`, fenced code blocks, headings,
	// ordered + unordered lists, links, tables, blockquotes, separators.
	Markdown string
	// Summary, when non-empty, is rendered as the single-line preview
	// Lark shows in the chat list / desktop notification. Empty falls
	// back to whatever Lark derives from the body.
	Summary string
}

// BindingPromptParams carries the data needed to render and send the
// member-binding prompt card (single CTA: open the binding URL).
type BindingPromptParams struct {
	InstallationID InstallationCredentials
	OpenID         OpenID
	// BindURL is the absolute URL the user clicks. The token is
	// embedded in the URL by the caller; the client never sees it.
	BindURL string
}

// AddReactionParams is the input shape for adding an emoji reaction to
// a message.
type AddReactionParams struct {
	InstallationID InstallationCredentials
	MessageID      string
	EmojiType      string
}

// DeleteReactionParams is the input shape for removing a previously-added
// reaction from a message.
type DeleteReactionParams struct {
	InstallationID InstallationCredentials
	MessageID      string
	ReactionID     string
}

// InstallationCredentials is the per-installation transport context the
// client needs to authenticate against Lark on behalf of a workspace's
// bot. Passing these explicitly to each call (rather than constructing
// per-installation clients) keeps lifecycle simple: the hub decrypts
// app_secret once and reuses the struct for every outbound call.
//
// The plaintext app_secret lives inside this struct exactly while a
// call is in flight; callers MUST NOT log or persist it.
type InstallationCredentials struct {
	AppID     string
	AppSecret string
	TenantKey string
	// Region selects the Lark open-platform host (Feishu mainland vs
	// Lark international) for every call made with these credentials.
	// Empty defaults to Feishu. Credential-build sites copy it from
	// lark_installation.region; the device-flow installer sets it from
	// the auto-detected tenant. This is what lets one deployment serve
	// both clouds — see http_client.go resolveBaseURL and
	// ws_endpoint.go Endpoint.
	Region Region
}

// ErrAPIClientNotConfigured is returned by the stub client to signal
// that a real Lark client has not been wired in yet. Call sites SHOULD
// treat this as an expected condition on self-host deployments without
// a Lark app — log a warning, fall back to "Lark integration not
// configured", and continue serving other workspace functionality.
var ErrAPIClientNotConfigured = errors.New("lark: API client not configured")

// stubAPIClient is the default APIClient used when no production client
// has been registered. It refuses every transport call with
// ErrAPIClientNotConfigured so a misconfigured deployment fails loudly
// instead of silently dropping cards or device-flow registration
// responses.
//
// We deliberately do NOT silently succeed: a stub that returned ""
// message IDs would let the inbound dispatcher record bogus
// lark_outbound_card_message rows pointing at nothing.
type stubAPIClient struct {
	log *slog.Logger
}

// NewStubAPIClient returns the default no-op APIClient. The hub
// constructs one of these when no real implementation has been
// supplied, so subsystems that depend on APIClient (outbound patcher,
// device-flow registration) can still wire up; their first call
// surfaces a clear error.
func NewStubAPIClient(log *slog.Logger) APIClient {
	if log == nil {
		log = slog.Default()
	}
	return &stubAPIClient{log: log}
}

func (s *stubAPIClient) IsConfigured() bool { return false }

func (s *stubAPIClient) SendInteractiveCard(ctx context.Context, p SendCardParams) (string, error) {
	s.log.Warn("lark stub client: SendInteractiveCard called", "chat_id", string(p.ChatID))
	return "", ErrAPIClientNotConfigured
}

func (s *stubAPIClient) PatchInteractiveCard(ctx context.Context, p PatchCardParams) error {
	s.log.Warn("lark stub client: PatchInteractiveCard called", "card_message_id", p.LarkCardMessageID)
	return ErrAPIClientNotConfigured
}

func (s *stubAPIClient) SendTextMessage(ctx context.Context, p SendTextParams) (string, error) {
	s.log.Warn("lark stub client: SendTextMessage called", "chat_id", string(p.ChatID))
	return "", ErrAPIClientNotConfigured
}

func (s *stubAPIClient) SendDirectTextMessage(ctx context.Context, p SendDirectTextParams) (string, error) {
	s.log.Warn("lark stub client: SendDirectTextMessage called", "open_id", string(p.OpenID))
	return "", ErrAPIClientNotConfigured
}

func (s *stubAPIClient) SendMarkdownCard(ctx context.Context, p SendMarkdownCardParams) (string, error) {
	s.log.Warn("lark stub client: SendMarkdownCard called", "chat_id", string(p.ChatID))
	return "", ErrAPIClientNotConfigured
}

func (s *stubAPIClient) SendBindingPromptCard(ctx context.Context, p BindingPromptParams) error {
	s.log.Warn("lark stub client: SendBindingPromptCard called", "open_id", string(p.OpenID))
	return ErrAPIClientNotConfigured
}

func (s *stubAPIClient) GetBotInfo(ctx context.Context, creds InstallationCredentials) (BotInfo, error) {
	s.log.Warn("lark stub client: GetBotInfo called", "app_id", creds.AppID)
	return BotInfo{}, ErrAPIClientNotConfigured
}

func (s *stubAPIClient) GetMessage(ctx context.Context, creds InstallationCredentials, messageID string) ([]LarkMessage, error) {
	s.log.Warn("lark stub client: GetMessage called", "message_id", messageID)
	return nil, ErrAPIClientNotConfigured
}

func (s *stubAPIClient) ListChatMessages(ctx context.Context, creds InstallationCredentials, p ListMessagesParams) ([]LarkMessage, error) {
	s.log.Warn("lark stub client: ListChatMessages called", "chat_id", string(p.ChatID))
	return nil, ErrAPIClientNotConfigured
}

func (s *stubAPIClient) BatchGetUsers(ctx context.Context, creds InstallationCredentials, openIDs []string) (map[string]string, error) {
	s.log.Warn("lark stub client: BatchGetUsers called", "count", len(openIDs))
	return nil, ErrAPIClientNotConfigured
}

func (s *stubAPIClient) AddMessageReaction(ctx context.Context, p AddReactionParams) (string, error) {
	s.log.Warn("lark stub client: AddMessageReaction called", "message_id", p.MessageID, "emoji_type", p.EmojiType)
	return "", ErrAPIClientNotConfigured
}

func (s *stubAPIClient) DeleteMessageReaction(ctx context.Context, p DeleteReactionParams) error {
	s.log.Warn("lark stub client: DeleteMessageReaction called", "message_id", p.MessageID, "reaction_id", p.ReactionID)
	return ErrAPIClientNotConfigured
}
