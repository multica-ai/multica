// Package codex drives Codex app-server for local conversations.
package codex

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Event is one normalized conversation event without sequence numbers or
// turn. Types and payloads follow the v2 schema (local-v2.schema.json).
type Event struct {
	ItemID  string
	Type    string
	Final   bool
	Payload map[string]any
	// Image is set on an image item.completed whose bytes the service stores
	// as an attachment before committing.
	Image *InlineImage
}

// InlineImage is an image produced by a tool or image generation.
type InlineImage struct {
	MimeType string
	Data     string
}

type openItem struct {
	kind  string
	text  strings.Builder
	start map[string]any
	// part is the reasoning summary or content part being streamed.
	part string
}

// Normalizer converts one turn's app-server item notifications into v2
// events. Each item ends with exactly one final event: item/completed is
// authoritative, and Close synthesizes finals for items the turn left open.
// The caller only passes notifications of the active turn; resume replay is
// filtered before this point.
type Normalizer struct {
	contextServer string
	open          map[string]*openItem
	order         []string
	completed     map[string]bool
}

// NewNormalizer returns a normalizer. Calls to contextMCPName's
// ask_user_question tool are answered through interaction requests and
// produce no tool items.
func NewNormalizer(contextMCPName string) *Normalizer {
	return &Normalizer{contextServer: contextMCPName, open: map[string]*openItem{}, completed: map[string]bool{}}
}

// Accept normalizes one notification.
func (n *Normalizer) Accept(method string, params json.RawMessage) []Event {
	var p struct {
		ItemID       string          `json:"itemId"`
		Delta        string          `json:"delta"`
		Item         json.RawMessage `json:"item"`
		SummaryIndex int             `json:"summaryIndex"`
		ContentIndex int             `json:"contentIndex"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil
	}
	switch method {
	case "item/agentMessage/delta":
		return n.agentDelta(p.ItemID, p.Delta)
	case "item/reasoning/summaryPartAdded":
		return n.reasoningDelta(p.ItemID, "summary:"+strconv.Itoa(p.SummaryIndex), "")
	case "item/reasoning/summaryTextDelta":
		return n.reasoningDelta(p.ItemID, "summary:"+strconv.Itoa(p.SummaryIndex), p.Delta)
	case "item/reasoning/textDelta":
		return n.reasoningDelta(p.ItemID, "content:"+strconv.Itoa(p.ContentIndex), p.Delta)
	case "item/started", "item/completed":
	default:
		return nil
	}
	var item map[string]any
	if json.Unmarshal(p.Item, &item) != nil || item == nil {
		return nil
	}
	id, _ := item["id"].(string)
	if id == "" || n.completed[id] || n.isQuestion(item) {
		return nil
	}
	if method == "item/started" {
		return n.started(id, item)
	}
	return n.complete(id, item)
}

// Close ends every open item with status ("cancelled" or "failed"),
// keeping content already received.
func (n *Normalizer) Close(status string) []Event {
	var events []Event
	for _, id := range n.order {
		item := n.open[id]
		if item == nil {
			continue
		}
		state := map[string]any{"kind": item.kind, "status": status}
		if item.kind == "assistant_text" || item.kind == "reasoning" {
			state["text"] = item.text.String()
		} else {
			for k, v := range item.start {
				if k != "kind" {
					state[k] = v
				}
			}
			switch item.kind {
			case "command":
				state["output"], state["exit_code"] = "", nil
			case "file_change":
				if _, ok := state["changes"]; !ok {
					state["changes"] = []any{}
				}
			case "tool_call":
				state["output"] = ""
			}
		}
		events = append(events, n.final(id, state))
	}
	return events
}

func (n *Normalizer) agentDelta(id, delta string) []Event {
	if id == "" || delta == "" || n.completed[id] {
		return nil
	}
	var events []Event
	item := n.open[id]
	if item == nil {
		item = n.track(id, "assistant_text", nil)
		events = append(events, Event{ItemID: id, Type: "item.started", Payload: map[string]any{"kind": "assistant_text"}})
	}
	item.text.WriteString(delta)
	return append(events, Event{ItemID: id, Type: "item.delta", Payload: map[string]any{"kind": "assistant_text", "field": "text", "text": delta}})
}

// reasoningDelta streams one reasoning part. Parts are separated the way
// complete joins the final summary and content, so the streamed text matches
// the final text.
func (n *Normalizer) reasoningDelta(id, part, delta string) []Event {
	if id == "" || n.completed[id] {
		return nil
	}
	var events []Event
	item := n.open[id]
	if item == nil {
		item = n.track(id, "reasoning", nil)
		events = append(events, Event{ItemID: id, Type: "item.started", Payload: map[string]any{"kind": "reasoning"}})
	}
	if part != item.part && item.text.Len() > 0 {
		delta = "\n" + delta
	}
	item.part = part
	if delta == "" {
		return events
	}
	item.text.WriteString(delta)
	return append(events, Event{ItemID: id, Type: "item.delta", Payload: map[string]any{"kind": "reasoning", "field": "text", "text": delta}})
}

func (n *Normalizer) started(id string, item map[string]any) []Event {
	if n.open[id] != nil {
		return nil
	}
	var start map[string]any
	switch item["type"] {
	case "contextCompaction":
		start = map[string]any{"kind": "compaction"}
	case "commandExecution":
		start = map[string]any{"kind": "command", "command": str(item, "command")}
		if cwd := str(item, "cwd"); cwd != "" {
			start["cwd"] = cwd
		}
	case "fileChange":
		start = map[string]any{"kind": "file_change", "changes": changes(item["changes"])}
	case "mcpToolCall":
		start = map[string]any{"kind": "tool_call", "tool": mcpToolName(item), "server": str(item, "server"), "input": item["arguments"]}
	case "dynamicToolCall":
		start = map[string]any{"kind": "tool_call", "tool": str(item, "tool"), "input": item["arguments"]}
	case "reasoning":
		start = map[string]any{"kind": "reasoning"}
	default:
		return nil
	}
	n.track(id, start["kind"].(string), start)
	payload := map[string]any{}
	for k, v := range start {
		if k != "changes" {
			payload[k] = v
		}
	}
	return []Event{{ItemID: id, Type: "item.started", Payload: payload}}
}

func (n *Normalizer) complete(id string, item map[string]any) []Event {
	status := itemStatus(str(item, "status"))
	switch item["type"] {
	case "contextCompaction":
		return []Event{n.final(id, map[string]any{"kind": "compaction", "status": status})}
	case "agentMessage":
		state := map[string]any{"kind": "assistant_text", "status": "completed", "text": str(item, "text")}
		if phase := str(item, "phase"); phase != "" {
			state["phase"] = phase
		}
		return []Event{n.final(id, state)}
	case "reasoning":
		parts := append(strSlice(item["summary"]), strSlice(item["content"])...)
		text := strings.Join(parts, "\n")
		if open := n.open[id]; text == "" && open != nil {
			// The desktop reducer replaces an item with its final state.
			text = open.text.String()
		}
		return []Event{n.final(id, map[string]any{"kind": "reasoning", "status": "completed", "text": text})}
	case "commandExecution":
		state := map[string]any{"kind": "command", "status": status, "command": str(item, "command"), "output": str(item, "aggregatedOutput"), "exit_code": nil}
		if cwd := str(item, "cwd"); cwd != "" {
			state["cwd"] = cwd
		}
		if code, ok := item["exitCode"].(float64); ok {
			state["exit_code"] = int64(code)
		}
		if ms, ok := item["durationMs"].(float64); ok && ms >= 0 {
			state["duration_ms"] = int64(ms)
		}
		return []Event{n.final(id, state)}
	case "fileChange":
		return []Event{n.final(id, map[string]any{"kind": "file_change", "status": status, "changes": changes(item["changes"])})}
	case "mcpToolCall":
		state := map[string]any{"kind": "tool_call", "status": status, "tool": mcpToolName(item), "server": str(item, "server"), "input": item["arguments"]}
		var images []InlineImage
		if errValue, failed := item["error"]; failed && errValue != nil {
			state["status"] = "failed"
			state["output"] = jsonString(errValue)
			state["error"] = map[string]any{"kind": "tool_error", "message": nestedString(errValue, "message")}
		} else {
			result, _ := item["result"].(map[string]any)
			var content any
			if result != nil {
				content, images = splitImages(result["content"])
				copied := map[string]any{}
				for k, v := range result {
					copied[k] = v
				}
				copied["content"] = content
				result = copied
			}
			state["output"] = jsonString(orEmpty(result))
		}
		return append([]Event{n.final(id, state)}, imageEvents(id, images)...)
	case "dynamicToolCall":
		raw := item["contentItems"]
		if raw == nil {
			raw = item["content_items"]
		}
		content, images := splitImages(raw)
		state := map[string]any{"kind": "tool_call", "status": status, "tool": str(item, "tool"), "input": item["arguments"], "output": jsonString(content)}
		return append([]Event{n.final(id, state)}, imageEvents(id, images)...)
	case "imageGeneration":
		data := str(item, "result")
		if status == "failed" || !validImage("image/png", data) {
			delete(n.open, id)
			n.completed[id] = true
			return nil
		}
		n.completed[id] = true
		delete(n.open, id)
		return []Event{{ItemID: id, Type: "item.completed", Final: true, Image: &InlineImage{MimeType: "image/png", Data: data},
			Payload: map[string]any{"kind": "image", "status": "completed", "mime_type": "image/png"}}}
	}
	return nil
}

func (n *Normalizer) isQuestion(item map[string]any) bool {
	return item["type"] == "mcpToolCall" && str(item, "server") == n.contextServer && str(item, "tool") == "ask_user_question"
}

func (n *Normalizer) track(id, kind string, start map[string]any) *openItem {
	item := &openItem{kind: kind, start: start}
	n.open[id] = item
	n.order = append(n.order, id)
	return item
}

func (n *Normalizer) final(id string, state map[string]any) Event {
	delete(n.open, id)
	n.completed[id] = true
	return Event{ItemID: id, Type: "item.completed", Final: true, Payload: state}
}

func itemStatus(s string) string {
	switch s {
	case "failed", "declined":
		return "failed"
	case "cancelled", "interrupted":
		return "cancelled"
	}
	return "completed"
}

func mcpToolName(item map[string]any) string {
	return "mcp__" + str(item, "server") + "__" + str(item, "tool")
}

// changes keeps the v2 file_change fields: path, kind and optional
// move_path and diff. app-server reports kind as {"type": ...}.
func changes(raw any) []any {
	list, _ := raw.([]any)
	out := make([]any, 0, len(list))
	for _, c := range list {
		entry, _ := c.(map[string]any)
		if entry == nil || str(entry, "path") == "" {
			continue
		}
		kind, movePath := "update", ""
		switch k := entry["kind"].(type) {
		case map[string]any:
			kind, movePath = str(k, "type"), str(k, "move_path")
		case string:
			kind = k
		}
		switch kind {
		case "add", "delete", "update":
		default:
			kind = "update"
		}
		norm := map[string]any{"path": str(entry, "path"), "kind": kind}
		if movePath != "" {
			norm["kind"], norm["move_path"] = "move", movePath
		}
		if diff := str(entry, "diff"); diff != "" {
			norm["diff"] = diff
		}
		out = append(out, norm)
	}
	return out
}

var dataURL = regexp.MustCompile(`^data:(image/(?:png|jpeg|webp|gif));base64,([A-Za-z0-9+/]*={0,2})$`)
var base64Pattern = regexp.MustCompile(`^[A-Za-z0-9+/]*={0,2}$`)

// splitImages removes image blocks from content and returns them, matching
// the image rules of the TypeScript host (inline base64 only, at most 20).
func splitImages(raw any) (any, []InlineImage) {
	list, ok := raw.([]any)
	if !ok {
		return raw, nil
	}
	kept := make([]any, 0, len(list))
	var images []InlineImage
	for _, b := range list {
		block, _ := b.(map[string]any)
		if img, ok := blockImage(block); ok {
			if len(images) < 20 {
				images = append(images, img)
			}
			continue
		}
		kept = append(kept, b)
	}
	return kept, images
}

func blockImage(block map[string]any) (InlineImage, bool) {
	var mime, data string
	switch str(block, "type") {
	case "image":
		mime, data = str(block, "mimeType"), str(block, "data")
	case "input_image", "inputImage":
		url := str(block, "image_url")
		if url == "" {
			url = str(block, "imageUrl")
		}
		if m := dataURL.FindStringSubmatch(url); m != nil {
			mime, data = m[1], m[2]
		}
	default:
		return InlineImage{}, false
	}
	if !validImage(mime, data) {
		return InlineImage{}, false
	}
	return InlineImage{MimeType: mime, Data: data}, true
}

func validImage(mime, data string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
	default:
		return false
	}
	return data != "" && len(data) <= 36_000_000 && base64Pattern.MatchString(data)
}

func imageEvents(id string, images []InlineImage) []Event {
	events := make([]Event, 0, len(images))
	for i, img := range images {
		image := img
		events = append(events, Event{ItemID: "output-image:" + id + ":" + strconv.Itoa(i), Type: "item.completed", Final: true, Image: &image,
			Payload: map[string]any{"kind": "image", "status": "completed", "mime_type": img.MimeType}})
	}
	return events
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func strSlice(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func nestedString(v any, key string) string {
	m, _ := v.(map[string]any)
	return str(m, key)
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func jsonString(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}
