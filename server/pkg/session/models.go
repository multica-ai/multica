package session

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Session represents an agent session tied to a specific issue run.
type Session struct {
	ID                  uuid.UUID       `json:"id"`
	IssueID             uuid.UUID       `json:"issue_id"`
	AgentID             uuid.UUID       `json:"agent_id"`
	RunNumber           int             `json:"run_number"`
	State               json.RawMessage `json:"state"`
	ConversationSummary *string         `json:"conversation_summary,omitempty"`
	WorkingDirectory    *string         `json:"working_directory,omitempty"`
	Branch              *string         `json:"branch,omitempty"`
	FilesModified       []string        `json:"files_modified,omitempty"`
	IsActive            bool            `json:"is_active"`
	CreatedAt           time.Time       `json:"created_at"`
	LastActiveAt        time.Time       `json:"last_active_at"`
	ExpiresAt           *time.Time      `json:"expires_at,omitempty"`
	Version             int             `json:"version"`
}

// StateData is the structured content stored in the session's state JSONB field.
type StateData struct {
	Messages       []Message    `json:"messages,omitempty"`
	ToolResults    []ToolResult `json:"tool_results,omitempty"`
	WorkingDir     string       `json:"working_directory,omitempty"`
	Branch         string       `json:"branch,omitempty"`
	FilesModified  []string     `json:"files_modified,omitempty"`
	AnalysisDone   bool         `json:"analysis_done,omitempty"`
	LastCheckpoint time.Time    `json:"last_checkpoint,omitempty"`
}

// Message represents a conversation message in session state.
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// ToolResult represents a tool execution result stored in session state.
type ToolResult struct {
	ToolName  string    `json:"tool_name"`
	Input     string    `json:"input"`
	Output    string    `json:"output"`
	Timestamp time.Time `json:"timestamp"`
}

// Config holds configurable parameters for the session service.
type Config struct {
	// InactivityExpiry is the duration after which an inactive session expires.
	InactivityExpiry time.Duration
	// MaxMessagesBeforeCompress is the threshold above which older messages are summarized.
	MaxMessagesBeforeCompress int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		InactivityExpiry:          7 * 24 * time.Hour, // 7 days
		MaxMessagesBeforeCompress: 100,
	}
}
