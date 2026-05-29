// Package duplicatecheck powers the cerebro "find similar issues at creation
// time" feature (FIR-2504). The candidate-finder reuses the existing
// SearchIssues SQL helpers in the handler package; this file owns only the
// LLM judge step that turns those candidates into a verdict per candidate.
//
// The judge calls the Firtal Data Registry AI Gateway (same gateway used by
// agent_avatar) with a cheap Haiku model. The gateway endpoint is an
// OpenAI-compatible /v1/chat/completions, so the prompt asks Haiku to
// classify each candidate as `duplicate`, `related`, or `unrelated` and
// reply with strict JSON.
//
// The judge is best-effort: any error (gateway down, malformed JSON, etc.)
// returns an empty verdict slice, and the calling handler falls back to "no
// match panel" rather than blocking issue creation.
package duplicatecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// JudgeModel is the cheap Anthropic model we route through the Firtal AI
// Gateway. Haiku 4.5 gives a coherent 3-way classification on short inputs
// and keeps cost in the ~½ øre/oprettelse range targeted in the FIR-2504 PRD.
// Format matches firtalGatewayDefaultModel ("claude-sonnet-4-6") — the gateway
// exposes models under their bare upstream id via /v1/models, and chat
// completions echo that same id back as `model`. Override per workspace with
// CEREBRO_DUPLICATE_CHECK_MODEL if the gateway operator renames a model.
const JudgeModel = "claude-haiku-4-5"

// judgeTimeout caps the gateway call. The duplicate-check panel renders
// asynchronously after the user pauses typing, so 4s is generous — anything
// longer and the panel silently no-shows, which is better than blocking the
// "create" button.
const judgeTimeout = 4 * time.Second

// gatewayChatCompletionsPath mirrors the path used by agent_avatar — the
// gateway exposes an OpenAI-compatible surface under /api/ai/proxy/v1.
const gatewayChatCompletionsPath = "/api/ai/proxy/v1/chat/completions"

// Verdict is the classification Haiku returns for one candidate.
type Verdict string

const (
	VerdictDuplicate Verdict = "duplicate"
	VerdictRelated   Verdict = "related"
	VerdictUnrelated Verdict = "unrelated"
)

// IsActionable reports whether the verdict should appear in the create-issue
// panel. We hide "unrelated" to keep the panel signal-strong (PRD risk #2).
func (v Verdict) IsActionable() bool {
	return v == VerdictDuplicate || v == VerdictRelated
}

// Candidate is one existing issue handed to the judge along with the new
// draft. Kept intentionally small so the gateway prompt is short.
type Candidate struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// Draft is the new-issue text the user is composing.
type Draft struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// JudgedCandidate is the verdict for a single candidate. Reason is one short
// sentence Haiku produces explaining why; it's shown next to the match card
// so the user understands the suggestion.
type JudgedCandidate struct {
	ID      string  `json:"id"`
	Verdict Verdict `json:"verdict"`
	Reason  string  `json:"reason"`
}

// GatewayConfig is the resolved gateway credentials. Empty BaseURL or
// APIKey disables the judge (Judge returns an empty slice).
type GatewayConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

// GatewayConfigFromEnv reads the same env vars agent_avatar uses, so the
// duplicate-check feature inherits whatever gateway the operator already
// configured for image generation.
func GatewayConfigFromEnv() GatewayConfig {
	model := strings.TrimSpace(os.Getenv("CEREBRO_DUPLICATE_CHECK_MODEL"))
	if model == "" {
		model = JudgeModel
	}
	return GatewayConfig{
		BaseURL: strings.TrimRight(strings.TrimSpace(firstNonEmpty(
			os.Getenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL"),
			os.Getenv("FIRTAL_AE_GATEWAY_URL"),
		)), "/"),
		APIKey: strings.TrimSpace(firstNonEmpty(
			os.Getenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY"),
			os.Getenv("FIRTAL_AE_GATEWAY_KEY"),
		)),
		Model: model,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Judger calls the Firtal AI Gateway to classify candidates against the
// draft. A zero Judger uses http.DefaultClient and env-resolved config.
type Judger struct {
	Config GatewayConfig
	Client *http.Client
}

// Judge classifies candidates and returns the verdict slice in the order
// Haiku returned them. Best-effort: returns an empty slice + nil on any
// failure (no gateway configured, network error, malformed response). The
// caller treats an empty result as "no match panel".
func (j *Judger) Judge(parent context.Context, draft Draft, candidates []Candidate) []JudgedCandidate {
	if len(candidates) == 0 {
		return nil
	}
	cfg := j.Config
	if cfg.BaseURL == "" || cfg.APIKey == "" {
		cfg = GatewayConfigFromEnv()
	}
	if cfg.BaseURL == "" || cfg.APIKey == "" {
		return nil
	}
	if cfg.Model == "" {
		cfg.Model = JudgeModel
	}
	client := j.Client
	if client == nil {
		client = http.DefaultClient
	}

	ctx, cancel := context.WithTimeout(parent, judgeTimeout)
	defer cancel()

	prompt := buildPrompt(draft, candidates)
	body := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]any{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": prompt},
		},
		"temperature": 0,
		"max_tokens":  600,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+gatewayChatCompletionsPath, bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-trace-name", "cerebro-duplicate-check")
	req.Header.Set("x-tags", "multica,duplicate-check")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	var parsed openAIResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil || len(parsed.Choices) == 0 {
		return nil
	}
	text := extractText(parsed.Choices[0].Message.Content)
	if text == "" {
		return nil
	}

	verdicts, err := parseJudgeResponse(text)
	if err != nil {
		return nil
	}
	// Preserve only verdicts whose ID actually appears in the candidate
	// list — Haiku occasionally invents identifiers.
	known := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		known[c.ID] = struct{}{}
	}
	filtered := verdicts[:0]
	for _, v := range verdicts {
		if _, ok := known[v.ID]; ok {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// systemPrompt frames the task. Kept tight so Haiku focuses on the
// classification step; the per-candidate reason fits in one short Danish/
// English sentence.
const systemPrompt = `You judge whether a draft issue is the same sag as each existing issue. ` +
	`Output only valid JSON, no prose. Match user's language (Danish or English) for the reason. ` +
	`Schema: {"verdicts":[{"id":"<candidate id>","verdict":"duplicate"|"related"|"unrelated","reason":"<one short sentence>"}]}`

// buildPrompt assembles the user message: the draft, then each candidate
// with its identifier so Haiku can address it by ID in the response.
func buildPrompt(draft Draft, candidates []Candidate) string {
	var b strings.Builder
	b.WriteString("NEW ISSUE DRAFT:\n")
	b.WriteString("Title: ")
	b.WriteString(draft.Title)
	b.WriteString("\n")
	if d := strings.TrimSpace(draft.Description); d != "" {
		b.WriteString("Description: ")
		b.WriteString(truncate(d, 500))
		b.WriteString("\n")
	}
	b.WriteString("\nEXISTING CANDIDATES (judge each):\n")
	for _, c := range candidates {
		fmt.Fprintf(&b, "- id=%s | %s | %s",
			c.ID,
			truncate(c.Identifier, 16),
			truncate(c.Title, 140),
		)
		if d := strings.TrimSpace(c.Description); d != "" {
			b.WriteString(" | desc=")
			b.WriteString(truncate(d, 200))
		}
		b.WriteString("\n")
	}
	b.WriteString(`\nReturn JSON with one verdict per candidate using the id values above.`)
	return b.String()
}

func truncate(s string, max int) string {
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	return string(rs[:max]) + "…"
}

// openAIResponse mirrors the subset of the OpenAI chat-completions response
// we need. The gateway proxies upstream providers so this shape is stable.
type openAIResponse struct {
	Choices []struct {
		Message struct {
			// Content can be a string (OpenAI / Anthropic via gateway) or a
			// list of {type, text} blocks. Accept both via json.RawMessage
			// and the extractText helper.
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// extractText pulls a string out of either a bare-string content field or a
// list of content blocks. Returns "" when neither shape matches.
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var b strings.Builder
		for _, blk := range blocks {
			if blk.Type == "text" || blk.Type == "" {
				b.WriteString(blk.Text)
			}
		}
		return b.String()
	}
	return ""
}

// parseJudgeResponse extracts the verdict list from Haiku's reply. Haiku
// occasionally wraps the JSON in ```json ... ``` fences or prepends a stray
// sentence; we slice from the first '{' to the last '}' before unmarshalling.
func parseJudgeResponse(text string) ([]JudgedCandidate, error) {
	text = strings.TrimSpace(text)
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object in response")
	}
	var wire struct {
		Verdicts []struct {
			ID      string `json:"id"`
			Verdict string `json:"verdict"`
			Reason  string `json:"reason"`
		} `json:"verdicts"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &wire); err != nil {
		return nil, err
	}
	out := make([]JudgedCandidate, 0, len(wire.Verdicts))
	for _, v := range wire.Verdicts {
		verdict := normalizeVerdict(v.Verdict)
		if verdict == "" {
			continue
		}
		out = append(out, JudgedCandidate{
			ID:      v.ID,
			Verdict: verdict,
			Reason:  strings.TrimSpace(v.Reason),
		})
	}
	return out, nil
}

func normalizeVerdict(s string) Verdict {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "duplicate", "dubletter", "dublet", "duplikat":
		return VerdictDuplicate
	case "related", "relateret":
		return VerdictRelated
	case "unrelated", "urelateret":
		return VerdictUnrelated
	}
	return ""
}
