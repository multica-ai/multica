package codechanges

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ExecutionRef is captured from the claimed task, never resolved during replay.
// It binds the optional artifact without changing legacy task callbacks.
type ExecutionRef struct {
	RuntimeID    string `json:"runtime_id"`
	DispatchedAt string `json:"dispatched_at"`
}

func (r ExecutionRef) Validate() error {
	id, err := uuid.Parse(r.RuntimeID)
	if err != nil || id == uuid.Nil {
		return errors.New("invalid code changes runtime")
	}
	at, err := time.Parse(time.RFC3339Nano, r.DispatchedAt)
	if err != nil || at.IsZero() || at.Nanosecond()%1000 != 0 {
		return errors.New("invalid code changes dispatch generation")
	}
	return nil
}
