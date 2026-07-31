package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Budgets for one suggestion pass. The whole call is a nicety attached to a
// reply the user already has, so it is bounded tightly: a slow pass is worse
// than no pass, because the client holds a skeleton placeholder until it
// resolves (QUICK_ACTIONS_PENDING_TIMEOUT_MS on the frontend is sized from
// chatQuickActionsTimeout plus room for the write and the broadcast).
const (
	chatQuickActionsTimeout     = 8 * time.Second
	chatQuickActionsTemperature = 0.3
	// Output cap. GenerateJSON disables GPT-5.6 reasoning for this
	// latency-sensitive utility call, so the whole budget is available for the
	// JSON response. The headroom keeps verbose or multibyte actions from being
	// cut off mid-object; sanitizeChatQuickActions still applies the product's
	// visible size limits.
	chatQuickActionsMaxCompletionTokens = 2048
)

// Context window for the pass. Suggestions are about where the conversation
// goes next, so only the tail matters — older turns cost tokens and latency
// without changing the answer.
const (
	chatQuickActionsContextMessages = 6
	// The latest assistant reply gets the largest share: it is what the
	// suggestions must be anchored in.
	chatQuickActionsLatestBudget = 3000
	chatQuickActionsOlderBudget  = 800
	// Head/tail split applied to an over-long latest reply. The tail carries
	// the conclusion and the proposed next steps — exactly the material
	// suggestions are built from — so keeping only the head would strip the
	// most useful part.
	chatQuickActionsHeadBudget = 2000
	chatQuickActionsTailBudget = 1000
	// Cap on how many previously-suggested labels are replayed to the model.
	chatQuickActionsPreviousMax = 6
)

// chatQuickActionsMaxConcurrent bounds how many suggestion passes may be in
// flight process-wide. Generation is spawned from request handling with no
// queue behind it, so without a ceiling a burst of chat traffic (or a client
// looping the refresh endpoint) scales goroutines, upstream connections, and
// model spend without limit. Passes over the ceiling are dropped, not queued:
// a suggestion that arrives after the client's pending window has expired is
// worth nothing, so shedding beats backlogging.
const chatQuickActionsMaxConcurrent = 16

// ChatQuickActionsOrigin says which caller asked for a pass. It decides how a
// failure is reported: an explicit refresh is a user action that must surface
// its own failure, while the automatic pass is best-effort background work the
// user never asked for and must fail silently (protocol.ChatQuickActionsPayload
// documents Failed as false for the automatic pass, and the client turns every
// failed=true into a "couldn't refresh" toast).
type ChatQuickActionsOrigin int

const (
	ChatQuickActionsAutomatic ChatQuickActionsOrigin = iota
	ChatQuickActionsRefresh
)

// ChatQuickActionsLLM is the seam TaskService uses to generate follow-up
// suggestions, satisfied by *llm.Client. It is an interface (not the concrete
// client) so tests can drive the whole path — prompt rendering, parsing,
// persistence, broadcast — without an HTTP upstream, mirroring how
// ComposioOverlayBuilder and TaskWakeupNotifier are injected.
//
// A nil ChatQuickActionsLLM, or one whose Enabled() is false, disables the
// feature entirely: no pending marker is raised and no pills are generated.
// That is the expected state for a self-hosted deployment with no
// MULTICA_LLM_API_KEY / MULTICA_LLM_BASE_URL, and matches how chat auto-titling
// already degrades.
type ChatQuickActionsLLM interface {
	Enabled() bool
	GenerateJSON(ctx context.Context, model, systemPrompt, userPrompt string, temperature float64, maxCompletionTokens int64) (string, error)
}

// chatQuickActionsSystemPrompt is the entire instruction set for the pass. It
// is a system prompt (stable across calls, so upstream prompt caching applies);
// the per-call conversation goes in the user message.
//
// The rules are deliberately prescriptive about WHO the suggestions are for.
// The retired daemon pass ran as a resumed turn inside the agent's own session,
// so it inherited the Multica runtime brief's identity and drifted toward
// agent-operations actions; this pass has no such context and must be told the
// frame explicitly.
//
// The word "JSON" must stay in this text: response_format=json_object is
// rejected upstream without it.
const chatQuickActionsSystemPrompt = `You generate follow-up suggestions for a chat between a user and an AI agent.
Your output is rendered as three clickable buttons under the agent's latest
reply. Clicking one sends that suggestion's "prompt" to the same agent as the
user's next message.

You write FOR THE USER, not for the agent. Every suggestion must be something
the user would plausibly want to ask or do next — never a task the agent should
perform on its own, never a status report, never a question addressed to the user.

Quality bar:
- Return exactly 3 suggestions.
- Every suggestion must be anchored in something concrete the latest agent reply
  actually mentioned — a file, a name, an option it listed, a caveat it raised,
  a next step it proposed. Never invent a topic the conversation did not touch.
- Never suggest something the agent already did in this turn.
- Never repeat or paraphrase anything under ALREADY SUGGESTED.
- Make the three distinct from each other: different intents, not three
  rewordings of the same request.

Field rules:
- "label": the button text. A short verb phrase — at most 6 words in English, at
  most 12 characters in Chinese. No trailing punctuation, no quotes, no emoji.
  It is a button, not a sentence.
- "prompt": the full message sent on the user's behalf. First person, the user's
  own voice, and SELF-CONTAINED — the agent never sees the label, so the prompt
  must carry every detail itself. One or two sentences.
- "primary": true on exactly one suggestion, the single most likely next step.
  false on all others.

Write both fields in the same language the USER has been writing in, regardless
of what language the agent replied in.

Output JSON only, exactly this shape:
{"actions":[{"label":"...","prompt":"...","primary":true}]}
No prose, no markdown, no code fences.`

// GenerateChatQuickActionsForTask runs one suggestion pass for a completed chat
// turn and attaches the result to that turn's assistant row, broadcasting
// chat:quick_actions. It is the synchronous core shared by the automatic
// post-completion pass and the explicit refresh, and is safe to call directly
// from tests.
//
// origin decides failure reporting: a refresh surfaces failed=true so the user
// who pressed the button learns it did not work, while the automatic pass
// resolves the placeholder quietly. Reporting an automatic failure would pop a
// "couldn't refresh" toast for an action the user never took.
func (s *TaskService) GenerateChatQuickActionsForTask(ctx context.Context, task db.AgentTaskQueue, origin ChatQuickActionsOrigin) error {
	if s.QuickActions == nil || !s.QuickActions.Enabled() {
		return nil
	}
	if !task.ChatSessionID.Valid {
		return nil
	}

	// Resolve the target turn FIRST and anchor everything to it. The context
	// window, the write, and the broadcast must all describe the same assistant
	// message: this runs on a detached goroutine, so by the time it executes the
	// session may already have moved on by a full turn.
	target, err := s.Queries.GetChatMessageByTaskAssistant(ctx, task.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No assistant row to attach to (no_response, channel empty-drop).
			// No client is waiting either — the pending flag is only raised for
			// a written ordinary message.
			return nil
		}
		return fmt.Errorf("load chat quick actions target: %w", err)
	}
	if target.MessageKind != protocol.ChatMessageKindMessage || strings.TrimSpace(target.Content) == "" {
		// Nothing to build suggestions on. Resolve the placeholder rather than
		// leaving the client's skeleton to time out.
		return s.SupplementChatQuickActions(ctx, task, "", false)
	}

	prompt, err := s.buildChatQuickActionsPrompt(ctx, target)
	if err != nil {
		return err
	}

	raw, err := s.QuickActions.GenerateJSON(ctx,
		"", // deployment default: MULTICA_LLM_DEFAULT_MODEL, else llm.FallbackModel
		chatQuickActionsSystemPrompt,
		prompt,
		chatQuickActionsTemperature,
		chatQuickActionsMaxCompletionTokens,
	)
	if err != nil {
		// Resolve the placeholder either way; only an explicit refresh reports
		// the failure to the user (see origin).
		if suppErr := s.SupplementChatQuickActions(ctx, task, "", origin == ChatQuickActionsRefresh); suppErr != nil {
			slog.Warn("chat quick actions failure broadcast failed",
				"task_id", util.UUIDToString(task.ID),
				"error", suppErr,
			)
		}
		return fmt.Errorf("generate chat quick actions: %w", err)
	}
	return s.SupplementChatQuickActions(ctx, task, raw, false)
}

// GenerateChatQuickActionsAsync runs GenerateChatQuickActionsForTask on a
// detached goroutine and returns immediately. Used on the completion path,
// where the user's reply is already delivered and must never wait on this, and
// on the refresh path, which answers 202 before the pass runs.
//
// Two admission gates before anything is spawned:
//
//   - One pass per chat session. Concurrent passes on one session would race to
//     write the same row and burn duplicate spend for a single visible outcome
//     — two refreshes of one turn, or a refresh overlapping the automatic pass.
//   - A process-wide ceiling (chatQuickActionsMaxConcurrent).
//
// A rejected pass still resolves the client's placeholder, so a skeleton never
// hangs waiting for work that was never started.
//
// The goroutine owns its own context: the caller's is typically an HTTP request
// context that is cancelled the moment the completion callback returns.
func (s *TaskService) GenerateChatQuickActionsAsync(task db.AgentTaskQueue, origin ChatQuickActionsOrigin) {
	if s.QuickActions == nil || !s.QuickActions.Enabled() {
		return
	}
	sessionKey := util.UUIDToString(task.ChatSessionID)
	if sessionKey == "" {
		return
	}
	if _, inFlight := s.quickActionsInFlight.LoadOrStore(sessionKey, struct{}{}); inFlight {
		slog.Info("chat quick actions pass skipped: one already running for this session",
			"chat_session_id", sessionKey, "task_id", util.UUIDToString(task.ID))
		s.resolveChatQuickActionsPlaceholder(task)
		return
	}
	if s.quickActionsRunning.Add(1) > chatQuickActionsMaxConcurrent {
		s.quickActionsRunning.Add(-1)
		s.quickActionsInFlight.Delete(sessionKey)
		slog.Warn("chat quick actions pass shed: process-wide concurrency ceiling reached",
			"ceiling", chatQuickActionsMaxConcurrent, "task_id", util.UUIDToString(task.ID))
		s.resolveChatQuickActionsPlaceholder(task)
		return
	}

	go func() {
		defer func() {
			s.quickActionsRunning.Add(-1)
			s.quickActionsInFlight.Delete(sessionKey)
		}()
		// Panic containment: this goroutine is detached from the HTTP request,
		// so chi's Recoverer middleware is not in the call stack and a panic
		// here would take down the whole server process. Suggestions are a
		// nicety — swallow, log, and leave the turn's pills as they are.
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("chat quick actions generation panicked",
					"task_id", util.UUIDToString(task.ID),
					"panic", rec,
				)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), chatQuickActionsTimeout)
		defer cancel()

		if err := s.GenerateChatQuickActionsForTask(ctx, task, origin); err != nil {
			slog.Warn("chat quick actions generation failed",
				"task_id", util.UUIDToString(task.ID),
				"chat_session_id", sessionKey,
				"error", err,
			)
		}
	}()
}

// chatQuickActionsInFlight reports whether a pass is already running for this
// session. The refresh endpoint checks it so a duplicate request is refused
// with a 409 the client can act on, instead of being accepted and silently
// dropped by the admission gate in GenerateChatQuickActionsAsync.
func (s *TaskService) chatQuickActionsInFlight(sessionID pgtype.UUID) bool {
	_, running := s.quickActionsInFlight.Load(util.UUIDToString(sessionID))
	return running
}

// resolveChatQuickActionsPlaceholder ends a client's pending skeleton without
// generating anything, rebroadcasting the turn's current pills. Used when a
// pass is refused admission — the placeholder was already raised by chat:done,
// and leaving it to expire would show a spinner for work that never started.
func (s *TaskService) resolveChatQuickActionsPlaceholder(task db.AgentTaskQueue) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.SupplementChatQuickActions(ctx, task, "", false); err != nil {
		slog.Warn("chat quick actions placeholder resolve failed",
			"task_id", util.UUIDToString(task.ID), "error", err)
	}
}

// buildChatQuickActionsPrompt renders the pass's user message from the window
// of conversation ENDING AT target, which is the assistant turn the suggestions
// will be attached to.
//
// Anchoring on the target (rather than re-reading the session's newest
// messages) is what keeps an async pass correct: a turn that lands between the
// completion callback and this read would otherwise supply the context while
// the result is still written to the older turn, and a newly-sent user message
// would leave the window ending on a user row with no reply to build on.
func (s *TaskService) buildChatQuickActionsPrompt(ctx context.Context, target db.ChatMessage) (string, error) {
	// Strictly older than target, newest-first; reversed below. Over-fetch so
	// dropped rows (no_response, failures) don't shrink the window below the
	// intended turn count.
	rows, err := s.Queries.ListChatMessagesPage(ctx, db.ListChatMessagesPageParams{
		ChatSessionID:   target.ChatSessionID,
		Limit:           chatQuickActionsContextMessages * 2,
		BeforeCreatedAt: target.CreatedAt,
		BeforeID:        target.ID,
	})
	if err != nil {
		return "", fmt.Errorf("load chat messages for quick actions: %w", err)
	}

	msgs := make([]db.ChatMessage, 0, len(rows)+1)
	for i := len(rows) - 1; i >= 0; i-- {
		msg := rows[i]
		// Only ordinary turns carry usable text. A no_response row holds an
		// English placeholder body and a failure row holds an error, neither of
		// which describes what the conversation is about.
		if msg.MessageKind != protocol.ChatMessageKindMessage {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		msgs = append(msgs, msg)
	}
	if len(msgs) > chatQuickActionsContextMessages-1 {
		msgs = msgs[len(msgs)-(chatQuickActionsContextMessages-1):]
	}
	msgs = append(msgs, target)
	return renderChatQuickActionsContext(msgs, collectPreviousChatQuickActions(msgs)), nil
}

// collectPreviousChatQuickActions gathers the labels already offered in this
// window so the prompt can forbid repeating them. The newest assistant row is
// included: on an explicit refresh it holds the pills the user is asking to
// replace, and offering the same three back is the one outcome a refresh must
// never produce.
func collectPreviousChatQuickActions(msgs []db.ChatMessage) []string {
	labels := make([]string, 0, chatQuickActionsPreviousMax)
	seen := make(map[string]struct{}, chatQuickActionsPreviousMax)
	// Newest first, so the most recent suggestions survive the cap.
	for i := len(msgs) - 1; i >= 0; i-- {
		if len(msgs[i].QuickActions) == 0 {
			continue
		}
		var actions []protocol.ChatQuickAction
		if err := json.Unmarshal(msgs[i].QuickActions, &actions); err != nil {
			continue
		}
		for _, action := range actions {
			label := strings.TrimSpace(action.Label)
			if label == "" {
				continue
			}
			key := strings.ToLower(label)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			labels = append(labels, label)
			if len(labels) == chatQuickActionsPreviousMax {
				return labels
			}
		}
	}
	return labels
}

// renderChatQuickActionsContext formats the conversation window and the
// already-suggested labels into the pass's user message. Pure, so the
// truncation rules are unit-testable without a database.
//
// msgs is oldest-first and its last entry must be the assistant reply the
// suggestions are for.
func renderChatQuickActionsContext(msgs []db.ChatMessage, previous []string) string {
	var b strings.Builder
	b.WriteString("CONVERSATION (oldest first):\n")
	for i, msg := range msgs {
		speaker := "agent"
		if msg.Role == "user" {
			speaker = "user"
		}
		content := strings.TrimSpace(msg.Content)
		if i == len(msgs)-1 {
			content = truncateChatQuickActionsLatest(content)
		} else {
			content = truncateChatQuickActionsRunes(content, chatQuickActionsOlderBudget)
		}
		fmt.Fprintf(&b, "[%s]: %s\n", speaker, content)
	}

	b.WriteString("\nALREADY SUGGESTED (do not repeat or paraphrase):\n")
	if len(previous) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, label := range previous {
			fmt.Fprintf(&b, "- %s\n", label)
		}
	}

	b.WriteString("\nProduce the follow-up suggestions for the latest agent reply.")
	return b.String()
}

// truncateChatQuickActionsLatest shortens the anchor reply while keeping both
// ends. The head establishes what the reply is about and the tail holds its
// conclusion and proposed next steps; a plain head-only cut on a long reply
// discards exactly the material the suggestions should be built from.
func truncateChatQuickActionsLatest(content string) string {
	runes := []rune(content)
	if len(runes) <= chatQuickActionsLatestBudget {
		return content
	}
	head := string(runes[:chatQuickActionsHeadBudget])
	tail := string(runes[len(runes)-chatQuickActionsTailBudget:])
	return head + "\n…[truncated]…\n" + tail
}

// truncateChatQuickActionsRunes cuts an older message to a rune budget. Head
// only: for context turns, what the message opened with is enough to follow the
// thread.
func truncateChatQuickActionsRunes(content string, maxRunes int) string {
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return string(runes[:maxRunes]) + "…"
}
