package claude

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Event is one normalized conversation event without sequence numbers or
// turn; the conversation service assigns those when it commits the event.
// Types and payloads follow the v2 schema (local-v2.schema.json).
type Event struct {
	ItemID  string
	Type    string
	Final   bool
	Payload map[string]any
	// Image is set on an image item.completed whose bytes the service must
	// store as an attachment before committing; it then sets attachment_id.
	Image *InlineImage
}

// InlineImage is an image returned inline by a tool.
type InlineImage struct {
	MimeType string
	Data     string
}

// TurnResult is the outcome Claude Code reported for the current root turn.
type TurnResult struct {
	IsError bool
	Error   string
	Output  string
}

type openItem struct {
	kind  string
	text  []byte
	start map[string]any
}

type streamBlock struct {
	itemID  string
	kind    string
	matched bool
}

type backgroundTask struct {
	state      map[string]any
	background bool
	done       bool
}

// Normalizer converts raw Agent SDK messages of one conversation into v2
// events. Every output block ends with exactly one final event: Claude's
// complete assistant blocks and tool results become item.completed, and
// Close synthesizes finals for blocks a turn left open.
type Normalizer struct {
	questionTool string

	sessionID string
	result    *TurnResult

	messageID string
	blocks    map[string][]*streamBlock
	byIndex   map[int]*streamBlock
	ordinal   map[string]int
	open      map[string]*openItem
	order     []string
	toolNames map[string]string
	skipped   map[string]bool
	tasks     map[string]*backgroundTask

	compactionID   string
	compactionSeen map[string]bool
}

// NewNormalizer returns a normalizer. contextMCPName is the Context MCP
// server name; its ask_user_question tool is answered through interaction
// requests, so its calls never appear as tool items.
func NewNormalizer(contextMCPName string) *Normalizer {
	return &Normalizer{
		questionTool: "mcp__" + contextMCPName + "__ask_user_question",
		blocks:       map[string][]*streamBlock{},
		byIndex:      map[int]*streamBlock{},
		ordinal:      map[string]int{},
		open:         map[string]*openItem{},
		toolNames:    map[string]string{},
		skipped:      map[string]bool{},
		tasks:        map[string]*backgroundTask{},

		compactionSeen: map[string]bool{},
	}
}

// SessionID is the native Claude session id seen so far.
func (n *Normalizer) SessionID() string { return n.sessionID }

// TakeResult returns and clears the last turn result.
func (n *Normalizer) TakeResult() *TurnResult {
	r := n.result
	n.result = nil
	return r
}

type sdkMessage struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	SessionID       string          `json:"session_id"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	Event           *streamEvent    `json:"event"`
	Message         *apiMessage     `json:"message"`
	IsError         bool            `json:"is_error"`
	Result          string          `json:"result"`
	Errors          []string        `json:"errors"`
	TaskID          string          `json:"task_id"`
	Description     string          `json:"description"`
	Summary         *string         `json:"summary"`
	Status          string          `json:"status"`
	UUID            string          `json:"uuid"`
	CompactResult   string          `json:"compact_result"`
	CompactError    string          `json:"compact_error"`
	LastToolName    string          `json:"last_tool_name"`
	Usage           json.RawMessage `json:"usage"`
	Patch           map[string]any  `json:"patch"`
	Tasks           []struct {
		TaskID      string `json:"task_id"`
		Description string `json:"description"`
	} `json:"tasks"`
}

type streamEvent struct {
	Type         string               `json:"type"`
	Index        int                  `json:"index"`
	Message      *struct{ ID string } `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"content_block"`
	Delta *struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
	} `json:"delta"`
}

type apiMessage struct {
	ID      string         `json:"id"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Source    *struct {
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

// Accept normalizes one raw SDK message.
func (n *Normalizer) Accept(raw json.RawMessage) []Event {
	var m sdkMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	if m.SessionID != "" {
		n.sessionID = m.SessionID
	}
	// Subagent transcript frames belong to the tool call that spawned them.
	if m.ParentToolUseID != nil && *m.ParentToolUseID != "" {
		return nil
	}
	switch m.Type {
	case "stream_event":
		return n.streamEvent(m.Event)
	case "assistant":
		return n.assistant(m.Message)
	case "user":
		return n.user(m.Message)
	case "result":
		r := &TurnResult{IsError: m.IsError, Output: m.Result}
		if m.IsError {
			r.Error = m.Result
			if r.Error == "" && len(m.Errors) > 0 {
				r.Error = strings.Join(m.Errors, "\n")
			}
			if r.Error == "" {
				r.Error = "Claude Code execution failed"
			}
			r.Output = ""
		}
		n.result = r
	case "system":
		return n.system(m)
	}
	return nil
}

func (n *Normalizer) streamEvent(e *streamEvent) []Event {
	if e == nil {
		return nil
	}
	switch e.Type {
	case "message_start":
		if e.Message != nil {
			n.messageID = e.Message.ID
		}
		n.byIndex = map[int]*streamBlock{}
	case "content_block_start":
		if e.ContentBlock == nil {
			return nil
		}
		kind := textKind(e.ContentBlock.Type)
		block := &streamBlock{kind: kind}
		if kind != "" {
			block.itemID = n.nextTextItemID(n.messageID)
		}
		n.byIndex[e.Index] = block
		n.blocks[n.messageID] = append(n.blocks[n.messageID], block)
		if kind == "" {
			return nil
		}
		n.openItem(block.itemID, kind, nil)
		return []Event{{ItemID: block.itemID, Type: "item.started", Payload: map[string]any{"kind": kind}}}
	case "content_block_delta":
		block := n.byIndex[e.Index]
		if block == nil || block.kind == "" || e.Delta == nil {
			return nil
		}
		text := e.Delta.Text
		if block.kind == "reasoning" {
			text = e.Delta.Thinking
		}
		if text == "" {
			return nil
		}
		if item := n.open[block.itemID]; item != nil {
			item.text = append(item.text, text...)
		}
		return []Event{{ItemID: block.itemID, Type: "item.delta", Payload: map[string]any{"kind": block.kind, "field": "text", "text": text}}}
	}
	return nil
}

// assistant handles a complete assistant message. Its text and thinking
// blocks are the final state of the matching streamed blocks; tool_use
// blocks start tool items that finish with their tool_result.
func (n *Normalizer) assistant(msg *apiMessage) []Event {
	if msg == nil {
		return nil
	}
	var events []Event
	for _, block := range msg.Content {
		switch block.Type {
		case "text", "thinking":
			kind := textKind(block.Type)
			itemID := n.matchStreamed(msg.ID, kind)
			text := block.Text
			if kind == "reasoning" {
				text = block.Thinking
			}
			events = append(events, n.complete(itemID, map[string]any{"kind": kind, "status": "completed", "text": text}))
		case "tool_use":
			n.matchStreamed(msg.ID, "")
			if block.Name == n.questionTool {
				n.skipped[block.ID] = true
				continue
			}
			n.toolNames[block.ID] = block.Name
			start := toolStart(block.Name, block.Input)
			n.openItem(block.ID, start["kind"].(string), start)
			events = append(events, Event{ItemID: block.ID, Type: "item.started", Payload: start})
		}
	}
	return events
}

func (n *Normalizer) user(msg *apiMessage) []Event {
	if msg == nil {
		return nil
	}
	var events []Event
	for _, block := range msg.Content {
		if block.Type != "tool_result" || n.skipped[block.ToolUseID] {
			continue
		}
		name, known := n.toolNames[block.ToolUseID]
		item := n.open[block.ToolUseID]
		// An unknown or already finished call gets no second final event.
		if !known || item == nil {
			continue
		}
		output, images := toolOutput(block.Content)
		state := toolStart(name, item.startInput())
		state["status"] = "completed"
		state["output"] = output
		if state["kind"] == "command" {
			state["exit_code"] = nil
		}
		if block.IsError {
			state["status"] = "failed"
			state["error"] = map[string]any{"kind": "tool_error", "message": output}
		}
		events = append(events, n.complete(block.ToolUseID, state))
		if block.IsError {
			continue
		}
		for i, image := range images {
			id := "output-image:" + block.ToolUseID + ":" + strconv.Itoa(i)
			img := image
			events = append(events, Event{ItemID: id, Type: "item.completed", Final: true, Image: &img,
				Payload: map[string]any{"kind": "image", "status": "completed", "mime_type": image.MimeType}})
		}
	}
	return events
}

func (n *Normalizer) system(m sdkMessage) []Event {
	switch m.Subtype {
	case "status", "compact_boundary":
		return n.compaction(m)
	case "task_started":
		if m.TaskID == "" || n.tasks[m.TaskID] != nil {
			return nil
		}
		state := map[string]any{"kind": "background_task", "status": "in_progress", "task_id": m.TaskID, "description": m.Description}
		n.tasks[m.TaskID] = &backgroundTask{state: state}
		return []Event{{ItemID: taskItemID(m.TaskID), Type: "item.started", Payload: map[string]any{"kind": "background_task", "task_id": m.TaskID, "description": m.Description}}}
	case "task_progress":
		task := n.tasks[m.TaskID]
		if task == nil || task.done {
			return nil
		}
		if m.Summary != nil {
			task.state["progress_summary"] = *m.Summary
		}
		applyTaskUsage(task.state, m.Usage)
		return []Event{n.taskEvent(m.TaskID, "")}
	case "task_updated":
		task := n.tasks[m.TaskID]
		if task == nil || task.done {
			return nil
		}
		if desc, ok := m.Patch["description"].(string); ok {
			task.state["description"] = desc
		}
		if bg, ok := m.Patch["is_backgrounded"].(bool); ok {
			task.background = bg
		}
		applyTaskUsage(task.state, m.Usage)
		status := map[string]string{"completed": "completed", "failed": "failed", "killed": "cancelled"}[stringValue(m.Patch["status"])]
		if errText, ok := m.Patch["error"].(string); ok && status != "" {
			task.state["summary"] = errText
		}
		return []Event{n.taskEvent(m.TaskID, status)}
	case "task_notification":
		task := n.tasks[m.TaskID]
		status := map[string]string{"completed": "completed", "failed": "failed", "stopped": "cancelled"}[m.Status]
		if task == nil || task.done || status == "" {
			return nil
		}
		if m.Summary != nil {
			task.state["summary"] = *m.Summary
		}
		applyTaskUsage(task.state, m.Usage)
		return []Event{n.taskEvent(m.TaskID, status)}
	case "background_tasks_changed":
		live := map[string]bool{}
		for _, t := range m.Tasks {
			live[t.TaskID] = true
		}
		var events []Event
		ids := make([]string, 0, len(n.tasks))
		for id := range n.tasks {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			task := n.tasks[id]
			if task.background && !task.done && !live[id] {
				events = append(events, n.taskEvent(id, "completed"))
			}
		}
		for _, t := range m.Tasks {
			if task := n.tasks[t.TaskID]; task != nil {
				task.background = true
			}
		}
		return events
	}
	return nil
}

// Close ends every open foreground item with status ("cancelled" or
// "failed"), keeping the content already received, so each item still has
// exactly one final event. Background tasks outlive a turn and stay open.
func (n *Normalizer) Close(status string) []Event {
	var events []Event
	for _, id := range n.order {
		item := n.open[id]
		if item == nil {
			continue
		}
		state := map[string]any{"kind": item.kind, "status": status}
		switch item.kind {
		case "compaction":
			// Compaction has no streamed content to retain.
		case "assistant_text", "reasoning":
			state["text"] = string(item.text)
		default:
			for k, v := range item.start {
				if k != "kind" {
					state[k] = v
				}
			}
			state["output"] = ""
			if item.kind == "command" {
				state["exit_code"] = nil
			}
		}
		events = append(events, n.complete(id, state))
	}
	n.blocks = map[string][]*streamBlock{}
	n.byIndex = map[int]*streamBlock{}
	n.compactionID = ""
	return events
}

func (n *Normalizer) taskEvent(taskID, terminal string) Event {
	task := n.tasks[taskID]
	state := map[string]any{}
	for k, v := range task.state {
		state[k] = v
	}
	if terminal == "" {
		return Event{ItemID: taskItemID(taskID), Type: "item.updated", Payload: state}
	}
	state["status"] = terminal
	task.done = true
	return Event{ItemID: taskItemID(taskID), Type: "item.completed", Final: true, Payload: state}
}

func (n *Normalizer) openItem(id, kind string, start map[string]any) {
	if _, exists := n.open[id]; !exists {
		n.order = append(n.order, id)
	}
	n.open[id] = &openItem{kind: kind, start: start}
}

func (n *Normalizer) complete(id string, state map[string]any) Event {
	delete(n.open, id)
	return Event{ItemID: id, Type: "item.completed", Final: true, Payload: state}
}

// matchStreamed pairs a block of a complete assistant message with the next
// unmatched streamed block of the same message. Claude Code emits complete
// blocks in stream order, possibly one per assistant message. Without
// partial messages a fresh item id is allocated.
func (n *Normalizer) matchStreamed(messageID, kind string) string {
	for _, block := range n.blocks[messageID] {
		if block.matched {
			continue
		}
		block.matched = true
		if block.kind == kind && block.itemID != "" {
			return block.itemID
		}
		break
	}
	if kind == "" {
		return ""
	}
	return n.nextTextItemID(messageID)
}

func (n *Normalizer) nextTextItemID(messageID string) string {
	i := n.ordinal[messageID]
	n.ordinal[messageID] = i + 1
	return messageID + ":" + strconv.Itoa(i)
}

func (o *openItem) startInput() json.RawMessage {
	if o.start == nil {
		return nil
	}
	data, _ := json.Marshal(o.start["input"])
	return data
}

func textKind(blockType string) string {
	switch blockType {
	case "text":
		return "assistant_text"
	case "thinking":
		return "reasoning"
	}
	return ""
}

func toolStart(name string, input json.RawMessage) map[string]any {
	var parsed any
	if len(input) > 0 {
		_ = json.Unmarshal(input, &parsed)
	}
	if name == "Bash" {
		fields, _ := parsed.(map[string]any)
		command, _ := fields["command"].(string)
		return map[string]any{"kind": "command", "command": command, "input": parsed}
	}
	return map[string]any{"kind": "tool_call", "tool": name, "input": parsed}
}

// toolOutput returns tool_result content as text, with inline images removed
// from the text and returned separately.
func toolOutput(raw json.RawMessage) (string, []InlineImage) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return string(raw), nil
	}
	var kept []json.RawMessage
	var raws []json.RawMessage
	_ = json.Unmarshal(raw, &raws)
	var images []InlineImage
	for i, b := range blocks {
		if img, ok := inlineImage(b); ok {
			if len(images) < 20 {
				images = append(images, img)
			}
			continue
		}
		kept = append(kept, raws[i])
	}
	if kept == nil {
		kept = []json.RawMessage{}
	}
	out, _ := json.Marshal(kept)
	return string(out), images
}

var base64Pattern = regexp.MustCompile(`^[A-Za-z0-9+/]*={0,2}$`)

func inlineImage(b contentBlock) (InlineImage, bool) {
	if b.Type != "image" || b.Source == nil {
		return InlineImage{}, false
	}
	switch b.Source.MediaType {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
	default:
		return InlineImage{}, false
	}
	if b.Source.Data == "" || len(b.Source.Data) > 36_000_000 || !base64Pattern.MatchString(b.Source.Data) {
		return InlineImage{}, false
	}
	return InlineImage{MimeType: b.Source.MediaType, Data: b.Source.Data}, true
}

func applyTaskUsage(state map[string]any, raw json.RawMessage) {
	var u struct {
		TotalTokens *float64 `json:"total_tokens"`
		ToolUses    *float64 `json:"tool_uses"`
		DurationMs  *float64 `json:"duration_ms"`
	}
	if json.Unmarshal(raw, &u) != nil || u.TotalTokens == nil || u.ToolUses == nil || u.DurationMs == nil ||
		*u.TotalTokens < 0 || *u.ToolUses < 0 || *u.DurationMs < 0 {
		return
	}
	state["usage"] = map[string]any{"total_tokens": int64(*u.TotalTokens), "tool_uses": int64(*u.ToolUses), "duration_ms": int64(*u.DurationMs)}
}

func taskItemID(taskID string) string { return "task:" + taskID }

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
