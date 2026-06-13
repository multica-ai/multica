package lark

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type fakePatcherQueries struct {
	mu              sync.Mutex
	binding         db.LarkChatSessionBinding
	bindingErr      error
	installation    db.LarkInstallation
	installationErr error
	agent           db.Agent
	agentErr        error
	card            db.LarkOutboundCardMessage
	cardErr         error
	created         []db.CreateLarkOutboundCardMessageParams
	createReturn    db.LarkOutboundCardMessage
	statusUpdates   []db.UpdateLarkOutboundCardStatusParams
}

func (f *fakePatcherQueries) GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error) {
	return db.AgentTaskQueue{}, nil
}
func (f *fakePatcherQueries) GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error) {
	return db.ChatSession{}, nil
}
func (f *fakePatcherQueries) GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error) {
	return f.agent, f.agentErr
}
func (f *fakePatcherQueries) GetLarkInstallation(ctx context.Context, id pgtype.UUID) (db.LarkInstallation, error) {
	return f.installation, f.installationErr
}
func (f *fakePatcherQueries) GetLarkChatSessionBindingBySession(ctx context.Context, sessID pgtype.UUID) (db.LarkChatSessionBinding, error) {
	return f.binding, f.bindingErr
}
func (f *fakePatcherQueries) GetLarkOutboundCardByTask(ctx context.Context, taskID pgtype.UUID) (db.LarkOutboundCardMessage, error) {
	return f.card, f.cardErr
}
func (f *fakePatcherQueries) CreateLarkOutboundCardMessage(ctx context.Context, arg db.CreateLarkOutboundCardMessageParams) (db.LarkOutboundCardMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, arg)
	return f.createReturn, nil
}
func (f *fakePatcherQueries) UpdateLarkOutboundCardStatus(ctx context.Context, arg db.UpdateLarkOutboundCardStatusParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusUpdates = append(f.statusUpdates, arg)
	return nil
}

type fakeCredentials struct{ secret string }

func (f fakeCredentials) DecryptAppSecret(inst db.LarkInstallation) (string, error) {
	return f.secret, nil
}

type fakeAPIClient struct {
	mu             sync.Mutex
	sent           []SendCardParams
	patched        []PatchCardParams
	textSent       []SendTextParams
	mdCardSent     []SendMarkdownCardParams
	sendReturn     string
	sendErr        error
	patchErr       error
	textSendErr    error
	textSendReturn string
	mdCardErr      error
	mdCardReturn   string
	bindingSent    []BindingPromptParams
}

func (f *fakeAPIClient) IsConfigured() bool { return true }

func (f *fakeAPIClient) SendInteractiveCard(ctx context.Context, p SendCardParams) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, p)
	return f.sendReturn, f.sendErr
}
func (f *fakeAPIClient) PatchInteractiveCard(ctx context.Context, p PatchCardParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patched = append(f.patched, p)
	return f.patchErr
}
func (f *fakeAPIClient) SendTextMessage(ctx context.Context, p SendTextParams) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.textSent = append(f.textSent, p)
	return f.textSendReturn, f.textSendErr
}
func (f *fakeAPIClient) SendMarkdownCard(ctx context.Context, p SendMarkdownCardParams) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mdCardSent = append(f.mdCardSent, p)
	return f.mdCardReturn, f.mdCardErr
}
func (f *fakeAPIClient) SendBindingPromptCard(ctx context.Context, p BindingPromptParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bindingSent = append(f.bindingSent, p)
	return nil
}
func (f *fakeAPIClient) GetBotInfo(ctx context.Context, creds InstallationCredentials) (BotInfo, error) {
	return BotInfo{}, nil
}
func (f *fakeAPIClient) GetMessage(ctx context.Context, creds InstallationCredentials, messageID string) ([]LarkMessage, error) {
	return nil, nil
}
func (f *fakeAPIClient) ListChatMessages(ctx context.Context, creds InstallationCredentials, p ListMessagesParams) ([]LarkMessage, error) {
	return nil, nil
}
func (f *fakeAPIClient) BatchGetUsers(ctx context.Context, creds InstallationCredentials, openIDs []string) (map[string]string, error) {
	return nil, nil
}
func (f *fakeAPIClient) AddMessageReaction(ctx context.Context, p AddReactionParams) (string, error) {
	return "fake-reaction-id", nil
}
func (f *fakeAPIClient) DeleteMessageReaction(ctx context.Context, p DeleteReactionParams) error {
	return nil
}

func newTestPatcher(t *testing.T) (*Patcher, *fakePatcherQueries, *fakeAPIClient) {
	t.Helper()
	q := &fakePatcherQueries{
		binding: db.LarkChatSessionBinding{
			ChatSessionID:  uuidFromString(t, "cccccccc-cccc-cccc-cccc-cccccccccccc"),
			InstallationID: uuidFromString(t, "1111aaaa-1111-1111-1111-111111111111"),
			LarkChatID:     "oc_test_chat",
			LarkChatType:   "p2p",
		},
		installation: db.LarkInstallation{
			ID:                 uuidFromString(t, "1111aaaa-1111-1111-1111-111111111111"),
			AppID:              "cli_test_app",
			AppSecretEncrypted: []byte("ciphertext"),
			Status:             string(InstallationActive),
			AgentID:            uuidFromString(t, "aaaa1111-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		},
		agent:   db.Agent{Name: "TestAgent"},
		cardErr: pgx.ErrNoRows,
	}
	api := &fakeAPIClient{sendReturn: "lark_card_msg_1", textSendReturn: "lark_text_msg_1"}
	p := NewPatcher(q, fakeCredentials{secret: "shh"}, api, PatcherConfig{
		Logger: newDiscardLogger(),
		Now:    time.Now,
	})
	return p, q, api
}

// TestPatcherSendsPlainTextOnChatDone pins the new behaviour Bohan asked
// for: when the agent finishes replying, the Patcher posts the reply as
// a plain Lark IM text message (msg_type=text), not nested inside an
// interactive card. This is the load-bearing UX call — the prior card
// chrome made every reply look like a system notification.
func TestPatcherSendsPlainTextOnChatDone(t *testing.T) {
	p, q, api := newTestPatcher(t)
	taskID := uuidFromString(t, "ee333333-ee33-ee33-ee33-eeeeeeeeeeee")

	p.handleEvent(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        uuidString(taskID),
		ChatSessionID: uuidString(q.binding.ChatSessionID),
		Payload: protocol.ChatDonePayload{
			TaskID:        uuidString(taskID),
			ChatSessionID: uuidString(q.binding.ChatSessionID),
			Content:       "Hello! I'm cc, a coding agent…",
		},
	})

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.textSent) != 1 {
		t.Fatalf("expected one SendTextMessage call on ChatDone; got %d", len(api.textSent))
	}
	got := api.textSent[0]
	if got.Text != "Hello! I'm cc, a coding agent…" {
		t.Errorf("text mismatch: got %q", got.Text)
	}
	if got.ChatID != ChatID(q.binding.LarkChatID) {
		t.Errorf("chat_id mismatch: got %q want %q", got.ChatID, q.binding.LarkChatID)
	}
	if got.InstallationID.AppID != "cli_test_app" {
		t.Errorf("expected installation app_id propagated; got %q", got.InstallationID.AppID)
	}
	if len(api.sent) != 0 || len(api.patched) != 0 {
		t.Errorf("ChatDone must NOT send / patch any card; got sent=%d patched=%d",
			len(api.sent), len(api.patched))
	}
}

// TestPatcherRoutesMarkdownReplyToCard pins the two-path chat reply:
// when the agent's body contains markdown syntax, the Patcher MUST
// route to SendMarkdownCard (schema-2.0 interactive card with a
// `tag: "markdown"` body element) so Lark renders the formatting
// instead of leaving raw `**bold**` / `# heading` characters in the
// transcript. Plain prose continues to go through SendTextMessage.
func TestPatcherRoutesMarkdownReplyToCard(t *testing.T) {
	p, q, api := newTestPatcher(t)
	taskID := uuidFromString(t, "ee444444-ee44-ee44-ee44-eeeeeeeeeeee")

	body := "# Summary\n\n- bullet one\n- bullet two\n\n```go\nfunc f() {}\n```\n"
	p.handleEvent(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        uuidString(taskID),
		ChatSessionID: uuidString(q.binding.ChatSessionID),
		Payload: protocol.ChatDonePayload{
			TaskID:        uuidString(taskID),
			ChatSessionID: uuidString(q.binding.ChatSessionID),
			Content:       body,
		},
	})

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.mdCardSent) != 1 {
		t.Fatalf("expected one SendMarkdownCard call; got %d", len(api.mdCardSent))
	}
	got := api.mdCardSent[0]
	if got.Markdown != body {
		t.Errorf("markdown body must be forwarded verbatim; got %q", got.Markdown)
	}
	if got.ChatID != ChatID(q.binding.LarkChatID) {
		t.Errorf("chat_id mismatch: got %q want %q", got.ChatID, q.binding.LarkChatID)
	}
	if len(api.textSent) != 0 {
		t.Errorf("markdown body must NOT also fire SendTextMessage; got %d", len(api.textSent))
	}
	if len(api.sent) != 0 || len(api.patched) != 0 {
		t.Errorf("ChatDone must NOT use legacy card paths; sent=%d patched=%d", len(api.sent), len(api.patched))
	}
}

// TestPatcherRoutesPlainReplyToText is the inverse: a short prose
// reply without any markdown syntax should stay on the cheap
// msg_type=text path so the user sees a normal IM bubble.
func TestPatcherRoutesPlainReplyToText(t *testing.T) {
	p, q, api := newTestPatcher(t)
	taskID := uuidFromString(t, "ee555555-ee55-ee55-ee55-eeeeeeeeeeee")

	p.handleEvent(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        uuidString(taskID),
		ChatSessionID: uuidString(q.binding.ChatSessionID),
		Payload: protocol.ChatDonePayload{
			TaskID:        uuidString(taskID),
			ChatSessionID: uuidString(q.binding.ChatSessionID),
			Content:       "Sure, on it.",
		},
	})

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.textSent) != 1 {
		t.Fatalf("plain prose must take the text path; got %d text sends", len(api.textSent))
	}
	if len(api.mdCardSent) != 0 {
		t.Errorf("plain prose must NOT wrap in a markdown card; got %d card sends", len(api.mdCardSent))
	}
}

// TestPatcherDropsEmptyChatReply guards the fallback we deliberately
// removed: the previous design rendered "Done." when content was
// empty. Now an empty Content is silently dropped (no text message
// sent at all). Showing nothing is better than showing the misleading
// "Done." fallback, which Bohan reported confused him in the live env.
func TestPatcherDropsEmptyChatReply(t *testing.T) {
	p, q, api := newTestPatcher(t)
	taskID := uuidFromString(t, "ee777777-ee77-ee77-ee77-eeeeeeeeeeee")

	p.handleEvent(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        uuidString(taskID),
		ChatSessionID: uuidString(q.binding.ChatSessionID),
		Payload: protocol.ChatDonePayload{
			TaskID:        uuidString(taskID),
			ChatSessionID: uuidString(q.binding.ChatSessionID),
			Content:       "",
		},
	})

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.textSent) != 0 {
		t.Errorf("empty content must drop, not render the Done. fallback; got %d text sends", len(api.textSent))
	}
}

func TestPatcherSkipsWhenNoChatSessionBinding(t *testing.T) {
	p, q, api := newTestPatcher(t)
	q.bindingErr = pgx.ErrNoRows

	p.handleEvent(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        uuidString(uuidFromString(t, "ee222222-ee22-ee22-ee22-eeeeeeeeeeee")),
		ChatSessionID: uuidString(uuidFromString(t, "cc222222-cc22-cc22-cc22-cccccccccccc")),
		Payload: protocol.ChatDonePayload{
			Content: "irrelevant — no binding",
		},
	})

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.textSent) != 0 || len(api.sent) != 0 {
		t.Fatalf("web-only chat sessions must produce no outbound; got text=%d cards=%d",
			len(api.textSent), len(api.sent))
	}
}

// TestPatcherFailEventSendsErrorCard verifies the failure path still
// surfaces a card. The visual distinction between a successful reply
// (plain text bubble) and a failure (red header card) is genuinely
// useful — and failures are rare enough that the card chrome isn't
// noisy. One-shot send (no patching of any prior thinking card,
// because there isn't one anymore).
func TestPatcherFailEventSendsErrorCard(t *testing.T) {
	p, q, api := newTestPatcher(t)
	taskID := uuidFromString(t, "ee444444-ee44-ee44-ee44-eeeeeeeeeeee")

	p.handleEvent(events.Event{
		Type:          protocol.EventTaskFailed,
		TaskID:        uuidString(taskID),
		ChatSessionID: uuidString(q.binding.ChatSessionID),
		Payload: map[string]any{
			"task_id":         uuidString(taskID),
			"chat_session_id": uuidString(q.binding.ChatSessionID),
			"error":           "boom",
		},
	})

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.sent) != 1 {
		t.Fatalf("fail event must send an error card; got %d card sends", len(api.sent))
	}
	if len(api.patched) != 0 {
		t.Errorf("fail event must NOT patch any card (no prior card lifecycle); got %d patches", len(api.patched))
	}
	if !strings.Contains(api.sent[0].CardJSON, "boom") {
		t.Errorf("error card body should embed the error message; got %s", api.sent[0].CardJSON)
	}
}

func TestPatcherSwallowsInstallationLoadErrors(t *testing.T) {
	p, q, api := newTestPatcher(t)
	q.installationErr = errors.New("db down")

	p.handleEvent(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        uuidString(uuidFromString(t, "ee555555-ee55-ee55-ee55-eeeeeeeeeeee")),
		ChatSessionID: uuidString(q.binding.ChatSessionID),
		Payload: protocol.ChatDonePayload{
			Content: "would-be reply",
		},
	})

	// The patcher logs but never panics; no outbound.
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.textSent) != 0 || len(api.sent) != 0 {
		t.Fatalf("DB failure must not produce outbound; got text=%d cards=%d",
			len(api.textSent), len(api.sent))
	}
}

// TestPatcherIgnoresEventTaskCompletedForChatTasks pins the no-extra-send
// invariant. TaskService publishes ChatDone (with content) immediately
// before TaskCompleted (without content) for every chat task. The
// Patcher must NOT react to TaskCompleted — doing so would either
// re-send the same text reply (duplicate bubble) or send the "Done."
// fallback (the original bug Bohan reported). The fix is to leave
// EventTaskCompleted unsubscribed; this test asserts exactly one
// outbound text message from the sequence.
func TestPatcherIgnoresEventTaskCompletedForChatTasks(t *testing.T) {
	p, q, api := newTestPatcher(t)
	taskID := uuidFromString(t, "ee666666-ee66-ee66-ee66-eeeeeeeeeeee")

	// Step 1: ChatDone arrives with the real agent reply. Plain text
	// is sent to Lark.
	p.handleEvent(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        uuidString(taskID),
		ChatSessionID: uuidString(q.binding.ChatSessionID),
		Payload: protocol.ChatDonePayload{
			TaskID:        uuidString(taskID),
			ChatSessionID: uuidString(q.binding.ChatSessionID),
			Content:       "Hello! I'm cc, a coding agent…",
		},
	})

	// Step 2: TaskCompleted fires immediately after with no content.
	// The Patcher MUST NOT send a second message — neither a
	// duplicate of the reply nor the "Done." fallback.
	p.handleEvent(events.Event{
		Type:          protocol.EventTaskCompleted,
		TaskID:        uuidString(taskID),
		ChatSessionID: uuidString(q.binding.ChatSessionID),
		Payload: map[string]any{
			"task_id":         uuidString(taskID),
			"chat_session_id": uuidString(q.binding.ChatSessionID),
			"status":          "completed",
		},
	})

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.textSent) != 1 {
		t.Fatalf("exactly one text send expected (ChatDone); EventTaskCompleted must be ignored. Got %d sends", len(api.textSent))
	}
	if api.textSent[0].Text != "Hello! I'm cc, a coding agent…" {
		t.Errorf("text content mismatch; got %q", api.textSent[0].Text)
	}
	if len(api.sent) != 0 || len(api.patched) != 0 {
		t.Errorf("no card outbound expected on the success path; got sent=%d patched=%d",
			len(api.sent), len(api.patched))
	}
}

// TestDefaultRendererConfigCarriesUpdateMulti pins the streaming-card
// contract: Lark refuses PatchInteractiveCard on a card whose config
// does not declare update_multi=true. Since the Patcher's whole
// raison d'être is to send a thinking card and then patch it forward
// to streaming/final/error, ANY kind missing update_multi would make
// the patch silently no-op against Lark while the local DB row still
// flips. Hence the assertion covers every kind, not just the final
// patched kinds.
func TestDefaultRendererConfigCarriesUpdateMulti(t *testing.T) {
	r := NewDefaultRenderer()
	for _, kind := range []CardKind{CardKindThinking, CardKindRunning, CardKindFinal, CardKindError} {
		t.Run(string(kind), func(t *testing.T) {
			out, err := r.Render(RenderInput{Kind: kind, Content: "x", ErrorMessage: "y"})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(out.JSON), &doc); err != nil {
				t.Fatalf("decode card json: %v", err)
			}
			cfg, ok := doc["config"].(map[string]any)
			if !ok {
				t.Fatalf("missing config block: %v", doc)
			}
			if v, _ := cfg["update_multi"].(bool); !v {
				t.Errorf("config.update_multi must be true so subsequent patches apply; got %v", cfg)
			}
			if v, _ := cfg["wide_screen_mode"].(bool); !v {
				t.Errorf("config.wide_screen_mode regression: %v", cfg)
			}
		})
	}
}
