package kagentbroker

import "time"

func validRPCResponse(m, request map[string]any) bool {
	if !keys(m, []string{"jsonrpc", "id", "result"}) || m["jsonrpc"] != "2.0" || m["id"] != request["id"] {
		return false
	}
	result := object(m["result"])
	if request["method"] == "SendMessage" {
		if !keys(result, []string{"task"}, "message") || result["message"] != nil {
			return false
		}
		result = object(result["task"])
	} else if str(result["id"]) != str(object(request["params"])["id"]) {
		return false
	}
	return validTask(result)
}
func validTask(t map[string]any) bool {
	if !keys(t, []string{"id", "contextId", "status"}, "artifacts", "history", "metadata", "extensions") || !uuidPattern.MatchString(str(t["id"])) || !uuidPattern.MatchString(str(t["contextId"])) {
		return false
	}
	if !stripOptional(t) {
		return false
	}
	if v, ok := t["history"]; ok {
		h, ok := v.([]any)
		if !ok || len(h) > 256 {
			return false
		}
		for _, v := range h {
			if !validMessage(object(v), t, true) {
				return false
			}
		}
		delete(t, "history")
	}
	s := object(t["status"])
	if !keys(s, []string{"state"}, "message", "timestamp") {
		return false
	}
	switch s["state"] {
	case "TASK_STATE_SUBMITTED", "TASK_STATE_WORKING", "TASK_STATE_COMPLETED", "TASK_STATE_FAILED", "TASK_STATE_CANCELED", "TASK_STATE_REJECTED", "TASK_STATE_INPUT_REQUIRED", "TASK_STATE_AUTH_REQUIRED":
	default:
		return false
	}
	if v, ok := s["timestamp"]; ok {
		if _, err := time.Parse(time.RFC3339Nano, str(v)); err != nil {
			return false
		}
	}
	if v, ok := s["message"]; ok {
		if !validMessage(object(v), t, false) {
			return false
		}
	}
	if v, ok := t["artifacts"]; ok {
		a, ok := v.([]any)
		if !ok || len(a) > 32 {
			return false
		}
		for _, v := range a {
			m := object(v)
			if !keys(m, []string{"parts"}, "artifactId", "name", "description", "metadata", "extensions") || !stripOptional(m) || !textParts(m["parts"]) {
				return false
			}
			for _, k := range []string{"artifactId", "name", "description"} {
				if _, ok := m[k]; ok && !optionalText(m, k, false) {
					return false
				}
			}
		}
	}
	return true
}
func textParts(v any) bool {
	a, ok := v.([]any)
	if !ok || len(a) > 32 {
		return false
	}
	for _, v := range a {
		p := object(v)
		if !keys(p, []string{"text"}, "metadata", "extensions") || !stripOptional(p) || !optionalText(p, "text", false) {
			return false
		}
	}
	return true
}

// Optional extension content is bounded by the parser and response byte limit.
// It is never returned: the Multica client consumes only status and text parts.
func stripOptional(m map[string]any) bool {
	if v, ok := m["metadata"]; ok {
		if object(v) == nil || len(object(v)) > 128 {
			return false
		}
		delete(m, "metadata")
	}
	if v, ok := m["extensions"]; ok {
		a, ok := v.([]any)
		if !ok || len(a) > 32 {
			return false
		}
		for _, v := range a {
			s, ok := v.(string)
			if !ok || len(s) > 2048 {
				return false
			}
		}
		delete(m, "extensions")
	}
	return true
}

func validMessage(m, task map[string]any, history bool) bool {
	if !keys(m, []string{"messageId", "role", "parts"}, "contextId", "taskId", "metadata", "extensions") ||
		!optionalText(m, "messageId", false) || str(m["messageId"]) == "" || !stripOptional(m) || !textParts(m["parts"]) {
		return false
	}
	if m["role"] != "ROLE_AGENT" && (!history || m["role"] != "ROLE_USER") {
		return false
	}
	if c, ok := m["contextId"]; ok && c != task["contextId"] {
		return false
	}
	if id, ok := m["taskId"]; ok && id != task["id"] {
		return false
	}
	return true
}
