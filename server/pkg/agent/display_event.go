package agent

import "encoding/json"

const traceChannelDisplayEvent = "display_event"

func emitDisplayEvent(trace TraceCallback, eventType, title, content string, metadata map[string]any) {
	if trace == nil {
		return
	}
	payload := map[string]any{
		"type":    eventType,
		"title":   title,
		"content": content,
	}
	if len(metadata) > 0 {
		payload["metadata"] = metadata
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	trace(traceChannelDisplayEvent, string(data), "")
}
