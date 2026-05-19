// Package feishu implements the port.Channel adapter for Feishu (Lark).
//
// Architecture (DESIGN §3.1):
//
//	+------------------+      +-----------------+      +------------------+
//	| dispatcher (T11) | <--- |  Adapter (this) | ---> |  Client (seam)   |
//	+------------------+      +-----------------+      +------------------+
//	                                                            |
//	                                                            v
//	                                                  +-------------------+
//	                                                  |  Feishu OAPI SDK  |
//	                                                  |  (wired in T7 +)  |
//	                                                  +-------------------+
//
// The Adapter never imports the Feishu SDK directly. All platform interaction
// goes through the Client interface defined in this file, and the SDK-backed
// concrete implementation is wired up by a follow-up task (the M1-T7 leader-
// election + bot wiring task). For unit tests, a fakeFeishuClient sits behind
// the same interface, which is why TC-adapt-1 / TC-adapt-2 do not need a
// running Feishu account.
package feishu

import (
	"context"
	"encoding/json"
)

// Client is the seam between the adapter and the Feishu OpenAPI / WebSocket
// SDK. Every platform-touching call the adapter makes goes through this
// interface — so swapping in a fake (or, later, a different SDK version) is a
// single dependency-injection change.
//
// Implementations must satisfy two cross-cutting contracts:
//
//   - Concurrency: every method (except Subscribe, which only returns a
//     pre-existing channel) must be safe to call from a different goroutine
//     than the one currently consuming Subscribe(). The adapter's pump and
//     Send paths run independently.
//   - Error classification: SendMessage callers (currently Adapter.sendText)
//     read Retryable off the returned error via errors.As against the
//     concrete retryableError type produced by RetryableError(err). Wrap
//     transient errors (5xx, network resets, token-rotation in flight) with
//     RetryableError; leave 4xx-class errors un-wrapped. Mis-classifying
//     here can cause infinite send loops in the outbound queue (T15).
//
// All DTOs (RawEvent, SendRequest, …) are defined alongside the interface so
// test doubles only need to import this one package.
type Client interface {
	// Start opens the WebSocket long connection (and any other transport
	// the SDK needs) and begins delivering events on Subscribe(). It must
	// return only after the initial handshake succeeds, and fail fast on
	// auth / config errors so the caller can surface a meaningful boot
	// error instead of an opaque "no events ever arrive".
	//
	// Reconnect / replay is the implementation's responsibility. PRD AC2.1
	// requires no message loss across a 30s outage, which the SDK delivers
	// via its replay buffer; the wiring task (T7) attaches reconnect
	// alarms / metrics on top.
	//
	// Idempotency: calling Start twice on the same Client must return nil
	// the second time without re-opening the connection.
	Start(ctx context.Context) error

	// Stop tears down the platform connection. After Stop returns, the
	// channel returned by Subscribe() must be closed so downstream
	// `for range` consumers terminate cleanly. Stop is the only place
	// allowed to close that channel.
	//
	// Idempotency: calling Stop twice must return nil the second time;
	// the second close is a no-op (closing an already-closed Go channel
	// would panic).
	Stop(ctx context.Context) error

	// Subscribe returns the receive-only event stream. The same channel
	// must be returned across calls (callers may cache the reference and
	// loop on it), and the channel must be created before Start returns
	// so a goroutine that captures the reference before Start cannot
	// observe a nil channel.
	//
	// Sending on the channel after Stop closes it is a programming error
	// in the implementation, not a recoverable runtime condition.
	Subscribe() <-chan RawEvent

	// BotUserID returns the open_id of the bot account. The adapter uses
	// this to recognise and strip @-mentions of itself from incoming text
	// (Issue 关键实现要点 §4: "@_user_<bot_id>" must not survive into
	// InboundEvent.Text).
	//
	// May be called before Start has fully completed (e.g. from a test
	// fake) — implementations that learn the id only after handshake
	// should block here, not return a stale empty string.
	BotUserID() string

	// SendMessage POSTs an im.v1.messages.create request to the Feishu
	// OpenAPI. The returned SendResponse.MessageID is surfaced as
	// port.SendResult.PlatformMessageID; an empty MessageID with a nil
	// error is a contract violation (the adapter treats it as a
	// non-retryable failure).
	//
	// Errors must be classifiable: wrap transient ones with
	// RetryableError so Adapter.sendText can populate
	// SendResult.Retryable=true. Anything not wrapped is treated as a
	// permanent failure.
	SendMessage(ctx context.Context, req SendRequest) (SendResponse, error)

	// GetChatInfo fetches metadata for a chat room. Used by downstream
	// commands (binding flow, intent disambiguation). MVP only requires
	// the minimum surface; richer fields can be added without breaking
	// tests because the adapter projects this DTO into port.ChatInfo.
	//
	// Returning an error for an unknown chat_id is acceptable —
	// downstream code already needs to handle "chat not found" for stale
	// references after a workspace unbind.
	GetChatInfo(ctx context.Context, chatID string) (ChatInfoResponse, error)

	// GetUserInfo fetches metadata for a user. Same minimum-surface rules
	// as GetChatInfo apply — the adapter projects into port.UserInfo.
	GetUserInfo(ctx context.Context, userID string) (UserInfoResponse, error)
}

// RawEvent is the SDK-neutral envelope every event the Client emits. Payload
// holds the original event JSON verbatim; the adapter parses it into a
// port.InboundEvent. Keeping the raw bytes (rather than a typed struct) means
// the SDK can evolve its schema without forcing an interface change.
type RawEvent struct {
	// EventID is the platform-assigned event id. Used as the de-duplication
	// key by the inbound dedup table (T6) — adapters MUST NOT mint their
	// own id here, otherwise SDK replay after a 30s outage will deliver
	// the same logical event twice with different ids and bypass dedup.
	EventID string

	// EventType is the Feishu schema name (e.g. "im.message.receive_v1").
	// The adapter translates it into a typed port.EventType.
	EventType string

	// Payload is the raw event JSON, exactly as the platform delivered it.
	// Stored on InboundEvent.RawPayload so on-call engineers can replay
	// arbitrary event shapes during incident debugging.
	Payload json.RawMessage
}

// SendRequest is the input to Client.SendMessage. Field names mirror the
// Feishu OpenAPI body so concrete clients can marshal directly without an
// extra translation step.
type SendRequest struct {
	// ReceiveIDType is one of "chat_id", "open_id", "union_id", "email",
	// "user_id". The adapter always sets this to "chat_id" for outbound
	// replies (we always know the chat) so downstream platform code does
	// not need to resolve user identifiers.
	ReceiveIDType string
	// ReceiveID is the destination identifier matching ReceiveIDType.
	ReceiveID string
	// MsgType is one of "text", "post", "image", "interactive", … MVP only
	// uses "text"; cards (msg_type="interactive") land in T16.
	MsgType string
	// Content is the JSON-encoded message body — Feishu wraps even plain
	// text in {"text": "..."}. Storing it pre-marshalled keeps the seam
	// platform-shaped (the SDK consumes a JSON string here, not a struct).
	Content string
}

// SendResponse is the output of Client.SendMessage. MessageID is the
// platform-assigned message id, surfaced as port.SendResult.PlatformMessageID
// so reactions / edits can be correlated back to the originating outbound
// log row (T8).
type SendResponse struct {
	MessageID string
}

// ChatInfoResponse and UserInfoResponse are minimum-surface metadata DTOs
// used by GetChatInfo / GetUserInfo. The adapter projects them into the
// platform-neutral port.ChatInfo / port.UserInfo so callers never need to
// know which fields the SDK populated.
type ChatInfoResponse struct {
	ID   string
	Name string
	Type string // "group" | "p2p" — projected to port.ChatType
}

type UserInfoResponse struct {
	OpenID string
	Name   string
}
