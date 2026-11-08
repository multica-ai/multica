package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

const residentTaskContextFile = ".multica_resident_task.json"

type residentTaskContext struct {
	TaskID        string `json:"task_id"`
	AgentID       string `json:"agent_id"`
	WorkspaceID   string `json:"workspace_id"`
	ChatSessionID string `json:"chat_session_id,omitempty"`
	Token         string `json:"token"`
}

// The resident owner outlives task environments, so its tools read a short-lived credential file.
func writeResidentTaskContext(envRoot, provider string, task Task, token string) (func(), error) {
	noop := func() {}
	if !execenv.ResidentHermesGatewayEnabled() || provider != "hermes" || envRoot == "" {
		return noop, nil
	}
	data, err := json.Marshal(residentTaskContext{
		TaskID:        task.ID,
		AgentID:       task.AgentID,
		WorkspaceID:   task.WorkspaceID,
		ChatSessionID: task.ChatSessionID,
		Token:         token,
	})
	if err != nil {
		return noop, err
	}
	tmp, err := os.CreateTemp(envRoot, ".multica_resident_task-*.tmp")
	if err != nil {
		return noop, err
	}
	defer os.Remove(tmp.Name())
	// CreateTemp creates a private 0600 file before any credential bytes are written.
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil {
		return noop, writeErr
	}
	if closeErr != nil {
		return noop, closeErr
	}
	path := filepath.Join(envRoot, residentTaskContextFile)
	if err := os.Rename(tmp.Name(), path); err != nil {
		return noop, err
	}
	return func() { _ = os.Remove(path) }, nil
}
