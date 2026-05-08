// CEREBRO: short task-title generator. Owned by cerebro, not upstream,
// so future upstream syncs leave the LLM dependency alone.
//
// The package answers a single question: what's a 5-10 word headline for
// this agent task, given the comment that triggered it? The title is
// shown inline ("Lando is working: Summary of CAC week 18") in the
// channel comment stream, the inbox, AgentLiveCard, and the Tasks list.
//
// Two paths, picked at startup based on env:
//   - If ANTHROPIC_API_KEY is set, Build calls Anthropic Messages API
//     with a low-cost Haiku model and a 2-second timeout.
//   - Otherwise (or on any error / timeout) Build returns a heuristic
//     extracted from the comment content (first sentence / first 60
//     runes), with the fallback title used when the comment is empty.
//
// Either way the call is best-effort — Build always returns SOMETHING
// usable so the caller never has to deal with a nil/empty title.
package agent_title

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// titleMaxRunes is the hard cap we enforce on whatever path produces the
// title. ~80 chars covers a 10-word English title plus a couple of
// punctuation marks, with headroom for Danish words like "udvikling".
const titleMaxRunes = 80

// llmTimeout caps the LLM call so a slow/hung Anthropic upstream can't
// stall the comment-post path. The fallback heuristic kicks in on
// timeout — users always see SOME label.
const llmTimeout = 2 * time.Second

// llmModel is the cheapest Anthropic model that gives a coherent 10-word
// headline. Title generation is high-volume and latency-sensitive, so
// this is the right tradeoff vs Sonnet / Opus.
const llmModel = "claude-haiku-4-5"

// Builder produces titles. A nil Builder is valid and falls back to the
// heuristic path — call sites can always construct one and call Build.
type Builder struct {
	apiKey string
	client *http.Client
}

// New returns a Builder. If ANTHROPIC_API_KEY is set in the process env,
// the LLM path is enabled; otherwise Build always uses the heuristic.
// The HTTP client uses an explicit timeout so a runaway request can't
// outlive the parent ctx.
func New() *Builder {
	return &Builder{
		apiKey: strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		client: &http.Client{Timeout: llmTimeout},
	}
}

// Build returns a short title for a task triggered by the given comment.
// fallback is what we return when the comment is empty or all generation
// paths fail — typically the issue or channel title. Build never returns
// an empty string — if everything fails, fallback wins, and if fallback
// is also empty, a generic placeholder is used.
func (b *Builder) Build(ctx context.Context, commentContent, fallback string) string {
	commentContent = strings.TrimSpace(commentContent)
	fallback = strings.TrimSpace(fallback)

	if commentContent == "" {
		return ensureNonEmpty(fallback)
	}

	if b != nil && b.apiKey != "" {
		if title, err := b.callLLM(ctx, commentContent); err == nil {
			cleaned := cleanTitle(title)
			if cleaned != "" {
				return truncateRunes(cleaned, titleMaxRunes)
			}
		} else {
			slog.Debug("agent_title llm fallback", "error", err)
		}
	}

	return ensureNonEmpty(heuristic(commentContent, fallback))
}

// heuristic produces a title without calling the LLM. Strategy: take the
// first non-empty line, strip mention syntax, and cap to titleMaxRunes
// runes. Mentions are stripped because "@Lando lav et summary" reads
// better as just "Lav et summary" once the agent name is known.
func heuristic(commentContent, fallback string) string {
	first := firstMeaningfulLine(commentContent)
	first = stripMentions(first)
	first = strings.TrimSpace(first)
	if first == "" {
		return fallback
	}
	return truncateRunes(first, titleMaxRunes)
}

// firstMeaningfulLine returns the first non-blank line of s. Comments
// often start with "Hej @Lando," — taking the first line keeps the title
// focused on the actual ask, not the greeting that follows on later lines.
func firstMeaningfulLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// stripMentions removes mention syntax entirely. The agent name is
// already shown via avatar + label in the inline row, so repeating
// "Lando" in the title is redundant. We strip the whole reference, not
// just the `@`.
//
// Two forms appear in comments:
//   - markdown link `[@Lando](mention://agent/<id>)` — drop the entire
//     `[…](…)` construct
//   - bare `@Lando` followed by punctuation/space — drop the @-token
//     and any single trailing comma/colon, since the user's prose
//     starts at the next word ("@Lando, lav et summary" → "lav et
//     summary")
func stripMentions(s string) string {
	// Strip mention markdown: [@Name](mention://...) → ""
	for {
		idx := strings.Index(s, "[@")
		if idx < 0 {
			break
		}
		closeBracket := strings.Index(s[idx:], "](")
		if closeBracket < 0 {
			break
		}
		closeBracket += idx
		closeParen := strings.Index(s[closeBracket:], ")")
		if closeParen < 0 {
			break
		}
		closeParen += closeBracket
		s = s[:idx] + s[closeParen+1:]
	}
	s = strings.TrimSpace(s)
	// Strip leading "@Name" tokens (with optional trailing punctuation).
	// Only consume whole `@Word` runs so we don't eat unrelated `@` mid-text.
	for strings.HasPrefix(s, "@") {
		// Find end of the @-token: first space or end of string.
		end := len(s)
		for i := 1; i < len(s); i++ {
			if s[i] == ' ' || s[i] == '\t' {
				end = i
				break
			}
		}
		s = strings.TrimSpace(s[end:])
		// Drop a leading comma/colon left over from "@Lando, please…".
		s = strings.TrimLeft(s, ",: ")
	}
	return s
}

// truncateRunes caps s to maxRunes runes (multibyte-safe), adding an
// ellipsis when truncated. Operates on runes so Chinese/Danish characters
// each count as one.
func truncateRunes(s string, maxRunes int) string {
	rs := []rune(s)
	if len(rs) <= maxRunes {
		return s
	}
	return string(rs[:maxRunes]) + "…"
}

// cleanTitle scrubs LLM output: removes surrounding quotes (Haiku
// sometimes wraps the title in `"…"`), drops a trailing period, and
// trims whitespace.
func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'")
	s = strings.TrimSuffix(s, ".")
	return strings.TrimSpace(s)
}

// ensureNonEmpty guarantees the return value is something the UI can
// render. If both LLM and heuristic produced nothing AND the fallback is
// empty, this is the final stop.
func ensureNonEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "Agent task"
	}
	return s
}

// ─── Anthropic Messages API call ─────────────────────────────────────────

// systemPrompt frames the task for the model. We keep it tight: one
// sentence, declarative, no examples. Examples actually hurt quality
// here because Haiku tends to copy their wording instead of matching the
// user's comment.
const systemPrompt = `You generate a short headline (3-10 words) summarizing what an AI agent is being asked to do. Output only the headline, no quotes, no trailing period. Match the user's language (Danish or English).`

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicResponse struct {
	Content []anthropicContent `json:"content"`
	Error   *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// callLLM hits the Anthropic Messages API. Wrapped in our own ctx with
// llmTimeout so even if the parent ctx has no deadline we never hold the
// comment-post path for more than 2 seconds.
func (b *Builder) callLLM(parent context.Context, commentContent string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, llmTimeout)
	defer cancel()

	body, err := json.Marshal(anthropicRequest{
		Model:     llmModel,
		MaxTokens: 64,
		System:    systemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: commentContent},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", b.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := b.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("anthropic %d: %s", resp.StatusCode, truncateRunes(string(raw), 200))
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("anthropic error: %s", parsed.Error.Message)
	}

	for _, c := range parsed.Content {
		if c.Type == "text" && c.Text != "" {
			return c.Text, nil
		}
	}
	return "", errors.New("no text content in response")
}
