package agent

import "encoding/json"

func forceStreamJSONToolInputForeground(input map[string]any) bool {
	if runInBackground, ok := input["run_in_background"].(bool); ok && runInBackground {
		input["run_in_background"] = false
		return true
	}
	return false
}

func streamJSONToolResultHasAsyncLaunch(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	switch v := value.(type) {
	case map[string]any:
		if streamJSONMapHasAsyncLaunchStatus(v) {
			return true
		}
		if content, ok := v["content"].([]any); ok {
			return streamJSONArrayHasAsyncLaunchStatus(content)
		}
	case []any:
		return streamJSONArrayHasAsyncLaunchStatus(v)
	}
	return false
}

func streamJSONArrayHasAsyncLaunchStatus(values []any) bool {
	for _, value := range values {
		if item, ok := value.(map[string]any); ok && streamJSONMapHasAsyncLaunchStatus(item) {
			return true
		}
	}
	return false
}

func streamJSONMapHasAsyncLaunchStatus(value map[string]any) bool {
	status, ok := value["status"].(string)
	return ok && status == "async_launched"
}
