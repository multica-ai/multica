// Package recruitinbox implements a private, single-chat Feishu recruitment
// intake processor. Its storage contract is intentionally narrower than the
// general Multica chat channel: raw messages, chat IDs, resource keys, media,
// resumes, contact details, salary values, and free-form evaluations are never
// written to its ledger or logs.
package recruitinbox

import (
	"context"
	"io"
	"time"
)

type State string

const (
	StateProcessing State = "processing"
	StateReplied    State = "replied"
	StateIgnored    State = "ignored"
	StateDeadLetter State = "dead_letter"
)

type Inbound struct {
	MessageID   string
	ChatID      string
	SenderID    string
	SenderType  string
	MessageType string
	Content     string
	CreatedAt   time.Time
}

type ResourceRef struct {
	Key      string
	Kind     string
	Filename string
}

type Resource struct {
	Body        io.ReadCloser
	Filename    string
	ContentType string
}

type Extraction struct {
	Role             string   `json:"role"`
	BudgetPresent    bool     `json:"budget_present"`
	StartDate        string   `json:"start_date"`
	Owner            string   `json:"owner"`
	ProjectLead      string   `json:"project_lead"`
	RuleChange       bool     `json:"rule_change"`
	RuleType         string   `json:"rule_type"`
	AffectedScope    string   `json:"affected_scope"`
	MissingFields    []string `json:"missing_fields"`
	Uncertainties    []string `json:"uncertainties"`
	ProposedNextStep string   `json:"proposed_next_step"`
	Consequential    bool     `json:"consequential"`
	Clarification    string   `json:"clarification_question"`
}

type Summary struct {
	RolePresent        bool
	BudgetPresent      bool
	StartDatePresent   bool
	OwnerPresent       bool
	ProjectLeadPresent bool
	RuleChange         bool
	Consequential      bool
	MissingFieldCount  int
	UncertaintyCount   int
}

// Record is the complete persistent schema. MessageKey is HMAC(message_id),
// not the source ID; SentMessageKey receives the same treatment.
type Record struct {
	MessageKey      string
	SourceMessageID string
	Summary         Summary
	RoleVersion     string
	State           State
	ReceivedAt      time.Time
	UpdatedAt       time.Time
	ErrorCode       string
	SentMessageKey  string
	SentStatus      string
}

type Ledger interface {
	Claim(ctx context.Context, messageKey, sourceMessageID string, receivedAt time.Time) (bool, error)
	Pending(ctx context.Context) ([]Record, error)
	MarkReplied(ctx context.Context, messageKey string, summary Summary, roleVersion, sentMessageKey string, at time.Time) error
	MarkIgnored(ctx context.Context, messageKey string, at time.Time) error
	MarkDeadLetter(ctx context.Context, messageKey, errorCode, sentMessageKey string, at time.Time) error
	Health(ctx context.Context) error
	Close() error
}

type Analyzer interface {
	AnalyzeText(ctx context.Context, text string) (Extraction, error)
	AnalyzeImage(ctx context.Context, image []byte, contentType string) (Extraction, error)
	AnalyzeFile(ctx context.Context, data []byte, filename, contentType string) (Extraction, error)
	Transcribe(ctx context.Context, audio io.Reader, filename string) (string, error)
}

type FeishuClient interface {
	Get(ctx context.Context, messageID string) (Inbound, error)
	Download(ctx context.Context, messageID string, ref ResourceRef) (Resource, error)
	Reply(ctx context.Context, chatID, text, idempotencyKey string) (string, error)
}
