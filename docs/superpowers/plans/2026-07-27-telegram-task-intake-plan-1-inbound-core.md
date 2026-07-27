# Telegram Task Intake — Plan 1: Backend Inbound Core

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A linked Multica member who sends `/issue <title>` to a workspace's Telegram bot creates a tracked issue assigned to the bound agent; plain messages start an agent chat run. Delivered as a new adapter in the existing channel framework — no DB migration, no engine change.

**Architecture:** Telegram inbound arrives over an HTTP **webhook** (not a persistent connection), so it bypasses the Supervisor / `channel.Factory` / `channel.Channel` / WS-lease machinery Slack uses. A public webhook handler verifies Telegram's secret-token header, normalizes the update to `channel.InboundMessage`, and calls the shared `engine.Router.Handle`. A registered Telegram `engine.ResolverSet` (installation route by bot id, identity, dedup, session, audit) runs the same cross-platform pipeline as Slack/Lark. An install service validates the pasted bot token via `getMe`, encrypts it, registers the webhook via `setWebhook`, and persists a `channel_installation` row.

**Tech Stack:** Go, Chi router, sqlc (existing `channel_*` queries — no new SQL), pgx, `secretbox` for at-rest token encryption, Telegram Bot API over HTTP (`net/http`, no SDK).

## Global Constraints

- No FKs / cascades; no new DB tables or migrations. Reuse `channel_installation` + existing `channel.sql` queries (route by `config->>'app_id'`). (CLAUDE.md DB rules; spec.)
- Go: `gofmt`, `go vet`, checked errors; comments in English.
- Backend UUIDs: pure UUID request inputs use `parseUUIDOrBadRequest`; path params that may be human ids resolve through loaders; trusted round-trips use `parseUUID`. (CLAUDE.md Backend UUID Rules.)
- Default tests must never resolve or execute agent CLIs; inbound→create tests use a fake/seeded agent and DB fixtures. (CLAUDE.md Testing.)
- Issue-creation semantics are the shared engine's: `/issue <title>` (exact, case-sensitive) creates an issue; any other message starts a chat run. The adapter does NOT change this. (spec "Task creation vs. chat".)
- Telegram channel discriminator is the literal string `"telegram"`; it is durable data — keep it stable.
- Reference adapter: `server/internal/integrations/slack/`. Mirror its structure; the Telegram deltas are called out per task.

---

## File Structure

Create under `server/internal/integrations/telegram/`:

- `config.go` — `installConfig` JSON shape, `credentials`, `Decrypter`, `decodeCredentials`, `decryptToken`, `DecodePublicConfig`, `parseTelegramBotID`. Mirrors `slack/config.go`.
- `client.go` — minimal Telegram Bot API HTTP client: `GetMe`, `SetWebhook`, `DeleteWebhook`, `SendMessage`, with an `apiBase` override for tests. No SDK.
- `telegram.go` — `TypeTelegram` const, `telegramSender` (implements the reply transport via `SendMessage`), `maxMessageRunes`, `chunkMessage`. Analogue of `slack/channel.go`.
- `inbound.go` — Telegram `Update` structs, `telegramRawEvent`, `inboundFromUpdate` (update → `channel.InboundMessage`), chat-type mapping, addressed-to-bot detection, text cleaning. Analogue of `slack/inbound.go`.
- `resolvers.go` — `NewTelegramResolverSet` + `installationResolver`, `identityResolver` (no cross-installation reuse — Telegram has no "team"), `deduper`, `sessionBinder`, `auditor`. Analogue of `slack/resolvers.go`, simpler.
- `install.go` — `InstallService`: `RegisterBYO` (getMe validate → encrypt → setWebhook → persist), `ListByWorkspace`, `GetInWorkspace`, `Revoke` (+ deleteWebhook), `persistInstall`. Analogue of `slack/install.go` + `byo_install.go` merged (Telegram has one token, so it's smaller).

Modify:

- `server/internal/handler/telegram.go` — **create**: `TelegramWebhook` (public), `ListTelegramInstallations`, `RegisterTelegramBYO`, `RevokeTelegramInstallation`, response types. Analogue of `handler/slack.go` (minus binding-redeem, which is Plan 2).
- `server/internal/handler/handler.go` — add `Handler` fields `TelegramInstall *telegram.InstallService`. (`ChannelRouter` and `Queries` already exist.)
- `server/pkg/protocol/events.go` — add `EventTelegramInstallationCreated`/`Revoked`.
- `server/cmd/server/router.go` — Telegram wiring block (gated by `MULTICA_TELEGRAM_SECRET_KEY`), public webhook route, management routes.

Tests live beside the code they test (`*_test.go` in `telegram/`, handler tests in `handler/`).

---

## Task 1: Telegram config + bot-id parsing + token crypto

**Files:**
- Create: `server/internal/integrations/telegram/config.go`
- Test: `server/internal/integrations/telegram/config_test.go`

**Interfaces:**
- Produces:
  - `type installConfig struct { AppID string json:"app_id"; BotUsername string json:"bot_username,omitempty"; BotTokenEncrypted string json:"bot_token_encrypted"; WebhookSecret string json:"webhook_secret,omitempty" }`
  - `type credentials struct { BotID string; BotUsername string; BotToken string }`
  - `type Decrypter func(ciphertext []byte) (plaintext []byte, err error)`
  - `func decodeCredentials(raw json.RawMessage, decrypt Decrypter) (credentials, error)`
  - `func DecodePublicConfig(raw json.RawMessage) PublicConfig` where `type PublicConfig struct { AppID, BotUsername string }`
  - `func parseTelegramBotID(botToken string) (string, error)` — the id before the first `:`.
  - `var ErrInvalidBotToken = errors.New("telegram: bot token must be <bot_id>:<secret>")`

Telegram delta vs Slack: one token (no app token), and `app_id` = the numeric bot id parsed from the token prefix (`123456789:AA...` → `123456789`), not from a separate token. `webhook_secret` is stored in plaintext (it is a verification token — like a GitHub/Stripe webhook secret — not a credential; a spoofer who knows it still must pass member-binding to do anything). The bot token is encrypted at rest exactly like Slack's.

- [ ] **Step 1: Write the failing test**

```go
package telegram

import (
	"encoding/json"
	"testing"
)

func TestParseTelegramBotID(t *testing.T) {
	id, err := parseTelegramBotID("123456789:AAExampleSecretToken")
	if err != nil || id != "123456789" {
		t.Fatalf("got (%q, %v), want (\"123456789\", nil)", id, err)
	}
	if _, err := parseTelegramBotID("nocolon"); err == nil {
		t.Fatal("expected error for token without ':'")
	}
	if _, err := parseTelegramBotID(":AA"); err == nil {
		t.Fatal("expected error for empty bot id")
	}
}

func TestDecodeCredentialsDecryptsBotToken(t *testing.T) {
	// nil Decrypter treats stored bytes as plaintext; the stored value is
	// base64(plaintext) to mirror the at-rest encoding.
	raw, _ := json.Marshal(installConfig{
		AppID:             "123456789",
		BotUsername:       "acme_tasks_bot",
		BotTokenEncrypted: base64Std("123456789:AAsecret"),
	})
	creds, err := decodeCredentials(raw, nil)
	if err != nil {
		t.Fatalf("decodeCredentials: %v", err)
	}
	if creds.BotID != "123456789" || creds.BotUsername != "acme_tasks_bot" || creds.BotToken != "123456789:AAsecret" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
}
```

Add a tiny test helper `func base64Std(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }` in the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `(cd server && go test ./internal/integrations/telegram/ -run 'TestParseTelegramBotID|TestDecodeCredentials' -v)`
Expected: FAIL — package/functions do not exist (build error).

- [ ] **Step 3: Write minimal implementation**

Copy `slack/config.go` and adapt: drop `AppTokenEncrypted`/`TeamID`/`BotUserID`; add `BotUsername`/`WebhookSecret`; `credentials` carries `BotID`/`BotUsername`/`BotToken`; in `decodeCredentials`, set `BotID: cfg.AppID`. Keep `decryptToken` and `stripWhitespace` verbatim (base64 + optional Decrypter). Add:

```go
// parseTelegramBotID extracts the numeric bot id from a bot token. Telegram
// tokens are "<bot_id>:<auth_secret>"; the bot id is the per-bot routing key
// stored at config->>'app_id' (mirrors slack's parseSlackAppID). It is stable
// for the life of the bot and is what the inbound webhook path routes on.
func parseTelegramBotID(botToken string) (string, error) {
	id, _, ok := strings.Cut(strings.TrimSpace(botToken), ":")
	if !ok || id == "" {
		return "", ErrInvalidBotToken
	}
	return id, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `(cd server && go test ./internal/integrations/telegram/ -run 'TestParseTelegramBotID|TestDecodeCredentials' -v)`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/integrations/telegram/config.go server/internal/integrations/telegram/config_test.go
git commit -m "feat(telegram): installation config + bot-id parsing + token crypto"
```

---

## Task 2: Telegram Bot API client (getMe / setWebhook / deleteWebhook / sendMessage)

**Files:**
- Create: `server/internal/integrations/telegram/client.go`
- Test: `server/internal/integrations/telegram/client_test.go`

**Interfaces:**
- Produces:
  - `type Client struct { ... }`
  - `func NewClient(botToken string, opts ...ClientOption) *Client`
  - `func WithAPIBase(base string) ClientOption` — test override (default `https://api.telegram.org`)
  - `func WithHTTPClient(c *http.Client) ClientOption`
  - `func (c *Client) GetMe(ctx context.Context) (BotInfo, error)` where `type BotInfo struct { ID int64; Username string }`
  - `func (c *Client) SetWebhook(ctx context.Context, url, secretToken string) error`
  - `func (c *Client) DeleteWebhook(ctx context.Context) error`
  - `func (c *Client) SendMessage(ctx context.Context, chatID string, text string, threadID string) error`

Telegram Bot API: methods are `POST {base}/bot{token}/{method}` with JSON body; responses are `{"ok":bool,"result":...,"description":string}`. `getMe.result` has `id` (int64) and `username`. `setWebhook` takes `{"url":..., "secret_token":..., "allowed_updates":["message"]}`. `sendMessage` takes `{"chat_id":..., "text":..., "message_thread_id"?:...}`. A non-2xx or `ok:false` is an error carrying `description`.

- [ ] **Step 1: Write the failing test** (uses `httptest`, no network)

```go
package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientGetMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/bot123:secret/getMe") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "result": map[string]any{"id": int64(123), "username": "acme_bot"},
		})
	}))
	defer srv.Close()
	c := NewClient("123:secret", WithAPIBase(srv.URL))
	info, err := c.GetMe(context.Background())
	if err != nil || info.ID != 123 || info.Username != "acme_bot" {
		t.Fatalf("got (%+v, %v)", info, err)
	}
}

func TestClientGetMeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "Unauthorized"})
	}))
	defer srv.Close()
	if _, err := NewClient("bad:token", WithAPIBase(srv.URL)).GetMe(context.Background()); err == nil {
		t.Fatal("expected error on ok:false")
	}
}

func TestClientSendMessageThreads(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{}})
	}))
	defer srv.Close()
	if err := NewClient("123:s", WithAPIBase(srv.URL)).SendMessage(context.Background(), "555", "hi", "42"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if body["chat_id"] != "555" || body["text"] != "hi" || body["message_thread_id"] != "42" {
		t.Fatalf("unexpected body %+v", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `(cd server && go test ./internal/integrations/telegram/ -run TestClient -v)`
Expected: FAIL — `NewClient` undefined.

- [ ] **Step 3: Write minimal implementation**

Implement `Client` with `botToken`, `apiBase`, `httpClient`. A private `call(ctx, method string, req any, out any) error` that marshals `req`, POSTs to `apiBase + "/bot" + botToken + "/" + method`, decodes `{ok, result, description}`, and returns an error when `!ok` or non-2xx (include `description`). `GetMe` decodes result into `BotInfo`. `SetWebhook` sends `{url, secret_token, allowed_updates:["message"]}`. `DeleteWebhook` sends `{}`. `SendMessage` sends `{chat_id, text}` and, when `threadID != ""`, `message_thread_id: threadID`. Default `apiBase = "https://api.telegram.org"`, default `httpClient = http.DefaultClient`.

- [ ] **Step 4: Run test to verify it passes**

Run: `(cd server && go test ./internal/integrations/telegram/ -run TestClient -v)`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/integrations/telegram/client.go server/internal/integrations/telegram/client_test.go
git commit -m "feat(telegram): minimal Bot API client (getMe/setWebhook/sendMessage)"
```

---

## Task 3: Outbound sender + TypeTelegram + chunking

**Files:**
- Create: `server/internal/integrations/telegram/telegram.go`
- Test: `server/internal/integrations/telegram/telegram_test.go`

**Interfaces:**
- Consumes: `Client` (Task 2), `channel.OutboundMessage`, `channel.SendResult`.
- Produces:
  - `const TypeTelegram channel.Type = "telegram"`
  - `const maxMessageRunes = 4000` (Telegram hard cap is 4096; headroom)
  - `type telegramSender struct { client *Client; logger *slog.Logger }`
  - `func newTelegramSender(creds credentials, apiBase string, logger *slog.Logger) *telegramSender`
  - `func (s *telegramSender) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error)` — satisfies the `replySender`/outbound interface shape used by Slack (`Send(ctx, OutboundMessage) (SendResult, error)`).
  - `func chunkMessage(text string, maxRunes int) []string` — copy verbatim from `slack/channel.go:86`.

Telegram delta: no Markdown→mrkdwn conversion in v1 (send plain text; `parse_mode` omitted). `Send` chunks and calls `client.SendMessage(chatID, chunk, out.ThreadID)` per chunk. `apiBase` is threaded through so tests point at httptest.

- [ ] **Step 1: Write the failing test**

```go
package telegram

import "testing"

func TestChunkMessage(t *testing.T) {
	got := chunkMessage("abcdef", 4)
	if len(got) != 2 || got[0] != "abcd" || got[1] != "ef" {
		t.Fatalf("unexpected chunks %#v", got)
	}
	if one := chunkMessage("short", 100); len(one) != 1 || one[0] != "short" {
		t.Fatalf("expected single chunk, got %#v", one)
	}
}

func TestTypeTelegramValue(t *testing.T) {
	if string(TypeTelegram) != "telegram" {
		t.Fatalf("TypeTelegram = %q, want telegram", TypeTelegram)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `(cd server && go test ./internal/integrations/telegram/ -run 'TestChunkMessage|TestTypeTelegramValue' -v)`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

Define `TypeTelegram`, `maxMessageRunes`, `chunkMessage` (verbatim from Slack). Implement `telegramSender.Send`:

```go
func (s *telegramSender) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	for _, chunk := range chunkMessage(out.Text, maxMessageRunes) {
		if chunk == "" {
			continue
		}
		if err := s.client.SendMessage(ctx, out.ChatID, chunk, out.ThreadID); err != nil {
			return channel.SendResult{}, fmt.Errorf("telegram: sendMessage: %w", err)
		}
	}
	return channel.SendResult{}, nil
}

func newTelegramSender(creds credentials, apiBase string, logger *slog.Logger) *telegramSender {
	if logger == nil {
		logger = slog.Default()
	}
	opts := []ClientOption{}
	if apiBase != "" {
		opts = append(opts, WithAPIBase(apiBase))
	}
	return &telegramSender{client: NewClient(creds.BotToken, opts...), logger: logger}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `(cd server && go test ./internal/integrations/telegram/ -run 'TestChunkMessage|TestTypeTelegramValue' -v)`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/integrations/telegram/telegram.go server/internal/integrations/telegram/telegram_test.go
git commit -m "feat(telegram): outbound sender, TypeTelegram, message chunking"
```

---

## Task 4: Inbound normalization (Telegram Update → channel.InboundMessage)

**Files:**
- Create: `server/internal/integrations/telegram/inbound.go`
- Test: `server/internal/integrations/telegram/inbound_test.go`

**Interfaces:**
- Consumes: `channel.InboundMessage`, `channel.Source`, `channel.ChatType`, `channel.MsgTypeText`, `TypeTelegram`.
- Produces:
  - `type Update struct { UpdateID int64 json:"update_id"; Message *Message json:"message" }`
  - `type Message struct { MessageID int64 json:"message_id"; From *User json:"from"; Chat Chat json:"chat"; Text string json:"text"; Entities []Entity json:"entities"; ReplyToMessage *Message json:"reply_to_message"; MessageThreadID int64 json:"message_thread_id" }`
  - `type User struct { ID int64 json:"id"; IsBot bool json:"is_bot"; Username string json:"username" }`
  - `type Chat struct { ID int64 json:"id"; Type string json:"type" }` (`"private"|"group"|"supergroup"|"channel"`)
  - `type Entity struct { Type string json:"type"; Offset int json:"offset"; Length int json:"length" }`
  - `type telegramRawEvent struct { BotID string json:"bot_id"; ChatType string json:"chat_type"; EventType string json:"event_type" }`
  - `func inboundFromUpdate(u Update, botID, botUsername string) (channel.InboundMessage, bool)` — `ok=false` for updates that must not reach the core (no message, no sender, bot sender, empty text, non-`message` update).
  - `func telegramChatType(t string) channel.ChatType`

Mapping details:
- `SenderID = strconv.FormatInt(msg.From.ID, 10)` (Telegram user id, stable per bot; the identity-binding key).
- `ChatID = strconv.FormatInt(msg.Chat.ID, 10)`.
- `MessageID = EventID = strconv.FormatInt(msg.MessageID, 10)` (dedup key, per (installation, MessageID)).
- `ChatType`: `private → ChatTypeP2P`; `group`/`supergroup` → `ChatTypeGroup`; `channel` → drop (`ok=false`, broadcast channels are not conversations).
- `AddressedToBot`: for p2p, `true`. For group, `true` iff the text contains an `@botusername` mention entity OR it is a reply to a message from the bot (`ReplyToMessage.From.IsBot && ReplyToMessage.From.Username == botUsername`). v1: mention-based, mirroring Slack's group policy.
- `Text`: strip a leading `@botusername` token and trim (so `/issue` parsing sees the raw command). Telegram also allows `/issue@botusername …`; normalize `/issue@botusername` → `/issue` at the start so `engine.ParseIssueCommand` matches.
- `Raw`: JSON of `telegramRawEvent{BotID: botID, ChatType: msg.Chat.Type, EventType: "message"}` — carries the bot id so the installation resolver routes without re-parsing.
- Bot/loop guard: `ok=false` when `msg.From == nil || msg.From.IsBot || strings.TrimSpace(msg.Text) == ""`.

- [ ] **Step 1: Write the failing test**

```go
package telegram

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestInboundFromUpdate_PrivateIssueCommand(t *testing.T) {
	u := Update{UpdateID: 1, Message: &Message{
		MessageID: 77,
		From:      &User{ID: 900, Username: "alice"},
		Chat:      Chat{ID: 555, Type: "private"},
		Text:      "/issue Fix login\nit crashes",
	}}
	msg, ok := inboundFromUpdate(u, "123", "acme_bot")
	if !ok {
		t.Fatal("expected ok")
	}
	if msg.Source.ChannelType != TypeTelegram || msg.Source.ChatType != channel.ChatTypeP2P {
		t.Fatalf("bad source %+v", msg.Source)
	}
	if msg.Source.SenderID != "900" || msg.Source.ChatID != "555" || msg.MessageID != "77" {
		t.Fatalf("bad ids %+v", msg.Source)
	}
	if !msg.AddressedToBot || msg.Text != "/issue Fix login\nit crashes" {
		t.Fatalf("bad text/addressed: %q %v", msg.Text, msg.AddressedToBot)
	}
}

func TestInboundFromUpdate_GroupRequiresMention(t *testing.T) {
	base := Message{MessageID: 5, From: &User{ID: 1}, Chat: Chat{ID: -100, Type: "supergroup"}}
	noMention := base
	noMention.Text = "hello team"
	if msg, ok := inboundFromUpdate(Update{Message: &noMention}, "123", "acme_bot"); !ok || msg.AddressedToBot {
		t.Fatalf("group without mention should be ok but not addressed; got ok=%v addressed=%v", ok, msg.AddressedToBot)
	}
	withMention := base
	withMention.Text = "@acme_bot /issue Ship it"
	withMention.Entities = []Entity{{Type: "mention", Offset: 0, Length: 9}}
	msg, ok := inboundFromUpdate(Update{Message: &withMention}, "123", "acme_bot")
	if !ok || !msg.AddressedToBot {
		t.Fatalf("group with mention should be addressed; got ok=%v addressed=%v", ok, msg.AddressedToBot)
	}
	if msg.Text != "/issue Ship it" {
		t.Fatalf("mention not stripped: %q", msg.Text)
	}
}

func TestInboundFromUpdate_DropsBotAndEmptyAndChannel(t *testing.T) {
	if _, ok := inboundFromUpdate(Update{Message: &Message{From: &User{ID: 1, IsBot: true}, Chat: Chat{Type: "private"}, Text: "hi"}}, "123", "b"); ok {
		t.Fatal("bot sender should drop")
	}
	if _, ok := inboundFromUpdate(Update{Message: &Message{From: &User{ID: 1}, Chat: Chat{Type: "private"}, Text: "   "}}, "123", "b"); ok {
		t.Fatal("empty text should drop")
	}
	if _, ok := inboundFromUpdate(Update{Message: &Message{From: &User{ID: 1}, Chat: Chat{Type: "channel"}, Text: "x"}}, "123", "b"); ok {
		t.Fatal("channel post should drop")
	}
	if _, ok := inboundFromUpdate(Update{Message: nil}, "123", "b"); ok {
		t.Fatal("no message should drop")
	}
}

func TestInboundFromUpdate_StripsCommandBotSuffix(t *testing.T) {
	u := Update{Message: &Message{MessageID: 9, From: &User{ID: 2}, Chat: Chat{ID: 5, Type: "private"}, Text: "/issue@acme_bot Do the thing"}}
	msg, _ := inboundFromUpdate(u, "123", "acme_bot")
	if msg.Text != "/issue Do the thing" {
		t.Fatalf("command suffix not normalized: %q", msg.Text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `(cd server && go test ./internal/integrations/telegram/ -run TestInboundFromUpdate -v)`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

Implement the structs and `inboundFromUpdate`. Compute `addressed`; clean text (strip a leading `@botusername`, and rewrite a leading `/cmd@botusername` to `/cmd`); marshal `telegramRawEvent`; build `channel.InboundMessage` (`Type: channel.MsgTypeText`, `Source{ChannelType: TypeTelegram, ChatID, ChatType, SenderID, ThreadID: threadID}`). `threadID = ""` in v1 unless `MessageThreadID != 0` (then its decimal string). Mention detection: scan `Entities` for a `type=="mention"` whose substring equals `@botUsername`, or `type=="bot_command"` containing `@botUsername`; also accept reply-to-bot.

- [ ] **Step 4: Run test to verify it passes**

Run: `(cd server && go test ./internal/integrations/telegram/ -run TestInboundFromUpdate -v)`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/integrations/telegram/inbound.go server/internal/integrations/telegram/inbound_test.go
git commit -m "feat(telegram): normalize Update to channel.InboundMessage"
```

---

## Task 5: ResolverSet (installation / identity / dedup / session / audit)

**Files:**
- Create: `server/internal/integrations/telegram/resolvers.go`
- Test: `server/internal/integrations/telegram/resolvers_test.go`

**Interfaces:**
- Consumes: `engine.ResolverSet`, `engine.InstallationResolver`, `engine.IdentityResolver`, `engine.Deduper`, `engine.SessionBinder`, `engine.Auditor`, `engine.TxStarter`, `engine.NewChatSession`, `engine.SessionTitles`, the `channel.sql`-generated queries (`GetChannelInstallationByAppID`, `GetChannelUserBindingByUserID`, `GetMemberByUserAndWorkspace`, `ClaimChannelInboundDedup`, `MarkChannelInboundDedupProcessed`, `ReleaseChannelInboundDedup`, `RecordChannelInboundDrop`), and `inboundFromUpdate`'s `telegramRawEvent`.
- Produces:
  - `const originTelegramChat = "telegram_chat"`
  - `func NewTelegramResolverSet(q *db.Queries, tx engine.TxStarter, replier engine.OutboundReplier) engine.ResolverSet`

Telegram deltas vs `slack/resolvers.go`:
- **installationResolver**: decode `telegramRawEvent` from `msg.Raw`; `GetChannelInstallationByAppID(ChannelType: "telegram", AppID: raw.BotID)`; no team check (Telegram has no team). Map `pgx.ErrNoRows → engine.ErrInstallationNotFound`. Return `ResolvedInstallation{... Active: inst.Status == "active", Platform: inst}`.
- **identityResolver**: `GetChannelUserBindingByUserID(installation_id, senderID)`; on `pgx.ErrNoRows → engine.ErrSenderUnbound` (NO cross-installation reuse — delete Slack's `reusableBinding` path entirely). Then re-check membership via `GetMemberByUserAndWorkspace`; `pgx.ErrNoRows → engine.ErrSenderNotMember`. Return `ResolvedIdentity{UserID: binding.MulticaUserID}`.
- **deduper**: copy `slack/resolvers.go` deduper verbatim (uses the shared `channel_inbound_message_dedup` queries).
- **sessionBinder**: `telegramSessionRouting(msg)` returns `bindingKey = msg.Source.ChatID` (v1: one session per chat — no thread-root composite), `config = nil`, `replyThread = msg.Source.ThreadID`. Wrap `engine.NewChatSession(q, tx, TypeTelegram, engine.SessionTitles{Group: "Telegram group", Direct: "Telegram direct message", Fallback: "Telegram chat"})`. `EnsureSession`/`AppendMessage`/`BindMedia` mirror Slack (no media in v1 — `MediaResolver` stays nil; `BindMedia` delegates to `session.BindMediaRefs` with the passed refs, which are always empty).
- **auditor**: mirror Slack; decode `telegramRawEvent` for `EventType`.
- `NewTelegramResolverSet` assembles the set with `OriginType: originTelegramChat`, `Replier: replier` (may be nil in Plan 1 — passed non-nil in Plan 2), and no `Typing` (Telegram has no typing indicator in v1).

Add compile-time assertions `var _ engine.InstallationResolver = (*installationResolver)(nil)` etc.

- [ ] **Step 1: Write the failing test** (fakes for queries; no DB)

```go
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeIdentityQ struct {
	binding db.ChannelUserBinding
	bindErr error
	memErr  error
}

func (f fakeIdentityQ) GetChannelUserBindingByUserID(ctx context.Context, a db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	return f.binding, f.bindErr
}
func (f fakeIdentityQ) GetMemberByUserAndWorkspace(ctx context.Context, a db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	return db.Member{}, f.memErr
}

func inst() engine.ResolvedInstallation {
	return engine.ResolvedInstallation{ID: pgtype.UUID{Valid: true}, WorkspaceID: pgtype.UUID{Valid: true}}
}
func msgFrom(sender string) channel.InboundMessage {
	raw, _ := json.Marshal(telegramRawEvent{BotID: "123", EventType: "message"})
	return channel.InboundMessage{Source: channel.Source{ChannelType: TypeTelegram, SenderID: sender}, Raw: raw}
}

func TestIdentityResolver_Unbound(t *testing.T) {
	r := &identityResolver{q: fakeIdentityQ{bindErr: pgx.ErrNoRows}}
	_, err := r.ResolveSender(context.Background(), inst(), msgFrom("900"))
	if !errors.Is(err, engine.ErrSenderUnbound) {
		t.Fatalf("want ErrSenderUnbound, got %v", err)
	}
}

func TestIdentityResolver_BoundNonMember(t *testing.T) {
	r := &identityResolver{q: fakeIdentityQ{binding: db.ChannelUserBinding{MulticaUserID: pgtype.UUID{Valid: true}}, memErr: pgx.ErrNoRows}}
	_, err := r.ResolveSender(context.Background(), inst(), msgFrom("900"))
	if !errors.Is(err, engine.ErrSenderNotMember) {
		t.Fatalf("want ErrSenderNotMember, got %v", err)
	}
}

func TestIdentityResolver_BoundMember(t *testing.T) {
	uid := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	r := &identityResolver{q: fakeIdentityQ{binding: db.ChannelUserBinding{MulticaUserID: uid}}}
	got, err := r.ResolveSender(context.Background(), inst(), msgFrom("900"))
	if err != nil || got.UserID != uid {
		t.Fatalf("got (%v, %v)", got, err)
	}
}
```

Note: make `identityResolver.q` an interface `identityQueries` (only the two methods above) so this compiles with the fake, mirroring Slack's `identityQueries` pattern.

- [ ] **Step 2: Run test to verify it fails**

Run: `(cd server && go test ./internal/integrations/telegram/ -run TestIdentityResolver -v)`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

Port `slack/resolvers.go`, applying the deltas above. Delete `reusableBinding`, `installTeamID`, `installationServesTeam`, `slackBindingConfig`, and the typing notifier. `identityQueries` is `{ GetChannelUserBindingByUserID; GetMemberByUserAndWorkspace }`.

- [ ] **Step 4: Run test to verify it passes**

Run: `(cd server && go test ./internal/integrations/telegram/ -run TestIdentityResolver -v)`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/integrations/telegram/resolvers.go server/internal/integrations/telegram/resolvers_test.go
git commit -m "feat(telegram): engine ResolverSet (install/identity/dedup/session/audit)"
```

---

## Task 6: Install service (getMe validate → encrypt → setWebhook → persist; list/get/revoke)

**Files:**
- Create: `server/internal/integrations/telegram/install.go`
- Test: `server/internal/integrations/telegram/install_test.go`

**Interfaces:**
- Consumes: `secretbox.Box`, `engine.TxStarter`, `Client` (Task 2), `parseTelegramBotID`, `installConfig`, the install queries used by Slack (`UpsertChannelInstallation`, `ReclaimDeadChannelInstallationByAppID`, `GetChannelInstallationOwnerByAppID`, `ListChannelInstallationsByWorkspace`, `GetChannelInstallationInWorkspace`, `SetChannelInstallationStatus`).
- Produces:
  - `type InstallService struct { ... }`
  - `func NewInstallService(q *db.Queries, tx engine.TxStarter, box *secretbox.Box, publicURL string, logger *slog.Logger) (*InstallService, error)`
  - `type RegisterBYOParams struct { WorkspaceID, AgentID, InitiatorID pgtype.UUID; BotToken string }`
  - `func (s *InstallService) RegisterBYO(ctx context.Context, p RegisterBYOParams) (db.ChannelInstallation, error)`
  - `func (s *InstallService) ListByWorkspace(ctx, wsID) ([]db.ChannelInstallation, error)`
  - `func (s *InstallService) GetInWorkspace(ctx, id, wsID) (db.ChannelInstallation, error)`
  - `func (s *InstallService) Revoke(ctx, inst db.ChannelInstallation) error` — flips status AND calls `deleteWebhook`.
  - Sentinels: `ErrInstallationNotFound`, `ErrBotOwnedByAnotherWorkspace`, `ErrBotOwnedBySameWorkspace`, `ErrBotOwnedByArchivedAgent` (mirror Slack's team-conflict sentinels).
  - Test seam: `apiBase string` field (set via an unexported option or exported `SetAPIBaseForTest`) so `getMe`/`setWebhook` hit httptest.

`RegisterBYO` flow (Telegram delta vs Slack `byo_install.go`):
1. `botID, err := parseTelegramBotID(p.BotToken)` → `ErrInvalidBotToken` (handler maps to 400).
2. `info, err := NewClient(p.BotToken, apiBase...).GetMe(ctx)` — validates the token live and yields `Username`. On error → return a wrapped error (handler maps to 400 "could not verify the bot token").
3. Generate `webhookSecret` (32 random bytes, URL-safe — reuse the same `randomBindingToken(32)` helper shape, or `crypto/rand`).
4. `sealed, _ := s.box.Seal([]byte(p.BotToken))`; `cfgJSON = installConfig{AppID: botID, BotUsername: info.Username, BotTokenEncrypted: base64Std(sealed), WebhookSecret: webhookSecret}`.
5. `persistInstall` (copy Slack's `persistInstall` verbatim, `ChannelType: "telegram"`, `appIDKey: botID`) — keyed by `(workspace, agent, channel_type)`, reclaim dead by app_id, conflict → accurate sentinel via `liveOwnerConflictErr`.
6. **After** persist commits, register the webhook: `client.SetWebhook(ctx, s.publicURL + "/api/webhooks/telegram/" + botID, webhookSecret)`. If `setWebhook` fails, revoke the just-persisted row (best-effort) and return the error — do not leave an active install that receives nothing. (Mirrors Slack's "never persist a token that will silently never receive events," adapted: persist then register, roll back on failure.)

`Revoke`: `SetChannelInstallationStatus(id, "revoked")` then `client.DeleteWebhook` using the decrypted bot token from the row (best-effort; log on failure).

- [ ] **Step 1: Write the failing test** (fake queries + httptest for getMe/setWebhook)

Model on `slack/byo_install_test.go` / `install_test.go`. Minimum cases:
- `RegisterBYO` with an httptest server that answers `getMe` (ok) and `setWebhook` (ok) → returns a row whose `config->>'app_id'` equals the parsed bot id and whose `bot_username` matches; assert `setWebhook` was called with URL `.../api/webhooks/telegram/<botID>` and the stored secret.
- `RegisterBYO` with a malformed token (`"nocolon"`) → `ErrInvalidBotToken`, and neither getMe nor setWebhook is called.
- `RegisterBYO` where `getMe` returns `ok:false` → error; nothing persisted (assert the fake `UpsertChannelInstallation` was not called).

Use a fake `installQueries` (copy the interface + `dbInstallQueries` adapter pattern from `slack/install.go`) and a fake `TxStarter`. Seed the httptest handler to branch on `strings.HasSuffix(r.URL.Path, "/getMe")` vs `/setWebhook`.

- [ ] **Step 2: Run test to verify it fails**

Run: `(cd server && go test ./internal/integrations/telegram/ -run TestRegisterBYO -v)`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

Port `slack/install.go` (InstallService, `installQueries`, `dbInstallQueries`, `persistInstall`, `liveOwnerConflictErr`, `ListByWorkspace`, `GetInWorkspace`) and the relevant half of `byo_install.go`, applying the Telegram flow above. Rename Slack sentinels to Telegram (`ErrBotOwned*`). Add `publicURL` and `apiBase` fields.

- [ ] **Step 4: Run test to verify it passes**

Run: `(cd server && go test ./internal/integrations/telegram/ -run TestRegisterBYO -v)`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/integrations/telegram/install.go server/internal/integrations/telegram/install_test.go
git commit -m "feat(telegram): install service (getMe/setWebhook/persist, list/revoke)"
```

---

## Task 7: Protocol events + Handler fields

**Files:**
- Modify: `server/pkg/protocol/events.go:169` (after `EventSlackInstallationRevoked`)
- Modify: `server/internal/handler/handler.go` (the `Handler` struct — add fields near `SlackInstall`)
- Test: none new (compile-checked; covered by Task 8/9 handler tests)

**Interfaces:**
- Produces:
  - `EventTelegramInstallationCreated = "telegram_installation:created"`
  - `EventTelegramInstallationRevoked = "telegram_installation:revoked"`
  - `Handler.TelegramInstall *telegram.InstallService`

- [ ] **Step 1: Add the event constants**

In `server/pkg/protocol/events.go`, in the same const block as the Slack events:

```go
	EventTelegramInstallationCreated = "telegram_installation:created"
	EventTelegramInstallationRevoked = "telegram_installation:revoked"
```

- [ ] **Step 2: Add the Handler field**

In `server/internal/handler/handler.go`, add to the `Handler` struct (near `SlackInstall`):

```go
	TelegramInstall *telegram.InstallService
```

Add the import `"github.com/multica-ai/multica/server/internal/integrations/telegram"`.

- [ ] **Step 3: Verify it compiles**

Run: `(cd server && go build ./...)`
Expected: builds (field unused so far is fine; it's a struct field).

- [ ] **Step 4: Commit**

```bash
git add server/pkg/protocol/events.go server/internal/handler/handler.go
git commit -m "feat(telegram): installation events + handler field"
```

---

## Task 8: Management handlers (list / BYO install / revoke)

**Files:**
- Create: `server/internal/handler/telegram.go`
- Test: `server/internal/handler/telegram_test.go`

**Interfaces:**
- Consumes: `h.TelegramInstall`, `h.Queries.GetAgentInWorkspace`, `parseUUIDOrBadRequest`, `requireUserID`, `writeJSON`, `writeError`, `h.publish`, the protocol events (Task 7), `telegram.DecodePublicConfig`.
- Produces:
  - `type TelegramInstallationResponse struct { ID, WorkspaceID, AgentID, BotUsername, InstallerUserID, Status, InstalledAt, CreatedAt, UpdatedAt string }`
  - `func (h *Handler) ListTelegramInstallations(w, r)` — `GET /api/workspaces/{id}/telegram/installations`
  - `type RegisterTelegramBYORequest struct { BotToken string json:"bot_token" }`
  - `func (h *Handler) RegisterTelegramBYO(w, r)` — `POST /api/workspaces/{id}/telegram/install/byo?agent_id=…`
  - `func (h *Handler) RevokeTelegramInstallation(w, r)` — `DELETE /api/workspaces/{id}/telegram/installations/{installationId}`

Port `handler/slack.go` (list/BYO/revoke + `slackInstallationToResponse`/`publishSlackInstallationCreated`), applying: one token field (`bot_token`), `BotUsername` in the response (from `telegram.DecodePublicConfig`), Telegram sentinels for the error switch (`ErrInvalidBotToken` → 400; `ErrBotOwned*` → 409; default → 400 "could not verify the bot token — check that it is correct and the bot is not deleted"). `RevokeTelegramInstallation` loads the row via `GetInWorkspace` (workspace-scoped 404), then `h.TelegramInstall.Revoke(ctx, row)`.

- [ ] **Step 1: Write the failing test**

Mirror the pattern used by existing `handler/*_test.go` (construct a `Handler` with a fake/seeded `TelegramInstall` or a test DB per repo convention; check status codes and the malformed-response path). At minimum:
- `RegisterTelegramBYO` with `TelegramInstall == nil` → 503.
- `RegisterTelegramBYO` missing `agent_id` → 400.
- Successful install returns 200 with `bot_username` populated and publishes `telegram_installation:created` (assert via a fake publisher/bus per existing handler test convention).
- `RevokeTelegramInstallation` for a foreign workspace's id → 404.

Follow the exact construction/mocking style of `server/internal/handler/slack_test.go` if present; otherwise the nearest existing handler test. Do NOT mock `next/*` (backend test).

- [ ] **Step 2: Run test to verify it fails**

Run: `(cd server && go test ./internal/handler/ -run Telegram -v)`
Expected: FAIL — undefined handlers.

- [ ] **Step 3: Write minimal implementation**

Implement `server/internal/handler/telegram.go` per the port above.

- [ ] **Step 4: Run test to verify it passes**

Run: `(cd server && go test ./internal/handler/ -run Telegram -v)`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/telegram.go server/internal/handler/telegram_test.go
git commit -m "feat(telegram): management handlers (list/install/revoke)"
```

---

## Task 9: Public webhook handler (secret verify → normalize → Router.Handle)

**Files:**
- Create/extend: `server/internal/handler/telegram.go` (add `TelegramWebhook`)
- Test: `server/internal/handler/telegram_webhook_test.go`

**Interfaces:**
- Consumes: `h.Queries.GetChannelInstallationByAppID`, `h.ChannelRouter` (`engine.Router`, has `Handle(ctx, channel.InboundMessage) error`), `telegram.DecodePublicConfig` + a new exported `telegram.WebhookSecret(raw json.RawMessage) string` (add to `config.go`), `telegram.InboundFromUpdate` (export `inboundFromUpdate` as `InboundFromUpdate`, and `Update` is already exported).
- Produces:
  - `func (h *Handler) TelegramWebhook(w, r)` — `POST /api/webhooks/telegram/{botId}` (public/unauthenticated).

Flow:
1. `botID := chi.URLParam(r, "botId")` (trusted as an opaque routing key; not a UUID).
2. Load installation: `inst, err := h.Queries.GetChannelInstallationByAppID(ctx, {ChannelType: "telegram", AppID: botID})`. `pgx.ErrNoRows` or `inst.Status != "active"` → respond `200 OK` with empty body and return (never reveal existence; Telegram treats 200 as delivered — do NOT 401, or Telegram will retry forever).
3. Verify secret: `if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != telegram.WebhookSecret(inst.Config)` → `200 OK`, return (drop; optionally audit later). Constant-time compare via `subtle.ConstantTimeCompare`.
4. Decode body into `telegram.Update` (defensive: on decode error → `200 OK`, return).
5. `msg, ok := telegram.InboundFromUpdate(update, botID, telegram.DecodePublicConfig(inst.Config).BotUsername)`; if `!ok` → `200 OK`, return.
6. `_ = h.ChannelRouter.Handle(ctx, msg)` (product outcomes are not errors; an infra error is logged). Respond `200 OK`.

Rationale for always-200: Telegram retries non-2xx aggressively; the framework already dedups and audits, so the webhook should ACK fast and let the Router decide. Processing runs synchronously within the request but the Router's heavy work (reply/media) is already detached; for v1 this is acceptable (matches Slack's ACK-then-process shape closely enough).

- [ ] **Step 1: Write the failing test**

Construct a `Handler` with a test DB (or a fake `Queries` exposing `GetChannelInstallationByAppID`) seeded with a `telegram` installation whose config has a known `webhook_secret`, and a real `engine.Router` with the Telegram ResolverSet registered over a test DB — OR, to keep it a focused handler test, inject a fake `ChannelRouter` seam that records the `InboundMessage` it received. Prefer the latter: add a small interface `channelHandler interface { Handle(context.Context, channel.InboundMessage) error }` and have `TelegramWebhook` use `h.ChannelRouter` through it. Cases:
- Correct secret + valid `/issue` update → 200, and the fake router received a message with `Source.ChannelType == telegram` and the right `ChatID`.
- Wrong secret → 200, router NOT called.
- Unknown botId → 200, router NOT called.
- Malformed JSON body → 200, router NOT called, no panic.

- [ ] **Step 2: Run test to verify it fails**

Run: `(cd server && go test ./internal/handler/ -run TelegramWebhook -v)`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

Add `telegram.WebhookSecret`, export `InboundFromUpdate`, implement `TelegramWebhook` per the flow. Use `subtle.ConstantTimeCompare`.

- [ ] **Step 4: Run test to verify it passes**

Run: `(cd server && go test ./internal/handler/ -run TelegramWebhook -v)`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/telegram.go server/internal/handler/telegram_webhook_test.go server/internal/integrations/telegram/config.go server/internal/integrations/telegram/inbound.go
git commit -m "feat(telegram): public webhook handler (verify secret, route to engine)"
```

---

## Task 10: Wire Telegram into the server (router.go) + end-to-end verification

**Files:**
- Modify: `server/cmd/server/router.go` (wiring block after the Slack block ~line 569; routes near the Slack routes ~lines 988–993 and the public webhook near the GitHub/Stripe webhooks ~line 751)
- Test: `server/internal/integrations/telegram/e2e_test.go` (issue-creation over a real test DB) — optional but recommended

**Interfaces:**
- Consumes: `secretbox.LoadKey`/`New`, `telegram.NewInstallService`, `telegram.NewTelegramResolverSet`, `channelRouter.Register`, `appURLFromEnv`/`publicURLFromEnv` (find the existing public-URL helper used for webhooks).

- [ ] **Step 1: Add the wiring block** (after the Slack `if slackKey ...` block, ~line 569)

```go
	// Telegram integration (BYO bot). Inbound is a webhook, so — unlike Slack's
	// Socket Mode — it needs no Supervisor/Factory/lease: the webhook handler
	// calls channelRouter.Handle directly. Gated by MULTICA_TELEGRAM_SECRET_KEY
	// (at-rest bot-token encryption). The ResolverSet reuses the same channel_*
	// tables, IssueService and TaskService as Slack/Feishu, so /issue, dedup, and
	// run-triggering behave identically.
	if tgKey, err := secretbox.LoadKey("MULTICA_TELEGRAM_SECRET_KEY"); err == nil {
		box, err := secretbox.New(tgKey)
		if err != nil {
			slog.Error("telegram: secretbox.New failed; telegram integration disabled", "error", err)
		} else {
			// Replier is nil in Plan 1 (no account-linking prompt / issue-created
			// notice yet); Plan 2 passes a non-nil telegram.OutboundReplier.
			channelRouter.Register(telegram.TypeTelegram, telegram.NewTelegramResolverSet(queries, pool, nil))
			installSvc, ierr := telegram.NewInstallService(queries, pool, box, publicURLFromEnv(), slog.Default())
			if ierr != nil {
				slog.Error("telegram: InstallService init failed; install disabled", "error", ierr)
			} else {
				h.TelegramInstall = installSvc
			}
			slog.Info("telegram integration enabled (BYO webhook)")
		}
	} else {
		slog.Info("telegram integration disabled (MULTICA_TELEGRAM_SECRET_KEY not set)")
	}
```

Use the existing public-URL helper (the one Slack/webhooks use for the backend/API public URL — grep `PUBLIC_URL` in `router.go`; if the helper is named differently, use it). The webhook URL must be the backend host, not the app host.

- [ ] **Step 2: Add the public webhook route** (near the GitHub/Stripe webhooks, ~line 751, OUTSIDE the auth group)

```go
	r.Post("/api/webhooks/telegram/{botId}", h.TelegramWebhook)
```

- [ ] **Step 3: Add the management routes** (in the workspace admin/member split, mirroring Slack ~lines 988–993)

```go
		r.Get("/telegram/installations", h.ListTelegramInstallations)      // member-visible
		// admin-only sub-group (same split as Slack):
		r.Delete("/telegram/installations/{installationId}", h.RevokeTelegramInstallation)
		r.Post("/telegram/install/byo", h.RegisterTelegramBYO)
```

Place `Get` in the member-visible block and `Delete`/`Post` in the admin-only block, exactly matching the Slack placement.

- [ ] **Step 4: Verify build + vet + full package tests**

Run:
```bash
(cd server && go build ./... && go vet ./internal/integrations/telegram/... ./internal/handler/... && go test ./internal/integrations/telegram/... ./internal/handler/ -run Telegram -v)
```
Expected: builds; vet clean; tests PASS.

- [ ] **Step 5: (Recommended) end-to-end issue-creation test over a test DB**

Write `telegram/e2e_test.go` using the repo's Go test-DB harness (find how `slack`/engine tests spin up a `*db.Queries` against the `pgvector` test database; reuse that helper). Seed: a workspace, an agent, a `telegram` `channel_installation` (config with `app_id`, `webhook_secret`, encrypted token via an identity box), a member, and a `channel_user_binding` for a Telegram user id → that member. Build a `channelRouter` with `NewTelegramResolverSet` + a **fake IssueCreator/TaskEnqueuer** (do NOT execute a real agent CLI — inject fakes satisfying `engine.IssueCreator`/`engine.TaskEnqueuer` that record calls). Feed an `inboundFromUpdate` of `/issue Fix login` and assert the fake `IssueCreator.Create` was called with `WorkspaceID`, `Title == "Fix login"`, `AssigneeType == "agent"`, `AssigneeID == agentID`, `CreatorType == "member"`, `CreatorID == memberUserID`. Feed a plain `hello` message and assert `IssueCreator.Create` is NOT called but `TaskEnqueuer.EnqueueChatTask` IS.

Run: `(cd server && go test ./internal/integrations/telegram/ -run TestE2E -v)`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/cmd/server/router.go server/internal/integrations/telegram/e2e_test.go
git commit -m "feat(telegram): wire inbound webhook + install into server"
```

---

## Plan 1 self-review (done at authoring)

- **Spec coverage:** inbound webhook (Task 9/10), route by `config->>'app_id'` (Task 5/9), no allowlist/gated by membership (Task 5 identity), `/issue`→issue + plain→chat (Task 10 e2e), BYO config + `setWebhook` (Task 6), no new tables/migration (all tasks reuse `channel.sql`), member attribution via `channel_user_binding` (Task 5). Account-linking *prompt* + redeem, outbound agent replies, and frontend are **explicitly deferred to Plans 2–4** below (not gaps).
- **Type consistency:** `installConfig`, `credentials`, `parseTelegramBotID`, `TypeTelegram`, `inboundFromUpdate`/`InboundFromUpdate`, `telegramRawEvent`, `NewTelegramResolverSet`, `InstallService`, handler names are used consistently across tasks.
- **Deferred-by-design:** Plan 1 has NO way to create a `channel_user_binding` through the product yet (that's Plan 2's redeem flow). Until Plan 2 lands, a real Telegram user is always `NeedsBinding` and no issue is created for them; Plan 1 is validated by seeding a binding directly in the e2e test. This is called out so it is not mistaken for a bug.

---

## Follow-on plans (to be written after Plan 1 is reviewed)

- **Plan 2 — Account linking:** `telegram/binding.go` (`BindingTokenService.Mint`/`RedeemAndBind`, mirroring `slack/binding.go`), `telegram/replier.go` (`OutboundReplier` — `NeedsBinding` prompt with a `/telegram/bind?token=…` link, offline/archived/issue-created notices), `RedeemTelegramBindingToken` handler + `POST /api/telegram/binding/redeem` route, and the frontend bind page `apps/web/app/telegram/bind/page.tsx` → `packages/views/telegram/bind-page.tsx`. Wire the replier into `NewTelegramResolverSet` (replace the Plan 1 `nil`).
- **Plan 3 — Outbound agent chat replies:** `telegram/outbound.go` (`Outbound` EventChatDone subscriber, mirroring `slack/outbound.go`) so a plain-message chat run's reply is delivered back to the Telegram chat via `sendMessage`; register on the bus in `router.go`.
- **Plan 4 — Config UI:** `packages/core/telegram/queries.ts` + types, per-agent Integrations connect (paste bot token), workspace settings Integrations list/disconnect, and the malformed-response schema test — mirroring the Slack views.
