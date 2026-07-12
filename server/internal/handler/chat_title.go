package handler

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// chatTitleGenTimeout bounds the whole best-effort title generation (LLM call
// + CAS write). It runs on a detached background context — decoupled from the
// originating HTTP request, which returns immediately — so this is the only
// thing keeping the goroutine from lingering if the upstream hangs. Kept short:
// a chat title is a nicety, not worth pinning a goroutine for a minute.
const chatTitleGenTimeout = 20 * time.Second

// chatTitleSystemPrompt instructs the model to condense the opening of a
// conversation into a short, language-matched title. The rules mirror the
// acceptance criteria in MUL-4295: no quotes, no trailing punctuation, no
// "标题：" / "Title:" prefix, follow the conversation's language. sanitizeChatTitle
// re-applies these rules defensively in case the model ignores them.
const chatTitleSystemPrompt = `You write a very short title that summarizes the topic of a chat conversation, given the user's opening message.

Rules:
- Output ONLY the title text — nothing else, no explanation.
- Keep it short: a few words, ideally under 8, never a full sentence.
- Write the title in the SAME language as the user's message (Chinese input → Chinese title, English input → English title).
- Do NOT wrap the title in quotes or brackets.
- Do NOT prefix it with "Title:", "标题：", or similar.
- Do NOT end with a period or any trailing punctuation.`

// maybeGenerateChatTitleAsync kicks off best-effort LLM title generation for a
// chat session and returns immediately. It is the entry point wired into the
// first-user-message path of SendChatMessage.
//
// Design constraints from MUL-4295:
//   - Non-blocking: never delays the user's send / first response. The work
//     runs in a detached goroutine on context.Background() (the request context
//     is cancelled the moment SendChatMessage returns).
//   - Silent fallback: when the LLM layer is not configured (self-hosted with
//     no key) or the call fails, we do nothing and leave the original
//     first-message-derived title untouched — no error surfaces to the user.
//   - No clobber: the update is a compare-and-swap against currentTitle, so a
//     manual rename that lands during generation is never overwritten.
//
// currentTitle is the session's title as observed at trigger time (the
// default/original title). sourceText is the user's first message, which the
// model condenses into a title.
func (h *Handler) maybeGenerateChatTitleAsync(workspaceID, userID string, sessionID pgtype.UUID, currentTitle, sourceText string) {
	// Short-circuit before spawning a goroutine when the LLM layer is disabled
	// (self-hosted without MULTICA_LLM_API_KEY / MULTICA_LLM_BASE_URL): the
	// original title is kept as-is, exactly matching pre-feature behavior.
	if h.LLM == nil || !h.LLM.Enabled() {
		return
	}
	if strings.TrimSpace(sourceText) == "" {
		return
	}

	go func() {
		// Panic containment: this goroutine is detached from the HTTP request,
		// so chi's Recoverer middleware is NOT in the call stack. A panic
		// anywhere below (GenerateText, sanitizing, the DB write, publish)
		// would otherwise crash the whole server process. This is a
		// best-effort nicety — swallow the panic, log it, and leave the
		// original title in place.
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("chat title generation panicked; keeping original title",
					"session_id", uuidToString(sessionID),
					"panic", rec,
				)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), chatTitleGenTimeout)
		defer cancel()

		updated, applied, err := h.generateChatSessionTitle(ctx, sessionID, currentTitle, sourceText)
		if err != nil {
			// Timeout, upstream 4xx/5xx, empty choices, etc. Log-and-forget:
			// the send already succeeded and the original title stands.
			slog.Warn("chat title generation failed; keeping original title",
				"session_id", uuidToString(sessionID),
				"error", err,
			)
			return
		}
		if !applied {
			// Either the model produced nothing usable, or the title changed
			// under us (manual rename / already auto-titled). Nothing to push.
			return
		}

		// Reuse the existing chat:session_updated realtime channel so the
		// frontend refreshes the title in place, identical to a manual rename.
		resolvedSessionID := uuidToString(updated.ID)
		h.publishChat(protocol.EventChatSessionUpdated, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionUpdatedPayload{
			ChatSessionID: resolvedSessionID,
			Title:         updated.Title,
			UpdatedAt:     timestampToString(updated.UpdatedAt),
		})
	}()
}

// generateChatSessionTitle performs the synchronous core of title generation:
// call the LLM, sanitize the output, and compare-and-swap it onto the session.
// It is separated from the goroutine wrapper so it can be unit-tested directly.
//
// Return contract:
//   - (session, true, nil):  a new title was generated and written.
//   - (zero, false, nil):    generation produced nothing usable, OR the title
//     had changed since currentTitle was observed (CAS miss / manual rename) —
//     both are non-error "leave it alone" outcomes.
//   - (zero, false, err):    the LLM layer is disabled or the call failed, OR
//     the CAS write hit a real DB error. Callers treat this as best-effort and
//     keep the original title.
func (h *Handler) generateChatSessionTitle(ctx context.Context, sessionID pgtype.UUID, currentTitle, sourceText string) (db.ChatSession, bool, error) {
	// DefaultModel() is used implicitly by GenerateText when model == "": a
	// deployment configures MULTICA_LLM_DEFAULT_MODEL (or the built-in
	// gpt-4o-mini fallback) — no model is threaded through from the frontend.
	raw, err := h.LLM.GenerateText(ctx, "", chatTitleSystemPrompt, sourceText)
	if err != nil {
		return db.ChatSession{}, false, err
	}

	title := sanitizeChatTitle(raw)
	if title == "" {
		// Model returned only quotes/punctuation/whitespace — treat as "no
		// usable title" and fall back silently to the original.
		return db.ChatSession{}, false, nil
	}

	updated, err := h.Queries.UpdateChatSessionTitleIfCurrent(ctx, db.UpdateChatSessionTitleIfCurrentParams{
		ID:            sessionID,
		ExpectedTitle: currentTitle,
		NewTitle:      title,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Title changed since we observed currentTitle — a manual rename or
			// a competing writer won. Do not clobber it.
			return db.ChatSession{}, false, nil
		}
		return db.ChatSession{}, false, err
	}
	return updated, true, nil
}

// chatTitleLabelPrefixes are leading labels a model sometimes prepends despite
// the prompt. Matched case-insensitively and only at the very start.
var chatTitleLabelPrefixes = []string{
	"title:", "title：",
	"标题:", "标题：",
	"题目:", "题目：",
	"主题:", "主题：",
}

// sanitizeChatTitle defensively enforces the title formatting rules regardless
// of how well the model followed the prompt: collapse whitespace, strip a
// leading "Title:" / "标题：" style label, remove surrounding quote/bracket
// pairs, drop trailing sentence punctuation, and hard-cap the length at
// chatSessionTitleMaxLen runes (the same ceiling the manual rename endpoint
// enforces). Returns "" when nothing meaningful remains.
//
// Prefix-stripping, wrapper-stripping, AND trailing-punctuation trimming all
// run in a single loop until the string stops changing. They interact: a model
// may nest them (e.g. `"Title: Fix login".` or `「标题：修复登录问题」。`), where a
// trailing `.` / `。` keeps the closing wrapper from being recognized, which in
// turn hides the forbidden prefix. Iterating all three to a fixed point peels
// them in any order.
func sanitizeChatTitle(raw string) string {
	// Collapse all internal whitespace (including newlines/tabs the model may
	// emit) into single spaces so a multi-line reply becomes one clean line.
	s := strings.TrimSpace(strings.Join(strings.Fields(raw), " "))
	if s == "" {
		return ""
	}

	// Alternate stripping the leading label prefix, one layer of surrounding
	// quotes/brackets, and trailing sentence punctuation until none of them
	// changes anything. Bounded by the string only ever getting shorter, so it
	// always terminates.
	for {
		before := s
		s = strings.TrimSpace(stripChatTitleLabelPrefix(s))
		s = stripSurroundingQuotes(s)
		// ASCII + common CJK/full-width sentence punctuation. Trailing trim is
		// inside the loop so removing a trailing "." / "。" re-exposes a closing
		// wrapper (and the prefix it hides) for the next pass.
		s = strings.TrimSpace(strings.TrimRight(s, ".。!！?？,，;；:：、 "))
		if s == before || s == "" {
			break
		}
	}
	if s == "" {
		return ""
	}

	// Hard cap on rune length to match the manual-rename ceiling.
	if runes := []rune(s); len(runes) > chatSessionTitleMaxLen {
		s = strings.TrimSpace(string(runes[:chatSessionTitleMaxLen]))
	}
	return s
}

// stripChatTitleLabelPrefix removes one leading label prefix ("Title:",
// "标题：", ...) when present, matched case-insensitively. Returns s unchanged
// when none matches.
func stripChatTitleLabelPrefix(s string) string {
	lower := strings.ToLower(s)
	for _, p := range chatTitleLabelPrefixes {
		if strings.HasPrefix(lower, p) {
			return s[len(p):]
		}
	}
	return s
}

// chatTitleQuotePairs maps an opening quote/bracket to its closing partner.
var chatTitleQuotePairs = map[rune]rune{
	'"':  '"',
	'\'': '\'',
	'`':  '`',
	'“':  '”',
	'‘':  '’',
	'「':  '」',
	'『':  '』',
	'《':  '》',
	'（':  '）',
	'(':  ')',
	'【':  '】',
	'[':  ']',
}

// stripSurroundingQuotes removes matching opening/closing quote or bracket
// pairs that wrap the whole string, peeling repeatedly for nested wrappers.
func stripSurroundingQuotes(s string) string {
	for {
		runes := []rune(s)
		if len(runes) < 2 {
			return s
		}
		closer, ok := chatTitleQuotePairs[runes[0]]
		if !ok || runes[len(runes)-1] != closer {
			return s
		}
		s = strings.TrimSpace(string(runes[1 : len(runes)-1]))
		if s == "" {
			return s
		}
	}
}
