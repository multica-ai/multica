package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CardStatus mirrors lark_outbound_card_message.status. Kept as a typed
// alias so callers can't pass arbitrary strings into the status column.
type CardStatus string

const (
	CardStatusPending   CardStatus = "pending"
	CardStatusStreaming CardStatus = "streaming"
	CardStatusFinal     CardStatus = "final"
	CardStatusError     CardStatus = "error"
)

const (
	larkStreamPatchInterval = 800 * time.Millisecond
	larkThinkingLimit       = 8000
	larkAnswerLimit         = 24000
	larkToolInputLimit      = 1200
)

// CardKind enumerates the small set of card variants the patcher
// renders. The Renderer is plug-replaceable so the on-wire card
// template can evolve without touching the patcher's transport / DB
// logic.
type CardKind string

const (
	CardKindThinking CardKind = "thinking"
	CardKindRunning  CardKind = "running"
	CardKindFinal    CardKind = "final"
	CardKindError    CardKind = "error"
)

// CardRender is the rendered card body the Renderer produces. The
// patcher serializes the JSON before handing it to APIClient.
type CardRender struct {
	JSON string
}

// RenderInput is the (typed) snapshot the Renderer sees when building
// or patching a card. Fields are populated as they become available
// during a task lifecycle — IssueNumber is set for `/issue` flows,
// Content is set for completed chat tasks, ErrorMessage for failed.
type RenderInput struct {
	Kind         CardKind
	AgentName    string
	IssueNumber  int32
	IssueID      pgtype.UUID
	TaskID       pgtype.UUID
	Thinking     string
	Answer       string
	Content      string
	Expanded     bool
	Running      bool
	ErrorMessage string
}

// Renderer turns a typed RenderInput into the actual Lark card JSON.
// Centralizing this lets us swap card templates (or A/B them) without
// touching event subscription or persistence code.
type Renderer interface {
	Render(in RenderInput) (CardRender, error)
}

// defaultRenderer produces the streaming chat-reply card. Keep the
// body intentionally quiet: one collapsible thinking panel plus the
// assistant answer, with no source/footer chrome.
type defaultRenderer struct{}

// NewDefaultRenderer returns the production-default Renderer. Override
// via PatcherConfig.Renderer when a custom template is needed.
func NewDefaultRenderer() Renderer { return &defaultRenderer{} }

func (defaultRenderer) Render(in RenderInput) (CardRender, error) {
	thinking := strings.TrimSpace(in.Thinking)
	if thinking == "" {
		thinking = "暂无思考过程"
	}
	thinking = trimRunesFromEnd(thinking, larkThinkingLimit)

	answer := in.Answer
	if answer == "" {
		answer = in.Content
	}
	switch in.Kind {
	case CardKindThinking:
		in.Running = true
		in.Expanded = true
	case CardKindRunning:
		in.Running = true
	case CardKindFinal:
		in.Running = false
		in.Expanded = false
	case CardKindError:
		in.Running = false
		if in.ErrorMessage != "" {
			answer = "**运行失败**\n\n" + in.ErrorMessage
		} else if answer == "" {
			answer = "**运行失败**"
		}
	default:
		return CardRender{}, fmt.Errorf("unknown card kind %q", in.Kind)
	}
	answer = trimRunes(answer, larkAnswerLimit)
	if in.Running {
		if strings.TrimSpace(answer) == "" {
			answer = "处理中…"
		}
		answer += "▌"
	}
	if strings.TrimSpace(answer) == "" {
		answer = " "
	}

	doc := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"wide_screen_mode": true,
			"update_multi":     true,
		},
		"body": map[string]any{
			"elements": []any{
				map[string]any{
					"tag":      "collapsible_panel",
					"expanded": in.Expanded,
					"border": map[string]any{
						"color":         "grey",
						"corner_radius": "8px",
					},
					"padding": "0px 0px 10px 0px",
					"header": map[string]any{
						"title": map[string]any{
							"tag":     "plain_text",
							"content": "查看思考过程",
						},
						"icon": map[string]any{
							"tag":   "standard_icon",
							"token": "down-small-ccm_outlined",
							"color": "grey",
						},
						"icon_position":    "right",
						"background_color": "grey",
						"padding":          "10px 12px 10px 12px",
						"vertical_align":   "center",
					},
					"elements": []any{
						map[string]any{
							"tag":       "markdown",
							"content":   greyMarkdown(thinking),
							"text_size": "normal",
							"margin":    "10px 12px 0px 12px",
						},
					},
				},
				map[string]any{
					"tag":       "markdown",
					"content":   answer,
					"text_size": "normal",
					"margin":    "12px 0px 0px 0px",
				},
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return CardRender{}, err
	}
	return CardRender{JSON: string(raw)}, nil
}

// PatcherQueries is the narrow subset of *db.Queries the Patcher
// needs. Declared as an interface so the patcher is unit-testable
// without a real Postgres connection.
type PatcherQueries interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
	GetLarkInstallation(ctx context.Context, id pgtype.UUID) (db.LarkInstallation, error)
	GetLarkChatSessionBindingBySession(ctx context.Context, chatSessionID pgtype.UUID) (db.LarkChatSessionBinding, error)
	GetLarkOutboundCardByTask(ctx context.Context, taskID pgtype.UUID) (db.LarkOutboundCardMessage, error)
	CreateLarkOutboundCardMessage(ctx context.Context, arg db.CreateLarkOutboundCardMessageParams) (db.LarkOutboundCardMessage, error)
	UpdateLarkOutboundCardStatus(ctx context.Context, arg db.UpdateLarkOutboundCardStatusParams) error
}

// CredentialsResolver decrypts an installation's app_secret for the
// transport layer. *InstallationService satisfies it directly; tests
// substitute a fake.
type CredentialsResolver interface {
	DecryptAppSecret(inst db.LarkInstallation) (string, error)
}

// PatcherConfig tunes the outbound Patcher. Defaults via withDefaults;
// tests typically override Renderer / Now / Logger.
type PatcherConfig struct {
	// Renderer drives the Lark streaming reply card template. The
	// patcher owns transport and persistence; the renderer owns the
	// schema-2.0 JSON shape.
	Renderer Renderer
	Now      func() time.Time
	Logger   *slog.Logger
}

func (c PatcherConfig) withDefaults() PatcherConfig {
	if c.Renderer == nil {
		c.Renderer = NewDefaultRenderer()
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Patcher reacts to task-lifecycle events and forwards chat replies to
// Lark as a single updatable interactive card. Runtime thinking and
// tool-use instructions stream into a collapsible panel while answer
// text streams below it; the final patch collapses the panel.
//
// Scope:
//
//   - Only tasks whose chat_session has a lark_chat_session_binding
//     produce outbound. Tasks born from the web UI or autopilot pass
//     through unchanged.
//
//   - EventTaskMessage starts or patches the card. EventChatDone
//     finalizes it. EventTaskFailed patches or creates an error card.
//
//   - Multi-replica safety is inherited from the inbound WS lease: at
//     most one replica holds the installation lease at a time, the
//     event bus is per-process, so exactly one Patcher reacts per run.
type Patcher struct {
	queries         PatcherQueries
	credentials     CredentialsResolver
	client          APIClient
	typingIndicator *TypingIndicatorManager
	cfg             PatcherConfig

	mu      sync.Mutex
	flushMu sync.Mutex
	streams map[string]*larkStreamState
}

type larkStreamState struct {
	taskID            pgtype.UUID
	chatSessionID     pgtype.UUID
	cardID            pgtype.UUID
	larkCardMessageID string
	thinking          string
	answer            string
	lastSeq           int
	lastPatchAt       time.Time
	status            CardStatus
	typingCleared     bool
	startedNoted      bool
	answerNoted       bool
}

type larkStreamSnapshot struct {
	taskID            pgtype.UUID
	chatSessionID     pgtype.UUID
	cardID            pgtype.UUID
	larkCardMessageID string
	thinking          string
	answer            string
	expanded          bool
	running           bool
	errorMessage      string
	status            CardStatus
}

// NewPatcher constructs a Patcher bound to its dependencies. The
// patcher does not subscribe to the bus until Register is called.
func NewPatcher(queries PatcherQueries, credentials CredentialsResolver, client APIClient, cfg PatcherConfig) *Patcher {
	cfg = cfg.withDefaults()
	return &Patcher{
		queries:     queries,
		credentials: credentials,
		client:      client,
		cfg:         cfg,
		streams:     make(map[string]*larkStreamState),
	}
}

// SetTypingIndicatorManager wires the typing-indicator manager into the
// patcher so that replies clear the "processing" reaction before they
// are sent. Call once at boot after both the patcher and manager are
// constructed. Nil disables the clear step.
func (p *Patcher) SetTypingIndicatorManager(m *TypingIndicatorManager) {
	p.typingIndicator = m
}

// Register subscribes the patcher to the task-lifecycle events it
// cares about on the supplied bus. Idempotent only if you call it
// against a fresh bus; call sites should invoke it exactly once
// during server boot (after the bus + patcher are constructed and
// before HTTP traffic starts).
//
// EventTaskCompleted is intentionally ignored: chat tasks publish
// EventChatDone with the persisted assistant message, and that payload
// is the authoritative final answer.
func (p *Patcher) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventTaskRunning, p.handleEvent)
	bus.Subscribe(protocol.EventTaskMessage, p.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, p.handleEvent)
	bus.Subscribe(protocol.EventChatDone, p.handleEvent)
}

func (p *Patcher) handleEvent(e events.Event) {
	// Use a fresh background ctx with a tight timeout: bus delivery is
	// synchronous so a stuck Lark HTTP call would otherwise wedge the
	// whole publish call site.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.processEvent(ctx, e); err != nil {
		p.cfg.Logger.Warn("lark patcher: event handling failed",
			"event_type", e.Type,
			"task_id", e.TaskID,
			"chat_session_id", e.ChatSessionID,
			"error", err,
		)
	}
}

func (p *Patcher) processEvent(ctx context.Context, e events.Event) error {
	taskID, chatSessionID, ok := taskAndSessionFromEvent(e)
	if !ok {
		return nil
	}
	if !chatSessionID.Valid {
		task, err := p.queries.GetAgentTask(ctx, taskID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("lookup task: %w", err)
		}
		chatSessionID = task.ChatSessionID
		if !chatSessionID.Valid {
			// Issue / autopilot tasks have no chat_session.
			return nil
		}
	}

	binding, err := p.queries.GetLarkChatSessionBindingBySession(ctx, chatSessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Web-only chat session — not a Lark target.
			return nil
		}
		return fmt.Errorf("lookup chat session binding: %w", err)
	}

	inst, err := p.queries.GetLarkInstallation(ctx, binding.InstallationID)
	if err != nil {
		return fmt.Errorf("load installation: %w", err)
	}
	if InstallationStatus(inst.Status) != InstallationActive {
		// Revoked between trigger and event; nothing to patch.
		return nil
	}
	creds, err := p.installationCredentials(inst)
	if err != nil {
		return err
	}

	switch e.Type {
	case protocol.EventTaskRunning:
		return p.startStreamCard(ctx, creds, binding, taskID, chatSessionID)
	case protocol.EventTaskMessage:
		return p.streamTaskMessage(ctx, creds, binding, taskID, chatSessionID, e.Payload)
	case protocol.EventChatDone:
		return p.finishChatReply(ctx, creds, binding, taskID, chatSessionID, e.Payload)
	case protocol.EventTaskFailed:
		return p.fail(ctx, creds, binding, taskID, chatSessionID, e.Payload)
	}
	return nil
}

func (p *Patcher) startStreamCard(ctx context.Context, creds InstallationCredentials, binding db.LarkChatSessionBinding, taskID, chatSessionID pgtype.UUID) error {
	p.mu.Lock()
	st := p.streamStateLocked(taskID, chatSessionID)
	if st.larkCardMessageID != "" || st.status == CardStatusFinal || st.status == CardStatusError {
		p.mu.Unlock()
		return nil
	}
	addRuntimeNoteLocked(st, "Agent 已开始处理请求。")
	snap := snapshotFromState(st, true, true, "", CardStatusStreaming)
	p.mu.Unlock()
	return p.flushStreamCard(ctx, creds, binding, snap)
}

func (p *Patcher) streamTaskMessage(ctx context.Context, creds InstallationCredentials, binding db.LarkChatSessionBinding, taskID, chatSessionID pgtype.UUID, payload any) error {
	msg, ok := taskMessageFromPayload(payload)
	if !ok {
		return nil
	}
	if p.taskAlreadyTerminal(ctx, taskID) {
		return nil
	}
	changed := false
	p.mu.Lock()
	st := p.streamStateLocked(taskID, chatSessionID)
	if msg.Seq > 0 && msg.Seq <= st.lastSeq {
		p.mu.Unlock()
		return nil
	}
	if msg.Seq > 0 {
		st.lastSeq = msg.Seq
	}
	if !st.startedNoted {
		addRuntimeNoteLocked(st, "Agent 已开始处理请求。")
		changed = true
	}
	switch msg.Type {
	case "thinking":
		if msg.Content != "" {
			st.thinking = appendStreamText(st.thinking, msg.Content)
			st.thinking = trimRunesFromEnd(st.thinking, larkThinkingLimit)
			changed = true
		}
	case "tool_use":
		if line := toolUseSummary(msg.Tool, msg.Input); line != "" {
			st.thinking = appendStreamText(st.thinking, line)
			st.thinking = trimRunesFromEnd(st.thinking, larkThinkingLimit)
			changed = true
		}
	case "text":
		if msg.Content != "" {
			if !st.answerNoted {
				addRuntimeNoteLocked(st, "正在生成回复。")
				st.answerNoted = true
			}
			st.answer += msg.Content
			st.answer = trimRunes(st.answer, larkAnswerLimit)
			changed = true
		}
	case "error":
		if msg.Content != "" {
			st.thinking = appendStreamText(st.thinking, msg.Content)
			st.thinking = trimRunesFromEnd(st.thinking, larkThinkingLimit)
			changed = true
		}
	case "tool_result":
		// Tool output can be large or noisy; the Lark thinking panel only
		// shows the instruction that was run.
	default:
	}
	if !changed {
		p.mu.Unlock()
		return nil
	}
	now := p.cfg.Now()
	shouldPatch := st.larkCardMessageID == "" || st.lastPatchAt.IsZero() || now.Sub(st.lastPatchAt) >= larkStreamPatchInterval
	snap := snapshotFromState(st, true, true, "", CardStatusStreaming)
	p.mu.Unlock()
	if !shouldPatch {
		return nil
	}
	return p.flushStreamCard(ctx, creds, binding, snap)
}

func (p *Patcher) installationCredentials(inst db.LarkInstallation) (InstallationCredentials, error) {
	if p.credentials == nil {
		return InstallationCredentials{}, errors.New("lark patcher: credentials resolver missing")
	}
	secret, err := p.credentials.DecryptAppSecret(inst)
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

func (p *Patcher) finishChatReply(ctx context.Context, creds InstallationCredentials, binding db.LarkChatSessionBinding, taskID, chatSessionID pgtype.UUID, payload any) error {
	content := chatDoneContent(payload)
	p.mu.Lock()
	st, exists := p.streams[uuidString(taskID)]
	if !exists && content == "" {
		p.mu.Unlock()
		return nil
	}
	if !exists {
		st = p.streamStateLocked(taskID, chatSessionID)
	}
	if content != "" {
		st.answer = trimRunes(content, larkAnswerLimit)
	}
	snap := snapshotFromState(st, false, false, "", CardStatusFinal)
	p.mu.Unlock()
	if err := p.flushStreamCard(ctx, creds, binding, snap); err != nil {
		return err
	}
	p.removeStreamState(taskID)
	return nil
}

func (p *Patcher) flushStreamCard(ctx context.Context, creds InstallationCredentials, binding db.LarkChatSessionBinding, snap larkStreamSnapshot) error {
	p.flushMu.Lock()
	defer p.flushMu.Unlock()

	kind := CardKindRunning
	switch snap.status {
	case CardStatusFinal:
		kind = CardKindFinal
	case CardStatusError:
		kind = CardKindError
	}
	render, err := p.cfg.Renderer.Render(RenderInput{
		Kind:         kind,
		TaskID:       snap.taskID,
		Thinking:     snap.thinking,
		Answer:       snap.answer,
		Expanded:     snap.expanded,
		Running:      snap.running,
		ErrorMessage: snap.errorMessage,
	})
	if err != nil {
		return fmt.Errorf("render lark stream card: %w", err)
	}

	cardID, messageID, created, err := p.ensureStreamCard(ctx, creds, binding, snap, render.JSON)
	if err != nil {
		return err
	}
	if created {
		p.markStreamFlushed(snap.taskID, cardID, messageID, snap.status)
		return nil
	}
	p.clearTypingOnce(ctx, snap.taskID, snap.chatSessionID)
	if err := p.client.PatchInteractiveCard(ctx, PatchCardParams{
		InstallationID:    creds,
		LarkCardMessageID: messageID,
		CardJSON:          render.JSON,
	}); err != nil {
		return fmt.Errorf("patch lark stream card: %w", err)
	}
	if cardID.Valid {
		if err := p.queries.UpdateLarkOutboundCardStatus(ctx, db.UpdateLarkOutboundCardStatusParams{
			ID:     cardID,
			Status: string(snap.status),
		}); err != nil {
			return fmt.Errorf("update lark outbound card status: %w", err)
		}
	}
	p.markStreamFlushed(snap.taskID, cardID, messageID, snap.status)
	return nil
}

func (p *Patcher) taskAlreadyTerminal(ctx context.Context, taskID pgtype.UUID) bool {
	key := uuidString(taskID)
	p.mu.Lock()
	st := p.streams[key]
	if st != nil {
		terminal := st.status == CardStatusFinal || st.status == CardStatusError
		p.mu.Unlock()
		return terminal
	}
	p.mu.Unlock()

	existing, err := p.queries.GetLarkOutboundCardByTask(ctx, taskID)
	if err != nil {
		return false
	}
	return existing.Status == string(CardStatusFinal) || existing.Status == string(CardStatusError)
}

func (p *Patcher) ensureStreamCard(ctx context.Context, creds InstallationCredentials, binding db.LarkChatSessionBinding, snap larkStreamSnapshot, cardJSON string) (pgtype.UUID, string, bool, error) {
	if snap.larkCardMessageID != "" {
		return snap.cardID, snap.larkCardMessageID, false, nil
	}
	if existing, err := p.queries.GetLarkOutboundCardByTask(ctx, snap.taskID); err == nil {
		p.rememberStreamCard(snap.taskID, existing.ID, existing.LarkCardMessageID, CardStatus(existing.Status))
		return existing.ID, existing.LarkCardMessageID, false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, "", false, fmt.Errorf("lookup lark outbound card: %w", err)
	}

	p.clearTypingOnce(ctx, snap.taskID, snap.chatSessionID)
	messageID, err := p.client.SendInteractiveCard(ctx, SendCardParams{
		InstallationID: creds,
		ChatID:         ChatID(binding.LarkChatID),
		CardJSON:       cardJSON,
	})
	if err != nil {
		return pgtype.UUID{}, "", false, fmt.Errorf("send lark stream card: %w", err)
	}
	row, err := p.queries.CreateLarkOutboundCardMessage(ctx, db.CreateLarkOutboundCardMessageParams{
		ChatSessionID:     snap.chatSessionID,
		TaskID:            snap.taskID,
		LarkChatID:        binding.LarkChatID,
		LarkCardMessageID: messageID,
		Status:            string(snap.status),
	})
	if err != nil {
		return pgtype.UUID{}, "", false, fmt.Errorf("create lark outbound card row: %w", err)
	}
	return row.ID, row.LarkCardMessageID, true, nil
}

func (p *Patcher) streamStateLocked(taskID, chatSessionID pgtype.UUID) *larkStreamState {
	key := uuidString(taskID)
	if st, ok := p.streams[key]; ok {
		if chatSessionID.Valid {
			st.chatSessionID = chatSessionID
		}
		return st
	}
	st := &larkStreamState{
		taskID:        taskID,
		chatSessionID: chatSessionID,
		status:        CardStatusPending,
	}
	p.streams[key] = st
	return st
}

func snapshotFromState(st *larkStreamState, expanded, running bool, errorMessage string, status CardStatus) larkStreamSnapshot {
	return larkStreamSnapshot{
		taskID:            st.taskID,
		chatSessionID:     st.chatSessionID,
		cardID:            st.cardID,
		larkCardMessageID: st.larkCardMessageID,
		thinking:          st.thinking,
		answer:            st.answer,
		expanded:          expanded,
		running:           running,
		errorMessage:      errorMessage,
		status:            status,
	}
}

func (p *Patcher) rememberStreamCard(taskID, cardID pgtype.UUID, messageID string, status CardStatus) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.streamStateLocked(taskID, pgtype.UUID{})
	st.cardID = cardID
	st.larkCardMessageID = messageID
	st.status = status
}

func (p *Patcher) markStreamFlushed(taskID, cardID pgtype.UUID, messageID string, status CardStatus) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.streamStateLocked(taskID, pgtype.UUID{})
	st.cardID = cardID
	st.larkCardMessageID = messageID
	st.lastPatchAt = p.cfg.Now()
	st.status = status
}

func (p *Patcher) removeStreamState(taskID pgtype.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.streams, uuidString(taskID))
}

func (p *Patcher) clearTypingOnce(ctx context.Context, taskID, chatSessionID pgtype.UUID) {
	p.mu.Lock()
	st := p.streamStateLocked(taskID, chatSessionID)
	if st.typingCleared {
		p.mu.Unlock()
		return
	}
	st.typingCleared = true
	p.mu.Unlock()
	if p.typingIndicator != nil {
		p.typingIndicator.Clear(ctx, chatSessionID)
	}
}

func taskMessageFromPayload(payload any) (protocol.TaskMessagePayload, bool) {
	switch p := payload.(type) {
	case protocol.TaskMessagePayload:
		return p, true
	case map[string]any:
		msg := protocol.TaskMessagePayload{}
		if s, _ := p["task_id"].(string); s != "" {
			msg.TaskID = s
		}
		if s, _ := p["issue_id"].(string); s != "" {
			msg.IssueID = s
		}
		msg.Seq = intFromAny(p["seq"])
		if s, _ := p["type"].(string); s != "" {
			msg.Type = s
		}
		if s, _ := p["tool"].(string); s != "" {
			msg.Tool = s
		}
		if s, _ := p["content"].(string); s != "" {
			msg.Content = s
		}
		if input, ok := p["input"].(map[string]any); ok {
			msg.Input = input
		}
		if s, _ := p["output"].(string); s != "" {
			msg.Output = s
		}
		return msg, msg.Type != ""
	default:
		return protocol.TaskMessagePayload{}, false
	}
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

func toolUseSummary(tool string, input map[string]any) string {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		tool = "tool"
	}
	if command := commandFromInput(input); command != "" {
		return "Shell command: " + inlineCode(singleLine(command))
	}
	if len(input) == 0 {
		return "Tool use: " + inlineCode(tool)
	}
	return "Tool use: " + inlineCode(tool) + " " + compactToolInput(input)
}

func commandFromInput(input map[string]any) string {
	for _, key := range []string{"command", "cmd", "script", "shell_command"} {
		if v, ok := input[key]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func compactToolInput(input map[string]any) string {
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 5 {
		keys = keys[:5]
	}
	compact := make(map[string]any, len(keys))
	for _, k := range keys {
		compact[k] = input[k]
	}
	raw, err := json.Marshal(compact)
	if err != nil {
		return ""
	}
	return trimRunes(string(raw), larkToolInputLimit)
}

func appendStreamText(existing, chunk string) string {
	chunk = strings.TrimSpace(chunk)
	if chunk == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return chunk
	}
	return strings.TrimRight(existing, "\n") + "\n\n" + chunk
}

func addRuntimeNoteLocked(st *larkStreamState, note string) {
	if strings.TrimSpace(note) == "" {
		return
	}
	if note == "Agent 已开始处理请求。" {
		st.startedNoted = true
	}
	st.thinking = appendStreamText(st.thinking, note)
	st.thinking = trimRunesFromEnd(st.thinking, larkThinkingLimit)
}

func greyMarkdown(content string) string {
	return "<font color=\"grey\">" + content + "</font>"
}

func inlineCode(s string) string {
	s = strings.ReplaceAll(s, "`", "'")
	if s == "" {
		return "``"
	}
	return "`" + s + "`"
}

func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func trimRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func trimRunesFromEnd(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 2 {
		return string(r[len(r)-max:])
	}
	return "…\n" + string(r[len(r)-max+2:])
}

func (p *Patcher) fail(ctx context.Context, creds InstallationCredentials, binding db.LarkChatSessionBinding, taskID, chatSessionID pgtype.UUID, payload any) error {
	p.mu.Lock()
	st := p.streamStateLocked(taskID, chatSessionID)
	snap := snapshotFromState(st, true, false, errorMessageFromPayload(payload), CardStatusError)
	p.mu.Unlock()
	if err := p.flushStreamCard(ctx, creds, binding, snap); err != nil {
		return err
	}
	p.removeStreamState(taskID)
	return nil
}

// taskAndSessionFromEvent parses the typed-ish payload broadcastTaskEvent
// publishes — a map[string]any with `task_id` (always) and
// `chat_session_id` (chat tasks only). EventChatDone carries a
// ChatDonePayload struct instead.
func taskAndSessionFromEvent(e events.Event) (taskID, chatSessionID pgtype.UUID, ok bool) {
	if e.TaskID != "" {
		if err := taskID.Scan(e.TaskID); err != nil {
			taskID = pgtype.UUID{}
		}
	}
	if e.ChatSessionID != "" {
		if err := chatSessionID.Scan(e.ChatSessionID); err != nil {
			chatSessionID = pgtype.UUID{}
		}
	}
	switch p := e.Payload.(type) {
	case map[string]any:
		if !taskID.Valid {
			if s, _ := p["task_id"].(string); s != "" {
				_ = taskID.Scan(s)
			}
		}
		if !chatSessionID.Valid {
			if s, _ := p["chat_session_id"].(string); s != "" {
				_ = chatSessionID.Scan(s)
			}
		}
	case protocol.ChatDonePayload:
		if !taskID.Valid {
			_ = taskID.Scan(p.TaskID)
		}
		if !chatSessionID.Valid {
			_ = chatSessionID.Scan(p.ChatSessionID)
		}
	}
	return taskID, chatSessionID, taskID.Valid
}

func chatDoneContent(payload any) string {
	switch p := payload.(type) {
	case protocol.ChatDonePayload:
		return p.Content
	case map[string]any:
		if s, ok := p["content"].(string); ok {
			return s
		}
	}
	return ""
}

func errorMessageFromPayload(payload any) string {
	if m, ok := payload.(map[string]any); ok {
		if s, ok := m["error"].(string); ok {
			return s
		}
		if s, ok := m["error_message"].(string); ok {
			return s
		}
		if s, ok := m["failure_reason"].(string); ok {
			return s
		}
	}
	return ""
}
