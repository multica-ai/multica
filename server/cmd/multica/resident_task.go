package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// This filename is shared with the daemon's short-lived credential handoff.
const residentTaskContextFile = ".multica_resident_task.json"

type residentTaskCredential struct {
	TaskID      string `json:"task_id"`
	AgentID     string `json:"agent_id"`
	WorkspaceID string `json:"workspace_id"`
	Token       string `json:"token"`
}

// Resident tools inherit the owner's environment, so discover task identity from their workdir.
func residentTaskCredentialFromCwd(token string) *residentTaskCredential {
	if os.Getenv("MULTICA_RESIDENT_OWNER") != "1" || os.Getenv("MULTICA_TASK_ID") != "" || strings.HasPrefix(token, "mat_") {
		return nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, residentTaskContextFile))
		if err == nil {
			var credential residentTaskCredential
			if json.Unmarshal(data, &credential) == nil && credential.TaskID != "" && strings.HasPrefix(credential.Token, "mat_") {
				return &credential
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}
