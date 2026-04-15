package protocol

import "encoding/json"

// Message is the envelope for all WebSocket messages.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// TaskDispatchPayload is sent from server to daemon when a task is assigned.
type TaskDispatchPayload struct {
	TaskID      string `json:"task_id"`
	IssueID     string `json:"issue_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// TaskProgressPayload is sent from daemon to server during task execution.
type TaskProgressPayload struct {
	TaskID  string `json:"task_id"`
	Summary string `json:"summary"`
	Step    int    `json:"step,omitempty"`
	Total   int    `json:"total,omitempty"`
}

// TaskCompletedPayload is sent from daemon to server when a task finishes.
type TaskCompletedPayload struct {
	TaskID string `json:"task_id"`
	PRURL  string `json:"pr_url,omitempty"`
	Output string `json:"output,omitempty"`
}

// TaskMessagePayload represents a single agent execution message (tool call, text, etc.)
type TaskMessagePayload struct {
	TaskID  string         `json:"task_id"`
	IssueID string         `json:"issue_id,omitempty"`
	Seq     int            `json:"seq"`
	Type    string         `json:"type"`              // "text", "tool_use", "tool_result", "error"
	Tool    string         `json:"tool,omitempty"`     // tool name for tool_use/tool_result
	Content string         `json:"content,omitempty"`  // text content
	Input   map[string]any `json:"input,omitempty"`    // tool input (tool_use only)
	Output  string         `json:"output,omitempty"`   // tool output (tool_result only)
}

// DaemonRegisterPayload is sent from daemon to server on connection.
type DaemonRegisterPayload struct {
	DaemonID string        `json:"daemon_id"`
	AgentID  string        `json:"agent_id"`
	Runtimes []RuntimeInfo `json:"runtimes"`
}

// RuntimeInfo describes an available agent runtime on the daemon's machine.
type RuntimeInfo struct {
	Type    string `json:"type"`
	Version string `json:"version"`
	Status  string `json:"status"`
}

// ChatMessagePayload is broadcast when a new chat message is created.
type ChatMessagePayload struct {
	ChatSessionID string `json:"chat_session_id"`
	MessageID     string `json:"message_id"`
	Role          string `json:"role"`
	Content       string `json:"content"`
	TaskID        string `json:"task_id,omitempty"`
	CreatedAt     string `json:"created_at"`
}

// ChatDonePayload is broadcast when an agent finishes responding to a chat message.
type ChatDonePayload struct {
	ChatSessionID string `json:"chat_session_id"`
	TaskID        string `json:"task_id"`
	Content       string `json:"content"`
}

// ChatSessionReadPayload is broadcast when the creator marks a session as read.
// Fires to other devices so their unread counts stay in sync.
type ChatSessionReadPayload struct {
	ChatSessionID string `json:"chat_session_id"`
}

// HeartbeatPayload is sent periodically from daemon to server.
type HeartbeatPayload struct {
	DaemonID     string `json:"daemon_id"`
	AgentID      string `json:"agent_id"`
	CurrentTasks int    `json:"current_tasks"`
}
