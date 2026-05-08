package notify

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderWebhookPayloadUsesTemplateContentPlaceholder(t *testing.T) {
	payload, err := RenderWebhookPayload(
		`{"msgtype":"text","text":{"content":"{{content}}"}}`,
		"[Multica] Issue updated",
		map[string]any{"title": "Issue updated"},
	)
	if err != nil {
		t.Fatalf("render webhook payload: %v", err)
	}

	var got struct {
		MsgType string `json:"msgtype"`
		Text    struct {
			Content string `json:"content"`
		} `json:"text"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got.MsgType != "text" {
		t.Fatalf("expected msgtype text, got %q", got.MsgType)
	}
	if got.Text.Content != "[Multica] Issue updated" {
		t.Fatalf("unexpected content: %q", got.Text.Content)
	}
}

func TestRenderWebhookPayloadRequiresContentPlaceholder(t *testing.T) {
	_, err := RenderWebhookPayload(`{"msgtype":"text"}`, "Issue updated", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "{{content}}") {
		t.Fatalf("expected missing placeholder error, got %v", err)
	}
}

func TestRenderWebhookPayloadAddsContentToDefaultPayload(t *testing.T) {
	payload, err := RenderWebhookPayload("", "Issue title\nIssue body", map[string]any{
		"title": "Issue title",
	})
	if err != nil {
		t.Fatalf("render default payload: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got["content"] != "Issue title\nIssue body" {
		t.Fatalf("unexpected content: %q", got["content"])
	}
}
