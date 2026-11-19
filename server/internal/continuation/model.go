// Package continuation detects an explicitly enrolled, unfinished remainder.
// It never starts tasks or repeats external operations.
package continuation

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var ErrConflict = errors.New("continuation version changed; read the current contract before updating")

type Key struct {
	WorkspaceID string `json:"workspace_id"`
	IssueID     string `json:"issue_id"`
	ChainID     string `json:"chain_id"`
	RemainderID string `json:"remainder_id"`
}

// Contract is an operator-attested handoff, not an inference from issue status
// or free-form model output. Automatic receiver adoption keeps the generation;
// an operator can explicitly rearm a delivered incident for a new live receiver.
type Contract struct {
	Key
	Generation        int64      `json:"generation"`
	OwnerID           string     `json:"owner_id"`
	SourceTaskID      string     `json:"source_task_id,omitempty"`
	ReceiverTaskID    string     `json:"receiver_task_id,omitempty"`
	WakeupID          string     `json:"wakeup_id,omitempty"`
	WakeupRevision    int32      `json:"wakeup_revision,omitempty"`
	DueAt             time.Time  `json:"due_at"`
	WaitKind          string     `json:"wait_kind"`
	WaitUntil         *time.Time `json:"wait_until,omitempty"`
	ExternalOutcome   string     `json:"external_outcome"`
	ResultCommentID   string     `json:"result_comment_id,omitempty"`
	CoverageConfirmed bool       `json:"coverage_confirmed"`
	Evidence          string     `json:"evidence"`
}

type Change struct {
	Contract
	ExpectedVersion int64 `json:"expected_version"`
	NewGeneration   bool  `json:"new_generation"`
}

type Record struct {
	Contract
	Version        int64     `json:"version"`
	State          string    `json:"state"`
	LastProgressAt time.Time `json:"last_progress_at"`
}

func (c Contract) validate() error {
	for name, value := range map[string]string{"workspace_id": c.WorkspaceID, "issue_id": c.IssueID, "chain_id": c.ChainID, "remainder_id": c.RemainderID, "owner_id": c.OwnerID} {
		if _, err := uuid.Parse(value); err != nil {
			return fmt.Errorf("invalid %s", name)
		}
	}
	for name, value := range map[string]string{"source_task_id": c.SourceTaskID, "receiver_task_id": c.ReceiverTaskID, "wakeup_id": c.WakeupID, "result_comment_id": c.ResultCommentID} {
		if value != "" {
			if _, err := uuid.Parse(value); err != nil {
				return fmt.Errorf("invalid %s", name)
			}
		}
	}
	if c.Generation < 1 || c.DueAt.IsZero() || len(c.Evidence) == 0 || len(c.Evidence) > 2000 {
		return errors.New("generation, due_at and bounded evidence are required")
	}
	if (c.WakeupID == "") != (c.WakeupRevision == 0) || c.WakeupRevision < 0 {
		return errors.New("wakeup requires its exact revision")
	}
	switch c.WaitKind {
	case "none":
		if c.WaitUntil != nil {
			return errors.New("wait_until requires a wait_kind")
		}
	case "ci", "owner", "quota":
		if c.WaitUntil == nil || c.WaitUntil.IsZero() {
			return errors.New("a confirmed wait requires a deadline")
		}
	default:
		return errors.New("wait_kind must be none, ci, owner or quota")
	}
	if c.ExternalOutcome != "none" && c.ExternalOutcome != "unknown" {
		return errors.New("external_outcome must be none or unknown")
	}
	if c.CoverageConfirmed != (c.ResultCommentID != "") {
		return errors.New("resolution requires both an explicit coverage attestation and a result comment")
	}
	if c.CoverageConfirmed && c.ExternalOutcome == "unknown" {
		return errors.New("reconcile the external outcome before resolving the remainder")
	}
	return nil
}

type facts struct {
	status          string
	receiverPresent bool
	receiverMissing bool
	handoffChanged  bool
	terminalAt      *time.Time
}

func liveReceiver(status string) bool {
	switch status {
	case "queued", "deferred", "dispatched", "running", "waiting_local_directory":
		return true
	default:
		return false
	}
}

func decide(c Contract, f facts, now time.Time) (state, reason string) {
	if c.CoverageConfirmed {
		return "resolved", ""
	}
	if f.handoffChanged {
		return "needs_reconcile", "handoff_changed"
	}
	if f.receiverMissing {
		return "needs_reconcile", "receiver_missing"
	}
	switch f.status {
	case "queued", "deferred", "dispatched", "waiting_local_directory":
		return "receiver_queued", ""
	case "running":
		return "receiver_running", ""
	}
	if c.ExternalOutcome == "unknown" {
		return "needs_reconcile", "external_outcome_unknown"
	}
	if c.WaitKind != "none" && c.WaitUntil != nil && now.Before(*c.WaitUntil) {
		return "awaiting_event", ""
	}
	if f.receiverPresent {
		switch f.status {
		case "failed", "cancelled":
			return "needs_reconcile", "receiver_" + f.status
		case "completed":
			return "needs_reconcile", "completed_with_remainder"
		default:
			return "needs_reconcile", "receiver_state_unknown"
		}
	}
	if !now.Before(c.DueAt) || (c.WaitUntil != nil && !now.Before(*c.WaitUntil)) {
		return "needs_reconcile", "deadline"
	}
	return "awaiting_event", ""
}
