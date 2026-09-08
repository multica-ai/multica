// Package runcontrol defines the immutable authority attached to a controlled run.
package runcontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const Version = 1

type Repository struct {
	URL    string `json:"url"`
	Commit string `json:"commit"`
}

// Manifest is issued by the controller, stored before enqueue, and checked again
// at claim and provider start. Provider session/path observations are receipts,
// not fields a worker can use to change its accepted authority.
type Manifest struct {
	Version             int          `json:"version"`
	WorkspaceID         string       `json:"workspace_id"`
	IssueID             string       `json:"issue_id"`
	ScopeRevision       string       `json:"accepted_scope_revision"`
	ActionID            string       `json:"action_id"`
	Attempt             int32        `json:"attempt"`
	MaxAttempts         int32        `json:"max_attempts"`
	ParentRunID         string       `json:"parent_run_id,omitempty"`
	RunID               string       `json:"run_id"`
	ProfileID           string       `json:"profile_id"`
	ProfileHash         string       `json:"profile_hash"`
	RuntimeID           string       `json:"runtime_id"`
	HostID              string       `json:"host_id"`
	ExecutionMode       string       `json:"execution_mode"`
	SourcePath          string       `json:"source_path"`
	BaseCommit          string       `json:"base_commit"`
	AllowedRepositories []Repository `json:"allowed_repositories"`
	AuthorityRecordIDs  []string     `json:"authority_record_ids"`
	RequiredEffects     []string     `json:"required_effects"`
	CandidateIdentity   string       `json:"candidate_identity"`
	CancellationScope   string       `json:"cancellation_scope"`
	AuthorityEpoch      int64        `json:"authority_epoch"`
	QueuedAt            time.Time    `json:"queued_at"`
	NotBefore           time.Time    `json:"not_before"`
	Priority            int32        `json:"priority"`
	BudgetKey           string       `json:"budget_key"`
}

func Digest(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func IsCommit(value string) bool {
	return len(value) == 40 && strings.Trim(value, "0123456789abcdef") == ""
}

func (m Manifest) Validate() error {
	if m.Version != Version || m.ExecutionMode != "run_owned" {
		return errors.New("controlled runs require version 1 run_owned authority")
	}
	for name, value := range map[string]string{"workspace": m.WorkspaceID, "issue": m.IssueID, "scope": m.ScopeRevision, "action": m.ActionID, "run": m.RunID, "profile": m.ProfileID, "profile hash": m.ProfileHash, "runtime": m.RuntimeID, "host": m.HostID, "source": m.SourcePath, "candidate": m.CandidateIdentity, "cancellation scope": m.CancellationScope, "budget": m.BudgetKey} {
		if strings.TrimSpace(value) == "" || len(value) > 4096 {
			return fmt.Errorf("missing or invalid %s in launch authority", name)
		}
	}
	if m.AuthorityEpoch < 1 || m.Attempt < 1 || m.MaxAttempts < m.Attempt || m.MaxAttempts > 4 || m.QueuedAt.IsZero() || m.NotBefore.Before(m.QueuedAt) || m.Priority < 0 || m.Priority > 4 || len(m.AuthorityRecordIDs) == 0 || m.AllowedRepositories == nil || m.RequiredEffects == nil {
		return errors.New("authority requires epoch, queue order, explicit sources, effects and authority records")
	}
	if m.BaseCommit != "" && !IsCommit(m.BaseCommit) {
		return errors.New("base must be an immutable commit")
	}
	for _, repo := range m.AllowedRepositories {
		if repo.URL == "" || !IsCommit(repo.Commit) {
			return errors.New("every repository needs an exact URL and commit")
		}
	}
	// Provider turns may propose effects but never reserve them for their whole
	// duration. The separate short effect operation carries its own fenced lease.
	if len(m.RequiredEffects) != 0 {
		return errors.New("provider launches cannot hold external effect authority; use the fenced effect endpoint")
	}
	return nil
}
