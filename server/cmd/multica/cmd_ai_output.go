package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const aiTaskOutputPathEnv = "MULTICA_AI_TASK_OUTPUT_PATH"

func writeStructuredAIResult(raw string) error {
	outputPath := strings.TrimSpace(os.Getenv(aiTaskOutputPathEnv))
	if outputPath == "" {
		return fmt.Errorf("%s is not set; this command is only available inside an AI task", aiTaskOutputPathEnv)
	}
	clean := filepath.Clean(outputPath)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("%s must be an absolute path", aiTaskOutputPathEnv)
	}
	if filepath.Base(clean) != "ai-task-output.json" {
		return fmt.Errorf("%s must point to ai-task-output.json", aiTaskOutputPathEnv)
	}
	if clean != outputPath && strings.Contains(outputPath, "..") {
		return fmt.Errorf("%s must not contain path traversal", aiTaskOutputPathEnv)
	}
	payload := strings.TrimSpace(raw)
	if payload == "" {
		return fmt.Errorf("--output-results is required")
	}
	if !json.Valid([]byte(payload)) {
		return fmt.Errorf("--output-results must be valid JSON")
	}
	if err := os.WriteFile(clean, []byte(payload), 0o600); err != nil {
		return fmt.Errorf("write AI task output: %w", err)
	}
	return nil
}
