package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// stripCodeFences removes ```json ... ``` wrappers and any text after the closing fence.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "```"); idx >= 0 {
		// Find the start of JSON after opening fence.
		after := s[idx+3:]
		if nl := strings.Index(after, "\n"); nl >= 0 {
			after = after[nl+1:]
		}
		// Find closing fence.
		if end := strings.Index(after, "```"); end >= 0 {
			after = after[:end]
		}
		return strings.TrimSpace(after)
	}
	return s
}

// Category is the triage classification result.
type Category string

const (
	CategoryIssue    Category = "issue"
	CategoryNote     Category = "note"
	CategoryReminder Category = "reminder"
	CategoryAnswer   Category = "answer"
)

// Result is the structured output from the triage classifier.
type Result struct {
	Category    Category `json:"category"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	// For reminders: ISO 8601 datetime or natural language time expression.
	RemindAt string `json:"remind_at,omitempty"`
	// For notes: suggested Obsidian vault path.
	NotePath string `json:"note_path,omitempty"`
	// For answers: the question to answer.
	Question string `json:"question,omitempty"`
	// Confidence 0-1. Below threshold → falls back to inbox.
	Confidence float64 `json:"confidence"`
}

const systemPrompt = `You are a triage agent. You receive messages from a user sent via mobile (Telegram).
Your job is to classify each message into exactly one category and extract structured data.

Categories:
- "issue": Something that needs to be done. A bug, feature request, task, or action item.
- "note": Information to save for later. Links, articles, ideas, knowledge to remember.
- "reminder": Something to be reminded about at a specific time.
- "answer": A question the user wants answered right now.

Rules:
- URLs with no other context → "note"
- "husk", "remind me", "påmind mig", time expressions → "reminder"
- "fix", "lav", "vi skal", "der er en bug", action verbs → "issue"
- Questions ("hvad", "hvordan", "what", "how", "?") → "answer"
- When ambiguous, prefer "note" over "issue" (user can reclassify)

For each message, respond with ONLY a JSON object:
{
  "category": "issue" | "note" | "reminder" | "answer",
  "title": "short descriptive title",
  "description": "fuller context if available",
  "priority": "urgent" | "high" | "medium" | "low" | "none",
  "remind_at": "2024-01-15T14:00:00Z or 'friday at 10' — only for reminders",
  "note_path": "suggested/path/in/vault.md — only for notes",
  "question": "the question to answer — only for answer category",
  "confidence": 0.0-1.0
}

The user communicates primarily in Danish but may mix in English. Understand both.
Title should be in the same language as the message.`

// Classifier uses Claude to classify incoming messages.
type Classifier struct {
	client anthropic.Client
	model  string
}

func NewClassifier(apiKey string) *Classifier {
	return &Classifier{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  anthropic.ModelClaudeHaiku4_5_20251001,
	}
}

func (c *Classifier) Classify(ctx context.Context, text string) (*Result, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: 512,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(text)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("claude API call: %w", err)
	}

	// Extract text content from response.
	var content string
	for _, block := range resp.Content {
		if block.Type == "text" {
			content = block.Text
			break
		}
	}
	if content == "" {
		return nil, fmt.Errorf("empty response from classifier")
	}

	// Strip markdown code fences if present.
	content = stripCodeFences(content)

	var result Result
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("parse classifier response: %w (raw: %s)", err, content)
	}

	return &result, nil
}
