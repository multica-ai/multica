package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"

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

// validateLocalDirectory checks a directory-based agent's local_directory:
// empty is valid (a normal agent), non-empty must be an absolute path. The
// path's existence / readability is validated by the daemon at claim time,
// not here — the browser cannot see the daemon machine's filesystem.
func validateLocalDirectory(path string) error {
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("local_directory must be an absolute path")
	}
	return nil
}
