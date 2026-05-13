package notify

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const WebhookContentPlaceholder = "{{content}}"

func BuildWebhookContent(title, body, link, prefix string) string {
	parts := make([]string, 0, 3)
	if title = strings.TrimSpace(title); title != "" {
		parts = append(parts, title)
	}
	if body = strings.TrimSpace(body); body != "" {
		parts = append(parts, body)
	}
	if link = strings.TrimSpace(link); link != "" {
		parts = append(parts, link)
	}
	return prefix + strings.Join(parts, "\n")
}

func RenderWebhookPayload(template string, content string, defaultPayload map[string]any) ([]byte, error) {
	content = strings.TrimSpace(content)
	if strings.TrimSpace(template) == "" {
		payload := make(map[string]any, len(defaultPayload)+1)
		for key, value := range defaultPayload {
			payload[key] = value
		}
		payload["content"] = content
		return json.Marshal(payload)
	}

	var raw any
	decoder := json.NewDecoder(strings.NewReader(template))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return nil, errors.New("webhook payload template must be valid json")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("webhook payload template must contain one json value")
	}

	rendered, replaced := replaceWebhookContentPlaceholder(raw, content)
	if !replaced {
		return nil, errors.New("webhook payload template must include {{content}}")
	}
	return json.Marshal(rendered)
}

func ValidateWebhookPayloadTemplate(template string) error {
	template = strings.TrimSpace(template)
	if template == "" {
		return nil
	}
	if len(template) > 8192 {
		return errors.New("webhook payload template is too long")
	}
	payload, err := RenderWebhookPayload(template, "Multica notification", map[string]any{})
	if err != nil {
		return err
	}
	if !json.Valid(payload) {
		return errors.New("webhook payload template must render valid json")
	}
	return nil
}

func replaceWebhookContentPlaceholder(value any, content string) (any, bool) {
	switch typed := value.(type) {
	case string:
		if strings.Contains(typed, WebhookContentPlaceholder) {
			return strings.ReplaceAll(typed, WebhookContentPlaceholder, content), true
		}
		return typed, false
	case []any:
		replacedAny := false
		next := make([]any, len(typed))
		for i, item := range typed {
			replaced, ok := replaceWebhookContentPlaceholder(item, content)
			next[i] = replaced
			replacedAny = replacedAny || ok
		}
		return next, replacedAny
	case map[string]any:
		replacedAny := false
		next := make(map[string]any, len(typed))
		for key, item := range typed {
			replaced, ok := replaceWebhookContentPlaceholder(item, content)
			next[key] = replaced
			replacedAny = replacedAny || ok
		}
		return next, replacedAny
	default:
		return typed, false
	}
}
