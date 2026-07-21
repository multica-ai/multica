package lark

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
)

// larkMsgTypeMergeForward is the msg_type of a "merged & forwarded"
// message — a bundle of other messages a user forwarded as one unit.
// Its own body.content is a fixed sentinel string; the actual forwarded
// messages come back as the extra items[] of a GetMessage call.
const larkMsgTypeMergeForward = "merge_forward"

// defaultMaxForwardChildren caps how many child messages we inline from
// a single forward. Lark itself bounds a merge_forward at 100 messages;
// we mirror that as a safety valve so a pathological bundle can't blow
// up the agent's context. Anything beyond the cap is dropped with a
// visible "... (N more truncated)" marker.
const defaultMaxForwardChildren = 100

// DefaultRecentContextSize is the window the production wiring uses for
// the group-context prefetch: the page_size of the single list call made
// when a user @-mentions the Bot in a group. It is a FETCH budget, not a
// guaranteed rendered count — the trigger message itself and any quoted
// parent are filtered out of the result, so the <recent_context> block
// usually renders one or two fewer lines. 10 keeps the agent's prompt
// meaningfully contextual without bloating it or straining the inbound
// ACK budget (one list call, page_size 10).
const DefaultRecentContextSize = 10

const (
	recentContextEndpoint         = "im/v1/messages.list"
	recentContextMaxFetchAttempts = 2

	recentContextFailureUnknown          = "unknown"
	recentContextFailureChannelUnbound   = "channel_unbound"
	recentContextFailureTimeout          = "timeout"
	recentContextFailurePermissionDenied = "permission_denied"
	recentContextFailureMessageDeleted   = "message_deleted"
	recentContextFailureRateLimited      = "rate_limited"
	recentContextFailureTokenExpired     = "token_expired"
	recentContextFailureTemporary        = "temporary"
)

var errRecentContextChannelUnbound = errors.New("lark enricher: missing chat_id for recent context")

// Enricher expands an inbound message's body with context the user
// EXPLICITLY attached — a quoted reply or a merged-and-forwarded bundle
// — by calling back into Lark's IM API. It runs after the (fast,
// HTTP-free) decoder and before the dispatcher, turning a bare
// "@bot 总结一下" into a body that already carries the referenced
// conversation inline.
//
// It is best-effort by contract: every fetch failure degrades to a
// readable note or placeholder and Enrich NEVER returns an error or
// blocks ingestion. A message with nothing to expand (no parent_id, not
// a merge_forward) is returned untouched without any network call.
type Enricher interface {
	Enrich(ctx context.Context, msg InboundMessage, creds InstallationCredentials) InboundMessage
}

// InboundEnricherConfig tunes the enricher. All fields default.
type InboundEnricherConfig struct {
	// MaxForwardChildren caps inlined forward children. <=0 uses
	// defaultMaxForwardChildren.
	MaxForwardChildren int
	// RecentContextSize caps how many surrounding group messages the
	// enricher prefetches and inlines as a <recent_context> block when a
	// user @-mentions the Bot in a group. <=0 DISABLES the prefetch
	// entirely (only explicitly-attached quote/forward context is used);
	// the production wiring sets DefaultRecentContextSize. Values above
	// Lark's 50-per-page cap are clamped by the client.
	RecentContextSize int
	// Logger receives best-effort warnings about fetch failures. Nil
	// uses slog.Default().
	Logger *slog.Logger
}

type inboundEnricher struct {
	client             APIClient
	maxForwardChildren int
	recentContextSize  int
	logger             *slog.Logger
}

// NewInboundEnricher builds an Enricher backed by the given Lark API
// client. The client supplies GetMessage; everything else (flattening,
// block assembly, speaker labelling) is local.
func NewInboundEnricher(client APIClient, cfg InboundEnricherConfig) Enricher {
	if cfg.MaxForwardChildren <= 0 {
		cfg.MaxForwardChildren = defaultMaxForwardChildren
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &inboundEnricher{
		client:             client,
		maxForwardChildren: cfg.MaxForwardChildren,
		recentContextSize:  cfg.RecentContextSize,
		logger:             cfg.Logger,
	}
}

// Enrich rewrites msg.Body to inline surrounding group context and/or
// any quoted-reply parent and/or forwarded bundle. Composition order
// goes broadest-to-narrowest: the surrounding group history first, then
// the explicitly-quoted parent (a specific reference), then the message's
// own content (or, for a forward, the rendered transcript).
//
//	<recent_context …>…</recent_context>
//
//	<quoted_message …>…</quoted_message>
//
//	<[sender name]: the user's own message, or the forwarded transcript>
//
// The <recent_context> block is only produced for a group message
// addressed to the Bot, and only when RecentContextSize > 0 — it answers
// MUL-3084 (the Bot saw only the single @-ed line, never the surrounding
// conversation). It is the one fetch here NOT triggered by something the
// user explicitly attached.
//
// In group chats, every speaker across ALL blocks (recent + quoted +
// forwarded) and the sender who @-mentioned the Bot are resolved to real
// display names via ONE Contact batch call, so the agent reads
// "[Alice]: …" rather than "[User 1]: …" and knows who addressed it. This
// is why the quote/forward items are fetched up front (Phase 1) before
// names are resolved (Phase 2). Unresolved senders fall back to positional
// "User N"; resolution is best-effort and never blocks. p2p chats keep
// positional labels (identity is unambiguous in a 1:1).
//
// Persistence note: like the quoted/forwarded blocks, the rewritten Body
// is persisted into the addressed turn's chat_message.content downstream
// (AppendUserMessage). Inlining nearby group messages — including ones
// from senders who did not address the Bot — into a member's addressed
// turn is an accepted product decision for MUL-3084. It does NOT relax
// the MUL-2671 drop-audit invariant: a non-addressed group message still
// never creates its own session row, and is only ever surfaced as read-
// context attached to a turn a workspace member explicitly directed at
// the Bot.
func (e *inboundEnricher) Enrich(ctx context.Context, msg InboundMessage, creds InstallationCredentials) InboundMessage {
	freshSource := msg.CommandBody
	if freshSource == "" {
		freshSource = msg.Body
	}
	if cmd, ok := parseFreshSessionCommand(freshSource); ok {
		msg.ForceFreshSession = true
		msg.Body = cmd.Body
	}

	isForward := msg.MessageType == larkMsgTypeMergeForward
	wantRecent := e.recentContextSize > 0 && msg.ChatType == ChatTypeGroup && msg.AddressedToBot
	if msg.ParentID == "" && !isForward && !wantRecent {
		// Nothing to expand and no group prefetch wanted — no network call.
		return msg
	}
	// If the transport isn't wired (stub client on a deployment without
	// a Lark app), skip rather than stamp every reply with a fetch
	// error. Body stays whatever the decoder produced.
	if e.client == nil || !e.client.IsConfigured() {
		return msg
	}

	// Phase 1 — fetch every set of messages we may render. Each is
	// best-effort; its error is handled where the block is rendered. We
	// fetch up front (rather than fetch-and-render per block) so Phase 2
	// can resolve display names for EVERY speaker across ALL blocks in a
	// single Contact batch — otherwise a quoted/forwarded sender that
	// isn't in the recent window would fall back to "User N".
	var recentItems []LarkMessage
	var recentErr error
	if wantRecent {
		recentItems, recentErr = e.fetchRecentItems(ctx, creds, msg)
	}
	var quotedItems []LarkMessage
	var quotedErr error
	if msg.ParentID != "" {
		quotedItems, quotedErr = e.client.GetMessage(ctx, creds, msg.ParentID)
	}
	var forwardItems []LarkMessage
	var forwardErr error
	if isForward {
		forwardItems, forwardErr = e.client.GetMessage(ctx, creds, msg.MessageID)
	}

	// Phase 2 — resolve display names for every speaker we're about to
	// render (recent + quoted + forwarded) plus the sender who @-mentioned
	// the Bot, in one batch. Group chats only; p2p keeps positional labels
	// (identity is unambiguous in a 1:1). Unresolved ids fall back to
	// "User N" per speakerLabeler.
	var names map[string]string
	if msg.ChatType == ChatTypeGroup {
		ids := senderOpenIDs(recentItems)
		ids = append(ids, senderOpenIDs(quotedItems)...)
		ids = append(ids, senderOpenIDs(forwardItems)...)
		if msg.SenderOpenID != "" {
			ids = append(ids, string(msg.SenderOpenID))
		}
		names = e.resolveNames(ctx, creds, ids)
	}

	// Phase 3 — render broadest-to-narrowest with the complete name map.
	var b strings.Builder
	if wantRecent {
		if recentErr != nil {
			b.WriteString(recentContextUnavailableLine(classifyRecentContextFetchError(recentErr).category))
		} else if len(recentItems) > 0 {
			b.WriteString(e.renderRecentContextBlock(recentItems, names))
		}
	}
	if msg.ParentID != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(e.renderQuotedBlock(msg.ParentID, quotedItems, quotedErr, names))
	}

	var core string
	if isForward {
		if forwardErr != nil {
			e.logger.Warn("lark enricher: forward fetch failed", "message_id", msg.MessageID, "err", forwardErr)
			core = forwardedErrorBlock()
		} else {
			core = e.renderForwardedItems(forwardItems, msg.MessageID, names)
		}
	} else {
		core = msg.Body
		// Label the user's own message with their real name so the agent
		// knows WHO @-mentioned it — not just what they said. Only when the
		// name resolved (group path); otherwise the body passes through.
		if name := names[string(msg.SenderOpenID)]; name != "" {
			core = fmt.Sprintf("[%s]: %s", name, msg.Body)
		}
	}
	if b.Len() > 0 && core != "" {
		b.WriteString("\n\n")
	}
	b.WriteString(core)

	msg.Body = b.String()
	return msg
}

// senderOpenIDs returns the distinct non-app sender open_ids across the
// given messages, in first-appearance order — the input set for a
// Contact name lookup.
func senderOpenIDs(msgs []LarkMessage) []string {
	seen := make(map[string]bool, len(msgs))
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.SenderType == "app" || m.SenderID == "" || seen[m.SenderID] {
			continue
		}
		seen[m.SenderID] = true
		out = append(out, m.SenderID)
	}
	return out
}

// resolveNames batch-resolves open_ids to display names, best-effort: a
// failure (restricted contact scope, transport error) logs and returns
// nil so every speaker labeler degrades to positional "User N" rather
// than blocking ingestion. Duplicate / empty ids are dropped first.
func (e *inboundEnricher) resolveNames(ctx context.Context, creds InstallationCredentials, ids []string) map[string]string {
	uniq := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return nil
	}
	names, err := e.client.BatchGetUsers(ctx, creds, uniq)
	if err != nil {
		e.logger.Warn("lark enricher: speaker name resolution failed", "ids", len(uniq), "err", err)
		return nil
	}
	return names
}

// fetchRecentItems pulls the recent group window and returns the
// messages to render — the trigger message itself and the directly-quoted
// parent (which gets its own <quoted_message> block) filtered out, sorted
// oldest-first. The window is anchored to the trigger message's time so
// it captures the conversation up to the @-mention rather than whatever
// is newest by the time this fetch runs. A fetch failure is returned to
// the caller (which renders a safe, readable degradation note); it never
// blocks ingestion.
func (e *inboundEnricher) fetchRecentItems(ctx context.Context, creds InstallationCredentials, msg InboundMessage) ([]LarkMessage, error) {
	if msg.ChatID == "" {
		classified := classifyRecentContextFetchError(errRecentContextChannelUnbound)
		e.logRecentContextFetchFailure(msg, errRecentContextChannelUnbound, classified, 0)
		return nil, errRecentContextChannelUnbound
	}

	params := ListMessagesParams{
		ChatID:   msg.ChatID,
		PageSize: e.recentContextSize,
		// Lark sends create_time as epoch millis; end_time wants seconds. A
		// missing/unparseable time yields 0, which the client treats as
		// "no end_time" (newest N).
		EndTime: parseLarkMillis(msg.CreateTime) / 1000,
	}
	var items []LarkMessage
	var err error
	for attempt := 1; attempt <= recentContextMaxFetchAttempts; attempt++ {
		items, err = e.client.ListChatMessages(ctx, creds, params)
		if err == nil {
			if attempt > 1 {
				e.logger.Info("lark enricher: recent context fetch recovered after retry",
					"layer", "lark_inbound_enricher",
					"endpoint", recentContextEndpoint,
					"status", "recovered",
					"attempts", attempt,
					"chat_id", string(msg.ChatID),
					"message_id", msg.MessageID)
			}
			break
		}
		classified := classifyRecentContextFetchError(err)
		// A retry only helps while the shared enrichment budget still has
		// time left. Both attempts reuse one ctx (ws_connector caps the
		// whole Enrich at EnrichTimeout, ~2s), so once ctx is done a second
		// call fails immediately — degrade now instead of burning a doomed
		// request. This is why a first-attempt deadline never "recovers".
		if !classified.retryable || attempt == recentContextMaxFetchAttempts || ctx.Err() != nil {
			e.logRecentContextFetchFailure(msg, err, classified, attempt)
			return nil, err
		}
		e.logger.Warn("lark enricher: recent context fetch failed; retrying",
			"layer", "lark_inbound_enricher",
			"endpoint", recentContextEndpoint,
			"status", "retrying",
			"category", classified.category,
			"retryable", classified.retryable,
			"attempt", attempt,
			"next_attempt", attempt+1,
			"chat_id", string(msg.ChatID),
			"message_id", msg.MessageID,
			"err", err)
	}

	exclude := map[string]bool{msg.MessageID: true}
	if msg.ParentID != "" {
		exclude[msg.ParentID] = true
	}
	kept := make([]LarkMessage, 0, len(items))
	for _, it := range items {
		if exclude[it.MessageID] {
			continue
		}
		kept = append(kept, it)
	}

	// The list endpoint returns newest-first; render oldest-first so the
	// transcript reads top-to-bottom like the chat does.
	sort.SliceStable(kept, func(i, j int) bool {
		return parseLarkMillis(kept[i].CreateTime) < parseLarkMillis(kept[j].CreateTime)
	})
	return kept, nil
}

func (e *inboundEnricher) logRecentContextFetchFailure(msg InboundMessage, err error, classified recentContextFetchClassification, attempts int) {
	e.logger.Warn("lark enricher: recent context fetch failed",
		"layer", "lark_inbound_enricher",
		"endpoint", recentContextEndpoint,
		"status", "failed",
		"category", classified.category,
		"retryable", classified.retryable,
		"attempts", attempts,
		"chat_id", string(msg.ChatID),
		"message_id", msg.MessageID,
		"err", err)
}

// renderRecentContextBlock renders the surrounding conversation as a
// <recent_context> block: one "[<speaker>]: <text>" line per message,
// oldest-first, speakers labeled with real names from `names` (falling
// back to positional "User N"). Callers pass a non-empty `kept`.
func (e *inboundEnricher) renderRecentContextBlock(kept []LarkMessage, names map[string]string) string {
	labeler := newSpeakerLabeler(names)
	lines := make([]string, 0, len(kept))
	for _, m := range kept {
		label := labeler.label(m)
		var text string
		switch {
		case m.MessageType == larkMsgTypeMergeForward:
			text = "[merge_forward, expand manually]"
		default:
			text = e.flattenMessage(m)
			if text == "" {
				text = "[empty message]"
			}
		}
		lines = append(lines, fmt.Sprintf("[%s]: %s", label, text))
	}
	return fmt.Sprintf("<recent_context count=\"%d\">\n%s\n</recent_context>",
		len(kept), strings.Join(lines, "\n"))
}

type recentContextFetchClassification struct {
	category  string
	retryable bool
}

func classifyRecentContextFetchError(err error) recentContextFetchClassification {
	if err == nil {
		return recentContextFetchClassification{category: recentContextFailureUnknown}
	}
	if errors.Is(err, errRecentContextChannelUnbound) {
		return recentContextFetchClassification{category: recentContextFailureChannelUnbound}
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return classifyRecentContextAPIError(apiErr.Code, apiErr.Msg)
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &netErr) && netErr.Timeout()) {
		return recentContextFetchClassification{category: recentContextFailureTimeout, retryable: true}
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "missing chat_id") || strings.Contains(msg, "missing chat id"):
		return recentContextFetchClassification{category: recentContextFailureChannelUnbound}
	case containsAny(msg, "code=99991002", "code=230001", "no permission", "permission denied", "insufficient permissions", "forbidden", "http 403"):
		return recentContextFetchClassification{category: recentContextFailurePermissionDenied}
	case containsAny(msg, "code=230110", "code=230011", "code=230050", "deleted", "recalled", "not visible", "invisible"):
		return recentContextFetchClassification{category: recentContextFailureMessageDeleted}
	case containsAny(msg, "code=99991663", "code=99991664"):
		return recentContextFetchClassification{category: recentContextFailureTokenExpired, retryable: true}
	case containsAny(msg, "code=230020", "rate limit", "rate_limit", "http 429"):
		// Not retryable: the client drops Retry-After, and an immediate
		// second call within the same budget almost always re-hits the
		// limit while doubling list load on an already-throttled tenant.
		return recentContextFetchClassification{category: recentContextFailureRateLimited}
	case containsAny(msg, "deadline exceeded", "timeout", "timed out"):
		return recentContextFetchClassification{category: recentContextFailureTimeout, retryable: true}
	case containsAny(msg, "http 500", "http 502", "http 503", "http 504", "connection reset", "connection refused", "temporary"):
		return recentContextFetchClassification{category: recentContextFailureTemporary, retryable: true}
	default:
		return recentContextFetchClassification{category: recentContextFailureUnknown}
	}
}

func classifyRecentContextAPIError(code int, msg string) recentContextFetchClassification {
	switch {
	case code == 99991002 || code == 230001:
		return recentContextFetchClassification{category: recentContextFailurePermissionDenied}
	case code == 230110 || code == 230011 || code == 230050:
		return recentContextFetchClassification{category: recentContextFailureMessageDeleted}
	case isTokenError(code):
		return recentContextFetchClassification{category: recentContextFailureTokenExpired, retryable: true}
	case code == 230020:
		// See classifyRecentContextFetchError: rate limits degrade rather
		// than retry, since the client drops Retry-After.
		return recentContextFetchClassification{category: recentContextFailureRateLimited}
	}
	return classifyRecentContextFetchError(errors.New(msg))
}

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func recentContextUnavailableLine(category string) string {
	switch category {
	case recentContextFailureChannelUnbound:
		return "[Recent Lark context unavailable: chat binding is missing. Continuing with the latest message.]"
	case recentContextFailurePermissionDenied:
		return "[Recent Lark context unavailable: the bot cannot read this chat history. Continuing with the latest message.]"
	case recentContextFailureMessageDeleted:
		return "[Recent Lark context unavailable: the referenced chat history is deleted or no longer visible. Continuing with the latest message.]"
	case recentContextFailureTimeout, recentContextFailureRateLimited, recentContextFailureTokenExpired, recentContextFailureTemporary:
		return "[Recent Lark context temporarily unavailable; continuing with the latest message.]"
	default:
		return "[Recent Lark context unavailable; continuing with the latest message.]"
	}
}

// renderQuotedBlock renders a <quoted_message> block from the already-
// fetched GetMessage(parentID) result. A parent that is itself a
// merge_forward nests a <forwarded_messages> transcript inside the quoted
// block (the GetMessage response already carries both the forward
// sentinel and its children). A fetch error / empty / deleted parent
// degrades to the documented error block. Speakers are labeled from
// `names` (the shared, already-resolved map), falling back to "User N".
func (e *inboundEnricher) renderQuotedBlock(parentID string, items []LarkMessage, err error, names map[string]string) string {
	if err != nil || len(items) == 0 {
		e.logger.Warn("lark enricher: quoted parent fetch failed",
			"parent_id", parentID, "items", len(items), "err", err)
		return quotedErrorBlock(parentID)
	}
	parent := items[0]
	if parent.Deleted {
		return quotedErrorBlock(parentID)
	}

	labeler := newSpeakerLabeler(names)
	sender := labeler.label(parent)

	if parent.MessageType == larkMsgTypeMergeForward {
		inner := e.renderForwardedItems(items, parentID, names)
		return wrapQuoted(parentID, sender, larkMsgTypeMergeForward, inner)
	}
	text := e.flattenMessage(parent)
	if text == "" {
		text = "[empty message]"
	}
	return wrapQuoted(parentID, sender, parent.MessageType, text)
}

// renderForwardedItems renders the children of a forward whose own
// record id is forwardID. Children are time-ordered, capped, and each
// rendered as "[<speaker>]: <text>"; a child that is itself a forward is
// not recursed into (it gets a manual-expand placeholder) so the HTTP
// fan-out on the ACK-latency-sensitive inbound path stays bounded.
func (e *inboundEnricher) renderForwardedItems(items []LarkMessage, forwardID string, names map[string]string) string {
	// The verified contract is that GetMessage(forward_id) returns one
	// level of bundling: [sentinel, direct-children…]. We therefore
	// treat every non-sentinel item as a direct child. We filter by id
	// (not by upper_message_id == forwardID) on purpose: a strict
	// upper_message_id match would silently DROP a real child if Lark
	// ever returned one with that field unpopulated. A child that is
	// itself a forward is rendered as a manual-expand placeholder below
	// rather than recursed into, so grandchildren are never inlined.
	children := make([]LarkMessage, 0, len(items))
	for _, it := range items {
		if it.MessageID == forwardID {
			continue // the forward sentinel itself
		}
		children = append(children, it)
	}
	total := len(children)
	if total == 0 {
		return "<forwarded_messages count=\"0\">\n[no forwarded content available]\n</forwarded_messages>"
	}

	sort.SliceStable(children, func(i, j int) bool {
		return parseLarkMillis(children[i].CreateTime) < parseLarkMillis(children[j].CreateTime)
	})

	truncated := 0
	if total > e.maxForwardChildren {
		truncated = total - e.maxForwardChildren
		children = children[:e.maxForwardChildren]
	}

	labeler := newSpeakerLabeler(names)
	lines := make([]string, 0, len(children))
	for _, c := range children {
		label := labeler.label(c)
		var text string
		switch {
		case c.MessageType == larkMsgTypeMergeForward:
			text = "[nested merge_forward, expand manually]"
		default:
			text = e.flattenMessage(c)
			if text == "" {
				text = "[empty message]"
			}
		}
		lines = append(lines, fmt.Sprintf("[%s]: %s", label, text))
	}
	body := strings.Join(lines, "\n")
	if truncated > 0 {
		body += fmt.Sprintf("\n... (%d more truncated)", truncated)
	}
	return fmt.Sprintf("<forwarded_messages count=\"%d\">\n%s\n</forwarded_messages>", total, body)
}

// flattenMessage turns one fetched message into plain text: structural
// flatten by msg_type, then @_user_N placeholder resolution against the
// message's own mentions. The bot mention is NOT stripped here (unlike
// the inbound decoder) — a quoted / forwarded message is historical
// context, not a fresh trigger, so passing empty bot identifiers leaves
// every @-mention rendered as a readable @name.
func (e *inboundEnricher) flattenMessage(m LarkMessage) string {
	if m.Deleted {
		return "[deleted message]"
	}
	raw := flattenContent(m.MessageType, m.Content)
	if raw == "" {
		return ""
	}
	return resolveMentions(raw, restMentionsToEvent(m.Mentions), "", "")
}

// restMentionsToEvent adapts the IM REST mention shape (flat string id)
// to the WS-event larkMention shape resolveMentions consumes, so a
// single mention-resolution implementation serves both ingress paths.
func restMentionsToEvent(ms []LarkMessageMention) []larkMention {
	if len(ms) == 0 {
		return nil
	}
	out := make([]larkMention, 0, len(ms))
	for _, m := range ms {
		lm := larkMention{Key: m.Key, Name: m.Name}
		lm.ID.OpenID = m.ID
		out = append(out, lm)
	}
	return out
}

func wrapQuoted(messageID, sender, msgType, inner string) string {
	return fmt.Sprintf("<quoted_message message_id=%q sender=%q type=%q>\n%s\n</quoted_message>",
		messageID, sender, msgType, inner)
}

func quotedErrorBlock(messageID string) string {
	return fmt.Sprintf("<quoted_message message_id=%q type=\"error\">[unable to fetch]</quoted_message>", messageID)
}

func forwardedErrorBlock() string {
	return "<forwarded_messages type=\"error\">[unable to fetch]</forwarded_messages>"
}

func parseLarkMillis(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// speakerLabeler assigns stable, human-readable labels to the senders
// within one rendered block. Lark message items carry only a sender id
// (no display name in the payload), so the enricher resolves real names
// out of band via the Contact API and passes them in as a sender-id ->
// name map. A sender present in that map is labeled with their real
// name; one that is not (restricted contact scope, deactivated user,
// name lookup failed) falls back to "User 1", "User 2", … in
// first-appearance order. App senders are always "Bot".
type speakerLabeler struct {
	names map[string]string // resolved open_id -> display name (may be nil)
	seen  map[string]string
	n     int
}

func newSpeakerLabeler(names map[string]string) *speakerLabeler {
	return &speakerLabeler{names: names, seen: make(map[string]string)}
}

func (l *speakerLabeler) label(m LarkMessage) string {
	if m.SenderType == "app" {
		return "Bot"
	}
	key := m.SenderID
	if key == "" {
		key = "unknown"
	}
	if lbl, ok := l.seen[key]; ok {
		return lbl
	}
	var lbl string
	if name := l.names[key]; name != "" {
		lbl = name
	} else {
		l.n++
		lbl = fmt.Sprintf("User %d", l.n)
	}
	l.seen[key] = lbl
	return lbl
}
