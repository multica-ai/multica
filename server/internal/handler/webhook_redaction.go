package handler

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Decode strings before replacing the known credential: byte replacement alone
// misses JSON-escaped strings, including nested URLs. UseNumber preserves large
// provider identifiers exactly. Keys can contain credentials too.
func redactWebhookJSON(body []byte, token string) ([]byte, error) {
	if token == "" {
		return body, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var redact func(any) any
	redact = func(value any) any {
		switch value := value.(type) {
		case string:
			return strings.ReplaceAll(value, token, "[REDACTED]")
		case []any:
			for i := range value {
				value[i] = redact(value[i])
			}
			return value
		case map[string]any:
			out := make(map[string]any, len(value))
			for key, item := range value {
				out[strings.ReplaceAll(key, token, "[REDACTED]")] = redact(item)
			}
			return out
		default:
			return value
		}
	}
	return json.Marshal(redact(value))
}
