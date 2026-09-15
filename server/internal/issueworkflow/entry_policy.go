package issueworkflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	ExecutorNone = "none"

	MaxEntryInstructionsRunes = 8000
)

// EntryPolicyPrincipal references an executor: none, agent, or squad. The
// storage envelope stays JSON so a future workflow executor can be added
// without changing the workflow-status table.
type EntryPolicyPrincipal struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

// EntryPolicy is snapshotted onto an Automation Execution when an issue enters
// a status. Defaults describe a purely manual status with no responsibility
// change, matching the historical empty JSON policy seeded by migrations.
type EntryPolicy struct {
	Executor     EntryPolicyPrincipal `json:"executor"`
	Instructions string               `json:"instructions"`
}

func DefaultEntryPolicy() EntryPolicy {
	return EntryPolicy{
		Executor: EntryPolicyPrincipal{Type: ExecutorNone},
	}
}

// NormalizeEntryPolicy fills the backwards-compatible defaults and validates
// the policy's intrinsic shape. Workspace membership, archive state and invoke
// permission are request-context checks owned by the handler.
func NormalizeEntryPolicy(policy EntryPolicy) (EntryPolicy, error) {
	defaults := DefaultEntryPolicy()
	if policy.Executor.Type == "" {
		policy.Executor.Type = defaults.Executor.Type
	}

	switch policy.Executor.Type {
	case ExecutorNone:
		if policy.Executor.ID != "" {
			return EntryPolicy{}, errors.New("executor.id must be empty when executor.type is none")
		}
	case "agent", "squad":
		if strings.TrimSpace(policy.Executor.ID) == "" {
			return EntryPolicy{}, fmt.Errorf("executor.id is required when executor.type is %s", policy.Executor.Type)
		}
		if strings.TrimSpace(policy.Instructions) == "" {
			return EntryPolicy{}, errors.New("instructions are required when an executor is configured")
		}
	default:
		return EntryPolicy{}, errors.New("executor.type must be none, agent, or squad")
	}

	if len([]rune(policy.Instructions)) > MaxEntryInstructionsRunes {
		return EntryPolicy{}, fmt.Errorf("instructions must be at most %d characters", MaxEntryInstructionsRunes)
	}
	return policy, nil
}

func DecodeEntryPolicy(raw []byte) (EntryPolicy, error) {
	if len(raw) == 0 {
		return DefaultEntryPolicy(), nil
	}
	var policy EntryPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return EntryPolicy{}, fmt.Errorf("decode entry policy: %w", err)
	}
	return NormalizeEntryPolicy(policy)
}

func EncodeEntryPolicy(policy EntryPolicy) ([]byte, EntryPolicy, error) {
	normalized, err := NormalizeEntryPolicy(policy)
	if err != nil {
		return nil, EntryPolicy{}, err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, EntryPolicy{}, fmt.Errorf("encode entry policy: %w", err)
	}
	return raw, normalized, nil
}
