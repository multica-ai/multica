package protocol

import (
	"fmt"
	"slices"
	"time"
)

const MediaMaxBytes int64 = 1 << 30

type MediaInput struct {
	Role         string `json:"role,omitempty"`
	AttachmentID string `json:"attachment_id"`
}

type MediaRequest struct {
	Capability string       `json:"capability"`
	Provider   string       `json:"provider"`
	Prompt     string       `json:"prompt"`
	Inputs     []MediaInput `json:"inputs,omitempty"`
	Model      string       `json:"model,omitempty"`
	Size       string       `json:"size,omitempty"`
	Aspect     string       `json:"aspect,omitempty"`
	Duration   int          `json:"duration,omitempty"`
	IssueID    string       `json:"issue_id,omitempty"`
	TaskID     string       `json:"task_id,omitempty"`
}

func (r MediaRequest) Validate() error {
	if !slices.Contains([]string{"image-generate", "image-edit"}, r.Capability) {
		return fmt.Errorf("unsupported capability")
	}
	if !slices.Contains([]string{"auto", "codex", "agy"}, r.Provider) {
		return fmt.Errorf("unsupported provider")
	}
	if len(r.Prompt) == 0 || len(r.Prompt) > 32000 || len(r.Inputs) > 16 || len(r.Model) > 128 || len(r.Size) > 64 || len(r.Aspect) > 32 || r.Duration != 0 {
		return fmt.Errorf("invalid media parameters")
	}
	for _, in := range r.Inputs {
		if !slices.Contains([]string{"", "input", "subject", "scene", "style"}, in.Role) {
			return fmt.Errorf("invalid input role")
		}
	}
	if r.Capability == "image-edit" && len(r.Inputs) == 0 {
		return fmt.Errorf("this capability requires input")
	}
	if r.Provider != "auto" && MediaCapabilityTag(r.Provider, r.Capability) == "" {
		return fmt.Errorf("provider does not support capability")
	}
	return nil
}

func MediaCapabilityTag(provider, capability string) string {
	switch provider {
	case "codex", "agy":
		if capability == "image-generate" || capability == "image-edit" {
			return "media-image-" + provider + "-v1"
		}
	}
	return ""
}

func MediaProvider(runtimeProvider string) string {
	if runtimeProvider == "antigravity" {
		return "agy"
	}
	return runtimeProvider
}

// MediaTagProvider identifies the media source independently of its host runtime.
func MediaTagProvider(tag string) string {
	switch tag {
	case "media-image-codex-v1":
		return "codex"
	case "media-image-agy-v1":
		return "agy"
	}
	return ""
}

func ValidMediaTag(runtimeProvider, tag string) bool {
	switch source := MediaTagProvider(tag); source {
	case "codex", "agy":
		return source == MediaProvider(runtimeProvider)
	}
	return false
}

type MediaJob struct {
	ID           string       `json:"id"`
	State        string       `json:"state"`
	Request      MediaRequest `json:"request"`
	RuntimeID    string       `json:"runtime_id,omitempty"`
	Error        string       `json:"error,omitempty"`
	AttachmentID string       `json:"attachment_id,omitempty"`
	MarkdownURL  string       `json:"markdown_url,omitempty"`
	Markdown     string       `json:"markdown,omitempty"`
	ExpiresAt    time.Time    `json:"expires_at"`
}

func (j MediaJob) Terminal() bool {
	return j.State == "completed" || j.State == "failed" || j.State == "cancelled"
}

type MediaClaimRequest struct {
	RuntimeID string `json:"runtime_id"`
}
type MediaAssignment struct {
	Job     MediaJob `json:"job"`
	Attempt string   `json:"attempt"`
	Token   string   `json:"token"`
}
type MediaClaimResponse struct {
	Assignment *MediaAssignment `json:"assignment,omitempty"`
}
type MediaResult struct {
	RuntimeID   string `json:"runtime_id"`
	JobID       string `json:"job_id"`
	Attempt     string `json:"attempt"`
	Error       string `json:"error,omitempty"`
	RateLimited bool   `json:"rate_limited,omitempty"`
	RetryAfter  int64  `json:"retry_after,omitempty"` // seconds
}
