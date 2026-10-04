package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/util"
)

// AgentQuestionPayload is the structured question an agent asked the human
// (Claude Code's AskUserQuestion), as stored on comment.question_payload and
// rendered by the issue timeline as an interactive card (GitHub #8048).
//
// The daemon forwards the tool input verbatim, so ParseAgentQuestions accepts
// Claude's camelCase `multiSelect` next to the stored snake_case
// `multi_select`; the stored form is always snake_case.
type AgentQuestionPayload struct {
	Questions []AgentQuestion `json:"questions"`
}

// AgentQuestion is one question with its selectable options.
type AgentQuestion struct {
	Question    string                `json:"question"`
	Header      string                `json:"header,omitempty"`
	MultiSelect bool                  `json:"multi_select"`
	Options     []AgentQuestionOption `json:"options"`
}

// AgentQuestionOption is one selectable answer.
type AgentQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Bounds on a question payload. Claude Code itself caps a call at four
// questions with two to four options each; the limits here are wider so a
// future tool revision does not get rejected, but bounded so a runaway payload
// cannot bloat the comment row or the inbox body.
const (
	agentQuestionMaxQuestions       = 8
	agentQuestionMaxOptions         = 12
	agentQuestionMaxQuestionRunes   = 2000
	agentQuestionMaxHeaderRunes     = 64
	agentQuestionMaxLabelRunes      = 200
	agentQuestionMaxDescRunes       = 1000
	agentQuestionMaxPayloadBytes    = 64 * 1024
	agentQuestionAnswerHintMarkdown = "_Answer in this thread — pick an option or write your own reply. The agent resumes once you reply._"
)

var errAgentQuestionEmpty = errors.New("question payload has no questions")

// agentQuestionWire is the accepted input shape: the stored snake_case form
// and Claude's camelCase form both decode into it.
type agentQuestionWire struct {
	Question         string                `json:"question"`
	Header           string                `json:"header"`
	MultiSelect      *bool                 `json:"multi_select"`
	MultiSelectCamel *bool                 `json:"multiSelect"`
	Options          []AgentQuestionOption `json:"options"`
}

// ParseAgentQuestions validates a daemon-reported `questions` array and
// returns the normalized payload. Text is trimmed, NUL-scrubbed for
// PostgreSQL, and bounded; anything structurally wrong is an error rather
// than a silently narrowed card.
func ParseAgentQuestions(raw json.RawMessage) (AgentQuestionPayload, error) {
	if len(raw) == 0 {
		return AgentQuestionPayload{}, errAgentQuestionEmpty
	}
	if len(raw) > agentQuestionMaxPayloadBytes {
		return AgentQuestionPayload{}, fmt.Errorf("question payload exceeds %d bytes", agentQuestionMaxPayloadBytes)
	}
	var wire []agentQuestionWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return AgentQuestionPayload{}, fmt.Errorf("questions must be an array: %w", err)
	}
	if len(wire) == 0 {
		return AgentQuestionPayload{}, errAgentQuestionEmpty
	}
	if len(wire) > agentQuestionMaxQuestions {
		return AgentQuestionPayload{}, fmt.Errorf("too many questions: %d > %d", len(wire), agentQuestionMaxQuestions)
	}

	out := AgentQuestionPayload{Questions: make([]AgentQuestion, 0, len(wire))}
	for i, w := range wire {
		q := AgentQuestion{
			Question: cleanQuestionText(w.Question, agentQuestionMaxQuestionRunes),
			Header:   cleanQuestionText(w.Header, agentQuestionMaxHeaderRunes),
		}
		if q.Question == "" {
			return AgentQuestionPayload{}, fmt.Errorf("question %d has no text", i+1)
		}
		switch {
		case w.MultiSelect != nil:
			q.MultiSelect = *w.MultiSelect
		case w.MultiSelectCamel != nil:
			q.MultiSelect = *w.MultiSelectCamel
		}
		if len(w.Options) > agentQuestionMaxOptions {
			return AgentQuestionPayload{}, fmt.Errorf("question %d has too many options: %d > %d", i+1, len(w.Options), agentQuestionMaxOptions)
		}
		q.Options = make([]AgentQuestionOption, 0, len(w.Options))
		for j, o := range w.Options {
			opt := AgentQuestionOption{
				Label:       cleanQuestionText(o.Label, agentQuestionMaxLabelRunes),
				Description: cleanQuestionText(o.Description, agentQuestionMaxDescRunes),
			}
			if opt.Label == "" {
				return AgentQuestionPayload{}, fmt.Errorf("question %d option %d has no label", i+1, j+1)
			}
			q.Options = append(q.Options, opt)
		}
		out.Questions = append(out.Questions, q)
	}
	return out, nil
}

func cleanQuestionText(s string, maxRunes int) string {
	s = strings.TrimSpace(util.SanitizeTextForPostgres(s))
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:maxRunes]))
}

// Markdown renders the payload as the comment body. The card is the primary
// surface, but the body is what every other reader sees — the mobile app,
// inbox previews, chat integrations, `multica issue comment list` — so it has
// to stand on its own as a readable question.
func (p AgentQuestionPayload) Markdown() string {
	var b strings.Builder
	for i, q := range p.Questions {
		if i > 0 {
			b.WriteString("\n")
		}
		if q.Header != "" {
			fmt.Fprintf(&b, "**%s** — %s\n", escapeQuestionMarkdown(q.Header), escapeQuestionMarkdown(q.Question))
		} else {
			fmt.Fprintf(&b, "**%s**\n", escapeQuestionMarkdown(q.Question))
		}
		if q.MultiSelect && len(q.Options) > 0 {
			b.WriteString("_(choose any that apply)_\n")
		}
		for _, o := range q.Options {
			if o.Description != "" {
				fmt.Fprintf(&b, "- **%s** — %s\n", escapeQuestionMarkdown(o.Label), escapeQuestionMarkdown(o.Description))
			} else {
				fmt.Fprintf(&b, "- **%s**\n", escapeQuestionMarkdown(o.Label))
			}
		}
	}
	b.WriteString("\n")
	b.WriteString(agentQuestionAnswerHintMarkdown)
	b.WriteString("\n")
	return b.String()
}

// escapeQuestionMarkdown keeps option labels such as `--dry-run` or `*.ts`
// from being read as list markers or emphasis inside the rendered body. The
// card renders the raw payload, so this only affects the plain-markdown view.
func escapeQuestionMarkdown(s string) string {
	r := strings.NewReplacer("*", "\\*", "_", "\\_", "`", "\\`", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
