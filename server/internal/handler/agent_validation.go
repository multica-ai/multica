package handler

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/multica-ai/multica/server/internal/agentconfig"
)

func validateAgentMaxConcurrentTasks(value int32) error {
	if err := agentconfig.ValidateMaxConcurrentTasks(value); err != nil {
		return fmt.Errorf("max_concurrent_tasks %w", err)
	}
	return nil
}

func defaultAndValidateAgentMaxConcurrentTasks(rawFields map[string]json.RawMessage, value *int32) error {
	raw, provided := rawFields["max_concurrent_tasks"]
	if !provided || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*value = agentconfig.DefaultMaxConcurrentTasks
		return nil
	}
	return validateAgentMaxConcurrentTasks(*value)
}
