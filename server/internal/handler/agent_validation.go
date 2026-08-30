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

const defaultAgentOperatingMode = "coding"

func validateAgentOperatingMode(value string) error {
	switch value {
	case "coding", "operational", "hybrid":
		return nil
	default:
		return fmt.Errorf("operating_mode must be one of coding, operational, hybrid")
	}
}

func defaultAndValidateAgentOperatingMode(rawFields map[string]json.RawMessage, value *string) error {
	if _, provided := rawFields["operating_mode"]; !provided {
		*value = defaultAgentOperatingMode
		return nil
	}
	return validateAgentOperatingMode(*value)
}

func normaliseStoredAgentOperatingMode(value string) string {
	if validateAgentOperatingMode(value) != nil {
		return defaultAgentOperatingMode
	}
	return value
}
